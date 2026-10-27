package collector

import (
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func commitPatch(t *testing.T, commit ingestion.Commit, name string) review.FilePatch {
	t.Helper()
	if commit.Snapshot == nil {
		t.Fatalf("commit %s has no diff", commit.Subject)
	}
	for _, file := range commit.Snapshot.Files {
		if file.Path == name {
			return commit.Patches[file.ID]
		}
	}
	t.Fatalf("commit %s lacks %s", commit.Subject, name)
	return review.FilePatch{}
}

func TestCollectCapturesBranchCommits(t *testing.T) {
	root := repository(t)
	write(t, root, "base", "base\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	git(t, root, "switch", "-c", "feature")
	write(t, root, "first", "one\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "Add first")
	git(t, root, "switch", "-c", "side", "main")
	write(t, root, "side", "side\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "Side")
	git(t, root, "switch", "feature")
	git(t, root, "merge", "--no-ff", "-m", "Merge side", "side")
	write(t, root, "first", "two\n")
	git(t, root, "commit", "-am", "Edit first")
	write(t, root, "first", "uncommitted\n")
	request := collect(t, root)
	if err := ingestion.Validate(request); err != nil {
		t.Fatal(err)
	}
	if request.CommitsTotal != 4 || len(request.Commits) != 4 {
		t.Fatalf("commits: total %d captured %d", request.CommitsTotal, len(request.Commits))
	}
	subjects := []string{}
	for _, commit := range request.Commits {
		subjects = append(subjects, commit.Subject)
	}
	last := request.Commits[len(request.Commits)-1]
	if subjects[len(subjects)-1] != "Edit first" || request.Metadata.Head == nil || last.OID != *request.Metadata.Head {
		t.Fatalf("commits must end at HEAD, oldest first: %v", subjects)
	}
	for _, commit := range request.Commits {
		if commit.Merge() != (commit.Snapshot == nil) {
			t.Fatalf("only merges lack a diff: %+v", commit.Commit)
		}
	}
	patch := commitPatch(t, last, "first")
	if !strings.Contains(patch.Patch, "+two") || strings.Contains(patch.Patch, "uncommitted") || patch.Contents == nil || patch.Contents.Before != "one\n" {
		t.Fatalf("commit preview: %+v", patch)
	}
}

func TestCollectWithoutBaselineCapturesNoCommits(t *testing.T) {
	root := repository(t)
	write(t, root, "base", "base\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	write(t, root, "base", "edited\n")
	request := collect(t, root)
	if len(request.Commits) != 0 || request.CommitsTotal != 0 {
		t.Fatalf("default branch captured commits: %+v", request.Commits)
	}
}

func TestCommitPreviewsFollowComparisonPatches(t *testing.T) {
	root := repository(t)
	write(t, root, "base", "base\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	git(t, root, "switch", "-c", "feature")
	write(t, root, "large", strings.Repeat("x", 1<<14)+"\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "Add large")
	request := collect(t, root)
	limits := defaultPreviewLimits
	// Room for every comparison patch but not the commit's own copy.
	scopePatches := 0
	for _, scope := range request.Scopes {
		for id, patch := range scope.Patches {
			patch.Contents = nil
			scopePatches += encodedPatch(id, patch)
		}
	}
	limits.requestBytes = skeletonBytes(t, request) + scopePatches
	result := recollect(t, root, request, limits)
	if patchFor(t, result, review.DiffAll, "large").Patch == "" || patchFor(t, result, review.DiffCommitted, "large").Patch == "" {
		t.Fatal("commit previews took a comparison's patch budget")
	}
	if commitPatch(t, result.Commits[0], "large").Patch != "" {
		t.Fatal("commit patch captured beyond the budget")
	}
	limits.requestBytes = skeletonBytes(t, request) + scopePatches*3
	result = recollect(t, root, request, limits)
	commit := commitPatch(t, result.Commits[0], "large")
	if commit.Patch == "" {
		t.Fatal("commit patch missing with budget to spare")
	}
	if commit.Contents != nil && patchFor(t, result, review.DiffAll, "large").Contents == nil {
		t.Fatal("commit contents captured before comparison contents")
	}
}
