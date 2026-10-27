package diffsource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/flexdinesh/diffx/internal/review"
)

// commitTarget selects one commit's diff against its first parent, or the
// empty tree for a root commit.
type commitTarget struct {
	oid, parent string
}

// CommitRange is a branch comparison's captured commits, oldest first. Total
// counts the whole merge-base..HEAD range, including commits beyond the cap.
type CommitRange struct {
	Commits []review.Commit
	Total   int
}

const (
	maxCommitSubject = 4 << 10
	maxCommitBody    = 64 << 10
	commitFields     = 7
)

// MaxCommits is how many of a branch's newest commits Commits captures, so
// object prefetchers cover exactly what collection reads.
const MaxCommits = review.MaxCommits

// Commits lists the newest MaxCommits commits of a branch comparison's
// merge-base..HEAD range, oldest first. False means the source has no branch
// baseline, so it has no commits to capture.
func Commits(ctx context.Context, source Source) (CommitRange, bool, error) {
	git, ok := source.(*gitSource)
	if !ok || git.comparison == nil || git.commit != nil {
		return CommitRange{}, false, nil
	}
	span := git.comparison.BaseOID + ".." + git.comparison.HeadOID
	count, err := runGit(ctx, git.root, 64, "rev-list", "--count", span, "--")
	if err != nil {
		return CommitRange{}, false, err
	}
	total, err := strconv.Atoi(strings.TrimSpace(count))
	if err != nil {
		return CommitRange{}, false, fmt.Errorf("count commits: %w", err)
	}
	result := CommitRange{Commits: []review.Commit{}, Total: total}
	if total == 0 {
		return result, true, nil
	}
	raw, err := runGit(ctx, git.root, 32<<20, "log", "-z", "--topo-order", "--no-color", "--no-show-signature", "--encoding=UTF-8",
		fmt.Sprintf("--max-count=%d", review.MaxCommits), "--format=%H%x00%P%x00%an%x00%at%x00%ct%x00%s%x00%b", span, "--")
	if err != nil {
		return CommitRange{}, false, err
	}
	fields := strings.Split(raw, "\x00")
	if len(fields)%commitFields == 1 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%commitFields != 0 {
		return CommitRange{}, false, errors.New("unexpected commit log output")
	}
	for index := len(fields) - commitFields; index >= 0; index -= commitFields {
		record := fields[index : index+commitFields]
		authored, authoredErr := strconv.ParseInt(record[3], 10, 64)
		committed, committedErr := strconv.ParseInt(record[4], 10, 64)
		if err := errors.Join(authoredErr, committedErr); err != nil {
			return CommitRange{}, false, fmt.Errorf("parse commit times: %w", err)
		}
		parents := strings.Fields(record[1])
		if parents == nil {
			parents = []string{}
		}
		result.Commits = append(result.Commits, review.Commit{
			OID:         strings.TrimSpace(record[0]),
			Parents:     parents,
			Subject:     bounded(record[5], maxCommitSubject),
			Body:        bounded(strings.TrimRight(record[6], "\n"), maxCommitBody),
			AuthorName:  bounded(record[2], maxCommitSubject),
			AuthoredAt:  authored * 1000,
			CommittedAt: committed * 1000,
		})
	}
	if err := git.patchIDs(ctx, result.Commits); err != nil {
		return CommitRange{}, false, err
	}
	return result, true, nil
}

// patchIDs records stable patch IDs, which survive a rebase that leaves a
// commit's changes intact. Patches over the output limit keep no patch ID.
func (source *gitSource) patchIDs(ctx context.Context, commits []review.Commit) error {
	args := []string{"show", "--no-color", "--no-show-signature", "--no-ext-diff", "--no-textconv", "--patch", "--format=commit %H"}
	selected := 0
	for _, commit := range commits {
		if !commit.Merge() {
			args = append(args, commit.OID)
			selected++
		}
	}
	if selected == 0 {
		return nil
	}
	patches, err := runGit(ctx, source.root, 64<<20, append(args, "--")...)
	if errors.Is(err, errOutputLimit) {
		return nil
	}
	if err != nil {
		return err
	}
	output, err := runGitInput(ctx, source.root, 1<<20, strings.NewReader(patches), "patch-id", "--stable")
	if err != nil {
		return err
	}
	ids := make(map[string]string, selected)
	for _, line := range strings.Split(output, "\n") {
		if patchID, oid, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			ids[oid] = patchID
		}
	}
	for index := range commits {
		commits[index].PatchID = ids[commits[index].OID]
	}
	return nil
}

// CommitSource reads one captured commit's diff against its first parent,
// within the branch comparison that listed it.
func CommitSource(ctx context.Context, source Source, commit review.Commit) (Source, error) {
	git, ok := source.(*gitSource)
	if !ok || git.comparison == nil || commit.Merge() {
		return nil, errors.New("commit diffs require a branch comparison and a non-merge commit")
	}
	parent := ""
	if len(commit.Parents) == 1 {
		parent = commit.Parents[0]
	} else {
		empty, err := runGit(ctx, git.root, 1024, "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return nil, err
		}
		parent = strings.TrimSpace(empty)
	}
	return &gitSource{root: git.root, comparison: git.comparison, commit: &commitTarget{oid: commit.OID, parent: parent}}, nil
}

// bounded keeps valid UTF-8 within maximum bytes.
func bounded(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximum {
		return value
	}
	cut := maximum
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
