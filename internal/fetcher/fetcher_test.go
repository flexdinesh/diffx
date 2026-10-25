package fetcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

const testToken = "ghs_installation-token-value"

func requireTestGit(t *testing.T) {
	t.Helper()
	raw, err := exec.Command("git", "version").Output()
	if err != nil || !gitAtLeast(string(raw), 2, 44) {
		t.Skipf("fetcher requires git 2.44 or later: %s %v", raw, err)
	}
	t.Setenv("GIT_NO_LAZY_FETCH", "1")
}

func git(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_LAZY_FETCH=1")
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, raw)
	}
	return strings.TrimSpace(string(raw))
}

func write(t *testing.T, root, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

type origin struct {
	bare, work string
	base, head string
	// unchanged is a blob outside the pull request diff.
	unchanged string
}

// newOrigin publishes main and pull request 7, whose base advanced after the
// pull request branched so merge-base and base tip differ.
func newOrigin(t *testing.T) origin {
	t.Helper()
	work := t.TempDir()
	git(t, work, "init", "--quiet", "--initial-branch=main")
	git(t, work, "config", "user.name", "Test")
	git(t, work, "config", "user.email", "test@example.com")
	write(t, work, "stable.txt", "unchanged\n")
	write(t, work, "edited.txt", "before\n")
	write(t, work, "removed.txt", "gone soon\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "--quiet", "-m", "initial")
	git(t, work, "switch", "--quiet", "-c", "feature")
	write(t, work, "edited.txt", "after\n")
	write(t, work, "added.txt", "new file\n")
	git(t, work, "rm", "--quiet", "removed.txt")
	git(t, work, "add", ".")
	git(t, work, "commit", "--quiet", "-m", "feature")
	git(t, work, "switch", "--quiet", "main")
	write(t, work, "base-only.txt", "not in the pull request\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "--quiet", "-m", "base advances")
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, work, "init", "--quiet", "--bare", bare)
	git(t, bare, "config", "uploadpack.allowFilter", "true")
	git(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	git(t, work, "push", "--quiet", bare, "main:refs/heads/main", "feature:refs/pull/7/head")
	return origin{bare: bare, work: work, base: git(t, work, "rev-parse", "main"), head: git(t, work, "rev-parse", "feature"), unchanged: git(t, work, "rev-parse", "main:stable.txt")}
}

func (origin origin) job(repository int64) fetchprotocol.Job {
	return fetchprotocol.Job{ID: "job", LeaseToken: "lease", Repository: fetchprotocol.Repository{ID: repository, FullName: "octo/repo", CloneURL: "https://github.com/octo/repo.git"},
		Number: 7, HeadSHA: origin.head, BaseRef: "main", BaseSHA: origin.base, Token: testToken}
}

// local substitutes a file origin; production permits only GitHub HTTPS.
func (origin origin) remote() remote {
	return remote{protocol: "file", url: func(fetchprotocol.Repository) (string, error) { return "file://" + origin.bare, nil }}
}

type fakeServer struct {
	mu        sync.Mutex
	jobs      []fetchprotocol.Job
	completed []ingestion.Request
	failed    []string
	reported  func()
}

func (server *fakeServer) Lease(context.Context) (*fetchprotocol.Job, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.jobs) == 0 {
		return nil, nil
	}
	job := server.jobs[0]
	server.jobs = server.jobs[1:]
	return &job, nil
}

func (server *fakeServer) Complete(_ context.Context, _ fetchprotocol.Job, request ingestion.Request) error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.completed = append(server.completed, request)
	if server.reported != nil {
		server.reported()
	}
	return nil
}

func (server *fakeServer) Fail(_ context.Context, _ fetchprotocol.Job, detail string) error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.failed = append(server.failed, detail)
	if server.reported != nil {
		server.reported()
	}
	return nil
}

