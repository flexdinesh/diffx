package diffsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/review"
)

func commitFile(t *testing.T, root, name, contents, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "add", name)
	testGit(t, root, "commit", "-m", message)
}

func branchCommits(t *testing.T, root string) (Source, CommitRange) {
	t.Helper()
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	commits, ok, err := Commits(t.Context(), source)
	if err != nil || !ok {
		t.Fatalf("commits: %v %v", ok, err)
	}
	return source, commits
}

func TestCommitsListBranchRangeOldestFirst(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "--create", "feature")
	commitFile(t, root, "first", "one\n", "Add first\n\nExplains why.")
	commitFile(t, root, "second", "two\n", "Add second")
	source, commits := branchCommits(t, root)
	if commits.Total != 2 || len(commits.Commits) != 2 {
		t.Fatalf("range: %+v", commits)
	}
	first, second := commits.Commits[0], commits.Commits[1]
	if first.Subject != "Add first" || first.Body != "Explains why." || second.Subject != "Add second" {
		t.Fatalf("order or message: %+v", commits.Commits)
	}
	if first.AuthorName != "Test" || first.AuthoredAt <= 0 || first.CommittedAt <= 0 || len(first.Parents) != 1 || first.PatchID == "" {
		t.Fatalf("metadata: %+v", first)
	}
	if second.Parents[0] != first.OID {
		t.Fatalf("parents: %+v", second)
	}
	commitSource, err := CommitSource(t.Context(), source, second)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := commitSource.Snapshot(t.Context(), review.DiffCommit)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ComparisonKind != review.ComparisonCommit || snapshot.Head == nil || *snapshot.Head != second.OID || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "second" {
		t.Fatalf("commit snapshot: %+v", snapshot)
	}
	patch, err := commitSource.Patch(t.Context(), review.DiffCommit, snapshot.Files[0], snapshot.Head)
	if err != nil || !strings.Contains(patch.Patch, "+two") || strings.Contains(patch.Patch, "+one") {
		t.Fatalf("commit patch: %+v %v", patch, err)
	}
	contents, err := commitSource.Contents(t.Context(), review.DiffCommit, snapshot.Files[0], snapshot.Head)
	if err != nil || contents.Before != "" || contents.After != "two\n" {
		t.Fatalf("commit contents: %+v %v", contents, err)
	}
}

func TestCommitsOmitUncommittedAndWorkingTreeChanges(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "--create", "feature")
	commitFile(t, root, "first", "one\n", "Add first")
	if err := os.WriteFile(filepath.Join(root, "first"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, commits := branchCommits(t, root)
	commitSource, err := CommitSource(t.Context(), source, commits.Commits[0])
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := commitSource.Snapshot(t.Context(), review.DiffCommit)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := commitSource.Contents(t.Context(), review.DiffCommit, snapshot.Files[0], snapshot.Head)
	if err != nil || contents.After != "one\n" {
		t.Fatalf("commit diff read the working tree: %+v %v", contents, err)
	}
}

func TestCommitsCapNewestAndCountOmitted(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "--create", "feature")
	for index := range review.MaxCommits + 2 {
		testGit(t, root, "commit", "--allow-empty", "-m", "commit "+strings.Repeat("x", index%3)+string(rune('a'+index%26)))
	}
	_, commits := branchCommits(t, root)
	if commits.Total != review.MaxCommits+2 || len(commits.Commits) != review.MaxCommits {
		t.Fatalf("cap: total %d captured %d", commits.Total, len(commits.Commits))
	}
	head := strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD"))
	if commits.Commits[len(commits.Commits)-1].OID != head {
		t.Fatalf("cap kept older commits")
	}
}

func TestCommitsKeepMergesWithoutPatchID(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "--create", "feature")
	commitFile(t, root, "feature", "feature\n", "Feature work")
	testGit(t, root, "switch", "--create", "side", "main")
	commitFile(t, root, "side", "side\n", "Side work")
	testGit(t, root, "switch", "feature")
	testGit(t, root, "merge", "--no-ff", "-m", "Merge side", "side")
	source, commits := branchCommits(t, root)
	var merge *review.Commit
	for index := range commits.Commits {
		if commits.Commits[index].Merge() {
			merge = &commits.Commits[index]
		}
	}
	if merge == nil || merge.Subject != "Merge side" || merge.PatchID != "" {
		t.Fatalf("merge: %+v", commits.Commits)
	}
	if _, err := CommitSource(t.Context(), source, *merge); err == nil {
		t.Fatal("merge commits have no captured diff")
	}
}

func TestCommitPatchIDSurvivesRebase(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "--create", "feature")
	commitFile(t, root, "feature", "feature\n", "Feature work")
	_, before := branchCommits(t, root)
	testGit(t, root, "switch", "main")
	commitFile(t, root, "other", "other\n", "Main moves")
	testGit(t, root, "switch", "feature")
	testGit(t, root, "rebase", "main")
	_, after := branchCommits(t, root)
	if before.Commits[0].OID == after.Commits[0].OID || before.Commits[0].PatchID != after.Commits[0].PatchID {
		t.Fatalf("rebase: %+v %+v", before.Commits[0], after.Commits[0])
	}
}

func TestCommitsRequireBranchComparison(t *testing.T) {
	root := comparisonRepository(t)
	source, err := OpenRepository(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Commits(t.Context(), source); ok || err != nil {
		t.Fatalf("HEAD-only source listed commits: %v %v", ok, err)
	}
	source, commits := branchCommits(t, root)
	if commits.Total != 0 || len(commits.Commits) != 0 {
		t.Fatalf("default branch listed its own commits: %+v", commits)
	}
	_ = source
}
