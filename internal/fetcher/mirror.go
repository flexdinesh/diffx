package fetcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/ingestion"
)

// remote resolves a leased repository's fetch URL and the only Git transport
// allowed to reach it. Production accepts GitHub HTTPS alone.
type remote struct {
	protocol string
	url      func(fetchprotocol.Repository) (string, error)
}

var github = remote{protocol: "https", url: githubURL}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?/[A-Za-z0-9._-]+$`)

func githubURL(repository fetchprotocol.Repository) (string, error) {
	name := repository.FullName[strings.IndexByte(repository.FullName, '/')+1:]
	if !repositoryName.MatchString(repository.FullName) || name == "." || name == ".." || strings.HasSuffix(name, ".git") ||
		repository.CloneURL != "https://github.com/"+repository.FullName+".git" {
		return "", errors.New("clone URL must be https://github.com/OWNER/NAME.git for the leased repository")
	}
	return repository.CloneURL, nil
}

var objectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func validateJob(job fetchprotocol.Job) error {
	switch {
	case job.Repository.ID <= 0 || job.Number <= 0:
		return errors.New("lease requires a repository ID and pull request number")
	case !objectID.MatchString(job.HeadSHA) || !objectID.MatchString(job.BaseSHA):
		return errors.New("lease head and base must be full commit IDs")
	case job.Token == "" || strings.ContainsAny(job.Token, "\r\n"):
		return errors.New("lease requires an installation token")
	case !validBranch(job.BaseRef):
		return errors.New("lease base ref is not a valid branch name")
	}
	return nil
}

// validBranch applies `git check-ref-format --branch` rules for a refs/heads
// name, and also excludes refspec and option syntax.
func validBranch(name string) bool {
	if name == "" || len(name) > 255 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") || name == "@" ||
		strings.ContainsAny(name, " ~^:?*[\\\x7f") {
		return false
	}
	for _, character := range name {
		if character < 0x20 {
			return false
		}
	}
	for _, component := range strings.Split(name, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

type mirror struct {
	dir      string
	protocol string
	token    string
}

const usedMarker = "diffx-fetcher-used"

// open reuses <Mirrors>/<repository ID>, or atomically publishes a new partial
// clone there. Non-bare layout lets the collector resolve a repository root;
// nothing is ever checked out.
func (fetcher *fetcher) open(ctx context.Context, id int64, url, token string) (mirror, error) {
	repository := mirror{dir: filepath.Join(fetcher.settings.Mirrors, strconv.FormatInt(id, 10)), protocol: fetcher.remote.protocol, token: token}
	if _, err := os.Stat(filepath.Join(repository.dir, ".git", usedMarker)); errors.Is(err, os.ErrNotExist) {
		// A mirror without its marker was never published; start over.
		if err := os.RemoveAll(repository.dir); err != nil {
			return mirror{}, err
		}
		if err := fetcher.create(ctx, repository); err != nil {
			return mirror{}, err
		}
	} else if err != nil {
		return mirror{}, err
	}
	// The URL follows repository renames by ID; it never carries credentials.
	if _, err := repository.git(ctx, false, "", "config", "remote.origin.url", url); err != nil {
		return mirror{}, err
	}
	now := time.Now()
	return repository, os.Chtimes(filepath.Join(repository.dir, ".git", usedMarker), now, now)
}

func (fetcher *fetcher) create(ctx context.Context, target mirror) error {
	staging, err := os.MkdirTemp(fetcher.settings.Mirrors, stagingPrefix)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	repository := mirror{dir: staging, protocol: target.protocol}
	if _, err := repository.git(ctx, false, "", "init", "--quiet"); err != nil {
		return err
	}
	for _, setting := range [][2]string{
		{"core.repositoryformatversion", "1"},
		{"extensions.partialClone", "origin"},
		{"remote.origin.promisor", "true"},
		{"remote.origin.partialclonefilter", "blob:none"},
	} {
		if _, err := repository.git(ctx, false, "", "config", setting[0], setting[1]); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(staging, ".git", usedMarker), nil, 0o600); err != nil {
		return err
	}
	return os.Rename(staging, target.dir)
}

func headRef(job fetchprotocol.Job) string { return "refs/heads/diffx/pr/" + strconv.Itoa(job.Number) }
func baseRef(job fetchprotocol.Job) string { return "refs/remotes/origin/" + job.BaseRef }

func (repository mirror) fetch(ctx context.Context, job fetchprotocol.Job) error {
	// Only the leased base is kept, so renamed or nested base branches never
	// conflict with an earlier lease's ref.
	refs, err := repository.git(ctx, false, "", "for-each-ref", "--format=%(refname)", "refs/remotes/origin/")
	if err != nil {
		return err
	}
	var stale strings.Builder
	for _, ref := range strings.Fields(refs) {
		if ref != baseRef(job) {
			stale.WriteString("delete " + ref + "\n")
		}
	}
	if stale.Len() > 0 {
		if _, err := repository.git(ctx, false, stale.String(), "update-ref", "--stdin"); err != nil {
			return err
		}
	}
	_, err = repository.git(ctx, true, "", "fetch", "--quiet", "--no-tags", "--recurse-submodules=no", "--filter=blob:none", "origin",
		"+refs/pull/"+strconv.Itoa(job.Number)+"/head:"+headRef(job), "+refs/heads/"+job.BaseRef+":"+baseRef(job))
	if err != nil {
		return fmt.Errorf("fetch pull request: %w", err)
	}
	return nil
}

func (repository mirror) verify(ctx context.Context, job fetchprotocol.Job) error {
	for _, expected := range [][2]string{{headRef(job), job.HeadSHA}, {baseRef(job), job.BaseSHA}} {
		current, err := repository.git(ctx, false, "", "rev-parse", "--verify", "--end-of-options", expected[0]+"^{commit}")
		if err != nil {
			return err
		}
		if strings.TrimSpace(current) != expected[1] {
			return errMoved
		}
	}
	return nil
}

// prefetch downloads only blobs on either side of the pull request diff and
// of each captured commit's own diff, so offline collection can read every
// patch and full file contents.
func (repository mirror) prefetch(ctx context.Context, job fetchprotocol.Job) error {
	mergeBase, err := repository.git(ctx, false, "", "merge-base", job.BaseSHA, job.HeadSHA)
	if err != nil {
		return fmt.Errorf("find merge base: %w", err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	// Without rename detection both sides of every change are listed.
	raw, err := repository.git(ctx, false, "", "diff-tree", "-r", "--raw", "--no-abbrev", "--no-renames", "-z", mergeBase, job.HeadSHA)
	if err != nil {
		return err
	}
	blobs, err := changedBlobs(raw)
	if err != nil {
		return err
	}
	// Collection captures the newest commits; merges carry no diff.
	commits, err := repository.git(ctx, false, "", "rev-list", "--no-merges", fmt.Sprintf("--max-count=%d", diffsource.MaxCommits), mergeBase+".."+job.HeadSHA, "--")
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}
	seen := make(map[string]bool, len(blobs))
	for _, blob := range blobs {
		seen[blob] = true
	}
	for _, commit := range strings.Fields(commits) {
		raw, err := repository.git(ctx, false, "", "diff-tree", "-r", "--raw", "--no-abbrev", "--no-renames", "--no-commit-id", "--root", "-z", commit)
		if err != nil {
			return err
		}
		changed, err := changedBlobs(raw)
		if err != nil {
			return err
		}
		for _, blob := range changed {
			if !seen[blob] {
				seen[blob] = true
				blobs = append(blobs, blob)
			}
		}
	}
	missing, err := repository.missing(ctx, blobs)
	if err != nil || len(missing) == 0 {
		return err
	}
	_, err = repository.git(ctx, true, strings.Join(missing, "\n")+"\n", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
		"--recurse-submodules=no", "--filter=blob:none", "--stdin", "origin")
	if err != nil {
		return fmt.Errorf("fetch changed files: %w", err)
	}
	missing, err = repository.missing(ctx, missing)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d changed file objects unavailable after fetch, including %s", len(missing), missing[0])
	}
	return nil
}

func changedBlobs(raw string) ([]string, error) {
	fields := strings.Split(raw, "\x00")
	seen := make(map[string]bool)
	var blobs []string
	for index := 0; index+1 < len(fields); index += 2 {
		header, found := strings.CutPrefix(fields[index], ":")
		parts := strings.Fields(header)
		if !found || len(parts) != 5 {
			return nil, fmt.Errorf("unexpected diff-tree record %q", fields[index])
		}
		for _, side := range [][2]string{{parts[0], parts[2]}, {parts[1], parts[3]}} {
			mode, oid := side[0], side[1]
			// Submodule commits and absent sides have no blob to read.
			if mode == "160000" || strings.Trim(oid, "0") == "" || seen[oid] {
				continue
			}
			seen[oid] = true
			blobs = append(blobs, oid)
		}
	}
	return blobs, nil
}

func (repository mirror) missing(ctx context.Context, objects []string) ([]string, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	raw, err := repository.git(ctx, false, strings.Join(objects, "\n")+"\n", "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if oid, absent := strings.CutSuffix(line, " missing"); absent {
			missing = append(missing, oid)
		}
	}
	return missing, nil
}

// collect reads only local objects: the collector's Git processes inherit
// GIT_NO_LAZY_FETCH=1, which Run requires.
func (repository mirror) collect(ctx context.Context, job fetchprotocol.Job, collectorVersion string) (ingestion.Request, error) {
	request, _, err := collector.CollectChanged(ctx, repository.dir, collector.Options{
		SourceID: "github", Hostname: "github.com", Trigger: "github", CollectorVersion: collectorVersion,
		Branch: strings.TrimPrefix(headRef(job), "refs/heads/"), Base: baseRef(job),
		Identity: &diffsource.StableRepositoryIdentity{RepositoryKey: "github", CheckoutKey: "github", BranchID: "github"},
	}, "")
	if err != nil {
		return ingestion.Request{}, fmt.Errorf("collect pull request: %w", err)
	}
	return request, nil
}