func newTestFetcher(t *testing.T, server Server, origin origin, limit int64) *fetcher {
	t.Helper()
	root := t.TempDir()
	return &fetcher{server: server, settings: Settings{Mirrors: root, DiskLimit: limit, Concurrency: 1, PollInterval: time.Millisecond, CollectorVersion: "test"},
		remote: origin.remote(), locks: repositoryLocks{held: make(map[int64]*repositoryLock)}}
}

func TestRunCollectsPullRequestDiffFromMergeBaseWithContents(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := &fakeServer{jobs: []fetchprotocol.Job{origin.job(42)}, reported: cancel}
	mirrors := t.TempDir()
	if err := run(ctx, server, Settings{Mirrors: mirrors, DiskLimit: 1 << 30, Concurrency: 2, PollInterval: time.Millisecond, CollectorVersion: "test"}, origin.remote()); err != nil {
		t.Fatal(err)
	}
	if len(server.failed) != 0 || len(server.completed) != 1 {
		t.Fatalf("completed %d failed %v", len(server.completed), server.failed)
	}
	request := server.completed[0]
	if len(request.Scopes) != 1 || request.Scopes[0].Snapshot.Mode != review.DiffAll {
		t.Fatalf("objects-only collection must capture only the all scope: %+v", request.Scopes)
	}
	if head := request.Metadata.Head; head == nil || *head != origin.head || request.Metadata.Comparison == nil || request.Metadata.Comparison.BaseCommit != origin.base {
		t.Fatalf("captured commits: %+v %+v", request.Metadata.Head, request.Metadata.Comparison)
	}
	scope := request.Scopes[0]
	contents := map[string]review.FileContents{}
	for _, file := range scope.Snapshot.Files {
		patch := scope.Patches[file.ID]
		if patch.Contents == nil {
			t.Fatalf("%s has no contents: %+v", file.Path, patch)
		}
		contents[file.Path+":"+file.Status] = *patch.Contents
	}
	want := map[string]review.FileContents{
		"edited.txt:M": {Before: "before\n", After: "after\n"}, "added.txt:A": {After: "new file\n"}, "removed.txt:D": {Before: "gone soon\n"},
	}
	if len(contents) != len(want) {
		t.Fatalf("pull request files %v, want %v (base-only changes excluded)", contents, want)
	}
	for key, value := range want {
		if contents[key] != value {
			t.Fatalf("%s contents %+v, want %+v", key, contents[key], value)
		}
	}
	mirror := filepath.Join(mirrors, "42")
	if missing := catFileMissing(t, mirror, origin.unchanged); !missing {
		t.Fatal("mirror downloaded a blob outside the pull request diff")
	}
	if git(t, mirror, "rev-parse", "--is-bare-repository") != "false" {
		t.Fatal("mirror must be a non-bare repository for collection")
	}
	assertNoToken(t, mirrors)
}

func catFileMissing(t *testing.T, repository, object string) bool {
	t.Helper()
	command := exec.Command("git", "cat-file", "--batch-check")
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	command.Stdin = strings.NewReader(object + "\n")
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.HasSuffix(strings.TrimSpace(string(raw)), " missing")
}

func assertNoToken(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), testToken) || strings.Contains(string(raw), basicCredential(testToken)) {
			t.Errorf("token persisted in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMovedHeadFailsForRefetch(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	server := &fakeServer{}
	fetcher := newTestFetcher(t, server, origin, 1<<30)
	job := origin.job(1)
	job.HeadSHA = origin.base
	fetcher.handle(t.Context(), job)
	if len(server.completed) != 0 || len(server.failed) != 1 || server.failed[0] != errMoved.Error() {
		t.Fatalf("completed %d failed %v", len(server.completed), server.failed)
	}
}

func TestFailuresNeverExposeToken(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	server := &fakeServer{}
	fetcher := newTestFetcher(t, server, origin, 1<<30)
	job := origin.job(1)
	job.Number = 8
	fetcher.handle(t.Context(), job)
	if len(server.failed) != 1 || !strings.Contains(server.failed[0], "fetch pull request") {
		t.Fatalf("missing pull request ref: %v", server.failed)
	}
	assertNoToken(t, fetcher.settings.Mirrors)
	if got := redact("header Authorization: Basic "+basicCredential(testToken)+" token "+testToken, testToken); strings.Contains(got, testToken) || strings.Contains(got, basicCredential(testToken)) {
		t.Fatalf("redaction leaked credentials: %s", got)
	}
}

func TestSecondJobReusesMirror(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	server := &fakeServer{}
	fetcher := newTestFetcher(t, server, origin, 1<<30)
	fetcher.handle(t.Context(), origin.job(5))
	sentinel := filepath.Join(fetcher.settings.Mirrors, "5", ".git", "sentinel")
	write(t, filepath.Dir(sentinel), "sentinel", "kept")
	git(t, origin.work, "switch", "--quiet", "feature")
	write(t, origin.work, "edited.txt", "after again\n")
	git(t, origin.work, "commit", "--quiet", "-am", "follow-up")
	git(t, origin.work, "push", "--quiet", "--force", origin.bare, "feature:refs/pull/7/head")
	job := origin.job(5)
	job.HeadSHA = git(t, origin.work, "rev-parse", "feature")
	fetcher.handle(t.Context(), job)
	if len(server.failed) != 0 || len(server.completed) != 2 {
		t.Fatalf("completed %d failed %v", len(server.completed), server.failed)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("second job recreated the mirror: %v", err)
	}
	if head := server.completed[1].Metadata.Head; head == nil || *head != job.HeadSHA {
		t.Fatalf("second job captured %v, want %s", head, job.HeadSHA)
	}
}

func TestCollectionNeverFetchesMissingObjects(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	fetcher := newTestFetcher(t, &fakeServer{}, origin, 1<<30)
	job := origin.job(3)
	mirror, err := fetcher.open(t.Context(), 3, "file://"+origin.bare, job.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := mirror.fetch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	// Without prefetch, changed blobs are absent; collection must fail rather
	// than fetch them from the reachable origin.
	if _, err := mirror.collect(t.Context(), job, "test"); err == nil || !strings.Contains(err.Error(), "lazy fetching disabled") {
		t.Fatalf("collection without the pull request's blobs: %v", err)
	}
	if !catFileMissing(t, mirror.dir, git(t, origin.work, "rev-parse", "feature:added.txt")) {
		t.Fatal("collection fetched a missing blob")
	}
}

func TestDiskLimitEvictsLeastRecentlyUsedIdleMirrors(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	server := &fakeServer{}
	fetcher := newTestFetcher(t, server, origin, 1<<40)
	for _, id := range []int64{1, 2, 3} {
		fetcher.handle(t.Context(), origin.job(id))
	}
	if len(server.completed) != 3 {
		t.Fatalf("setup failed: %v", server.failed)
	}
	root := fetcher.settings.Mirrors
	now := time.Now()
	for id, age := range map[string]time.Duration{"1": 3 * time.Hour, "2": time.Hour, "3": 2 * time.Hour} {
		marker := filepath.Join(root, id, ".git", usedMarker)
		if err := os.Chtimes(marker, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	fetcher.settings.DiskLimit = directorySize(root) - directorySize(filepath.Join(root, "1"))
	fetcher.enforceDiskLimit()
	assertMirrors(t, root, "2", "3")
	release := fetcher.locks.lock(2)
	fetcher.settings.DiskLimit = 1
	fetcher.enforceDiskLimit()
	release()
	assertMirrors(t, root, "2")
}

func assertMirrors(t *testing.T, root string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("mirrors %v, want %v", names, want)
	}
}

type blockingServer struct {
	fakeServer
	mu     sync.Mutex
	active int
	calls  int
}

func (server *blockingServer) Lease(ctx context.Context) (*fetchprotocol.Job, error) {
	server.mu.Lock()
	server.active++
	server.calls++
	server.mu.Unlock()
	<-ctx.Done()
	time.Sleep(10 * time.Millisecond)
	server.mu.Lock()
	server.active--
	server.mu.Unlock()
	return nil, ctx.Err()
}

func TestRunJoinsWorkersOnCancel(t *testing.T) {
	requireTestGit(t)
	server := &blockingServer{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, server, Settings{Mirrors: t.TempDir(), DiskLimit: 1, Concurrency: 3}, github)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		server.mu.Lock()
		calls := server.calls
		server.mu.Unlock()
		if calls == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of 3 workers leased", calls)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.active != 0 {
		t.Fatalf("Run returned with %d workers still leasing", server.active)
	}
}

func TestRunRequiresLazyFetchDisabled(t *testing.T) {
	t.Setenv("GIT_NO_LAZY_FETCH", "")
	err := Run(t.Context(), &fakeServer{}, Settings{Mirrors: t.TempDir(), DiskLimit: 1, Concurrency: 1})
	if err == nil || !strings.Contains(err.Error(), "GIT_NO_LAZY_FETCH") {
		t.Fatalf("Run started with lazy fetch enabled: %v", err)
	}
}

func TestGitHubRemoteAcceptsOnlyLeasedHTTPSRepository(t *testing.T) {
	valid := fetchprotocol.Repository{ID: 1, FullName: "octo-org/repo.name_1", CloneURL: "https://github.com/octo-org/repo.name_1.git"}
	if url, err := githubURL(valid); err != nil || url != valid.CloneURL {
		t.Fatalf("valid repository: %q %v", url, err)
	}
	for _, clone := range []string{
		"http://github.com/octo-org/repo.name_1.git", "https://github.com/other/repo.git", "https://x@github.com/octo-org/repo.name_1.git",
		"https://github.com.evil/octo-org/repo.name_1.git", "file:///octo-org/repo.name_1.git", "https://github.com/octo-org/repo.name_1",
		"ext::sh -c touch% /tmp/pwned", "https://github.com/octo-org/repo.name_1.git?x=1",
	} {
		repository := valid
		repository.CloneURL = clone
		if _, err := githubURL(repository); err == nil {
			t.Errorf("accepted clone URL %q", clone)
		}
	}
	for _, name := range []string{"octo/..", "octo/.", "../repo", "octo/repo/extra", "-octo/repo", "octo/re po", "octo/repo.git"} {
		repository := fetchprotocol.Repository{ID: 1, FullName: name, CloneURL: "https://github.com/" + name + ".git"}
		if _, err := githubURL(repository); err == nil {
			t.Errorf("accepted repository name %q", name)
		}
	}
}

func TestBaseRefRejectsRefspecAndOptionSyntax(t *testing.T) {
	for _, name := range []string{"main", "release/1.2", "feature/ü-name", "v1.0-rc"} {
		if !validBranch(name) {
			t.Errorf("rejected branch %q", name)
		}
	}
	for _, name := range []string{"", "-main", "a..b", "a:b", "main*", "a/.hidden", "main.lock", "main/", "a//b", "a@{1}", "@", "a b", "a\nb", "a~1", "a^", "a\\b", "main."} {
		if validBranch(name) {
			t.Errorf("accepted branch %q", name)
		}
	}
}

func TestChangedBlobsSkipsSubmodulesAndAbsentSides(t *testing.T) {
	zero := strings.Repeat("0", 40)
	a, b, c, d := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	raw := ":100644 100644 " + a + " " + b + " M\x00edited\x00:000000 100644 " + zero + " " + a + " A\x00copy\x00:160000 160000 " + c + " " + d + " M\x00sub\x00"
	blobs, err := changedBlobs(raw)
	if err != nil || strings.Join(blobs, ",") != a+","+b {
		t.Fatalf("blobs %v %v", blobs, err)
	}
	if _, err := changedBlobs("garbage\x00path\x00"); err == nil {
		t.Fatal("accepted malformed diff-tree output")
	}
}
