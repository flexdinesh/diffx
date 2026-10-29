package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

type unavailablePreviewReader struct{}

func (unavailablePreviewReader) PreparePatches(context.Context, []review.RepositoryDiff, int, int) error {
	return fmt.Errorf("preview I/O unavailable")
}
func (unavailablePreviewReader) Patch(context.Context, review.RepositoryDiff, review.ChangedFile) (review.FilePatch, error) {
	return review.FilePatch{}, fmt.Errorf("preview I/O unavailable")
}
func (unavailablePreviewReader) Contents(context.Context, review.RepositoryDiff, review.ChangedFile) (review.FileContents, error) {
	return review.FileContents{}, fmt.Errorf("preview I/O unavailable")
}

func TestExhaustedPreviewBudgetDoesNotRequireSourceReads(t *testing.T) {
	units := []previewUnit{{snapshot: review.RepositoryDiff{Mode: review.DiffAll, Files: []review.ChangedFile{{ID: "file", Path: "file"}}}}}
	candidates, remaining, err := capturePatches(t.Context(), unavailablePreviewReader{}, units, 0, defaultPreviewLimits)
	if err != nil || remaining != 0 || len(candidates) != 1 || len(candidates[0]) != 0 {
		t.Fatalf("exhausted budget required preview reads: %v %d %v", candidates, remaining, err)
	}
}

func encodedBytes(t *testing.T, request ingestion.Request) int {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

// skeletonBytes is the encoded request with every file's preview omitted.
func skeletonBytes(t *testing.T, request ingestion.Request) int {
	t.Helper()
	scopes := make([]ingestion.Scope, len(request.Scopes))
	for index, scope := range request.Scopes {
		patches := make(map[string]review.FilePatch, len(scope.Patches))
		for id := range scope.Patches {
			patches[id] = review.FilePatch{Message: diffsource.Message(overBudgetMessage)}
		}
		scopes[index] = ingestion.Scope{Snapshot: scope.Snapshot, Patches: patches}
	}
	request.Scopes = scopes
	commits := make([]ingestion.Commit, len(request.Commits))
	for index, commit := range request.Commits {
		commits[index] = commit
		if commit.Patches != nil {
			commits[index].Patches = make(map[string]review.FilePatch, len(commit.Patches))
			for id := range commit.Patches {
				commits[index].Patches[id] = review.FilePatch{Message: diffsource.Message(overBudgetMessage)}
			}
		}
	}
	request.Commits = commits
	return encodedBytes(t, request)
}

func recollect(t *testing.T, root string, request ingestion.Request, limits previewLimits) ingestion.Request {
	t.Helper()
	source, err := diffsource.OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := withPreviews(t.Context(), source, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAllocateKeepsSharesAndRedistributesUnused(t *testing.T) {
	for _, test := range []struct {
		name   string
		costs  [][]int
		budget int
		kept   [][]bool
	}{
		{"share bounds a large scope", [][]int{{10, 10, 10, 10}, {10}}, 20, [][]bool{{true, false, false, false}, {true}}},
		{"unused share moves on", [][]int{{30, 30}, {10}, {}}, 70, [][]bool{{true, true}, {true}}},
		{"misfit skipped in file order", [][]int{{40, 10, 10}}, 25, [][]bool{{false, true, true}}},
		{"pool fits what no share can", [][]int{{50}, {50}}, 60, [][]bool{{true}, {false}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			counts := make([]int, len(test.costs))
			kept := make([][]bool, len(test.costs))
			for index, costs := range test.costs {
				counts[index], kept[index] = len(costs), make([]bool, len(costs))
			}
			left, err := allocate(counts, test.budget, func(scope, position, limit int) (int, bool, error) {
				cost := test.costs[scope][position]
				if cost > limit {
					return cost, false, nil
				}
				kept[scope][position] = true
				return cost, true, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			spent := 0
			for index := range test.costs {
				if len(test.kept) > index && len(test.kept[index]) > 0 && fmt.Sprint(kept[index]) != fmt.Sprint(test.kept[index]) {
					t.Fatalf("scope %d kept %v, want %v", index, kept[index], test.kept[index])
				}
				for position, cost := range test.costs[index] {
					if kept[index][position] {
						spent += cost
					}
				}
			}
			if left != test.budget-spent {
				t.Fatalf("left %d after spending %d of %d", left, spent, test.budget)
			}
		})
	}
}

func TestPreviewSharesKeepEveryComparisonAndRedistributeUnused(t *testing.T) {
	root := repository(t)
	writeFiles := func(value string, count int) {
		for i := range count {
			write(t, root, fmt.Sprintf("file-%d", i), strings.Repeat(value, 1<<14)+"\n")
		}
	}
	writeFiles("a", 4)
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	git(t, root, "switch", "-c", "feature")
	writeFiles("b", 4)
	git(t, root, "commit", "-am", "committed")
	writeFiles("c", 1)
	request := collect(t, root)
	if len(request.Scopes) != 5 || len(scopeFor(t, request, review.DiffStaged).Snapshot.Files) != 0 {
		t.Fatalf("fixture must capture five scopes with nothing staged: %+v", request.Scopes)
	}
	extra := 200 << 10
	limits := defaultPreviewLimits
	limits.requestBytes = skeletonBytes(t, request) + extra
	result := recollect(t, root, request, limits)
	if size := encodedBytes(t, result); size > limits.requestBytes {
		t.Fatalf("encoded request %d exceeds limit %d", size, limits.requestBytes)
	}
	for _, mode := range []review.DiffMode{review.DiffAll, review.DiffCommitted, review.DiffUncommitted, review.DiffUnstaged} {
		if patchFor(t, result, mode, "file-0").Patch == "" {
			t.Fatalf("%s starved by another scope", mode)
		}
	}
	if patchFor(t, result, review.DiffAll, "file-1").Patch == "" {
		t.Fatal("unused shares were not redistributed to the combined scope")
	}
	missing, withContents := false, false
	for _, scope := range result.Scopes {
		for _, patch := range scope.Patches {
			missing = missing || patch.Patch == "" && patch.Contents == nil
			withContents = withContents || patch.Contents != nil
		}
	}
	if !missing || withContents {
		t.Fatalf("full contents captured before every patch: missing %v, contents %v", missing, withContents)
	}
}

func TestPreviewFileAndCountLimits(t *testing.T) {
	root := repository(t)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	write(t, root, "b", strings.Join(lines, "\n")+"\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	lines[0] = "changed"
	write(t, root, "b", strings.Join(lines, "\n")+"\n")
	write(t, root, "a", "small\n")
	write(t, root, "c", strings.Repeat("c", 600)+"\n")
	write(t, root, "d", "after the file limit\n")
	request := collect(t, root)
	result := recollect(t, root, request, previewLimits{requestBytes: defaultPreviewLimits.requestBytes, patchBytes: 512, contentsBytes: 512, files: 3})
	if patch := patchFor(t, result, review.DiffAll, "a"); patch.Patch == "" || patch.Contents == nil {
		t.Fatalf("small file preview: %+v", patch)
	}
	if patch := patchFor(t, result, review.DiffAll, "b"); patch.Patch == "" || patch.Contents != nil || patch.Message == nil || !strings.HasPrefix(*patch.Message, "Full contents unavailable: file exceeds") {
		t.Fatalf("large contents keep their patch: %+v", patch)
	}
	if patch := patchFor(t, result, review.DiffAll, "c"); patch.Patch != "" || patch.Contents != nil || patch.Message == nil || !strings.HasPrefix(*patch.Message, "Diff exceeds") {
		t.Fatalf("oversize patch: %+v", patch)
	}
	if patch := patchFor(t, result, review.DiffAll, "d"); patch.Patch != "" || patch.Contents != nil || patch.Message == nil || *patch.Message != "Not captured: comparison exceeds 1000 files." {
		t.Fatalf("file past the count limit: %+v", patch)
	}
}

func TestOversizePatchStillShowsFullContentsThatFit(t *testing.T) {
	root := repository(t)
	write(t, root, "c", strings.Repeat("c", 600)+"\n")
	request := collect(t, root)
	result := recollect(t, root, request, previewLimits{requestBytes: defaultPreviewLimits.requestBytes, patchBytes: 512, contentsBytes: 1024, files: 3})
	if patch := patchFor(t, result, review.DiffAll, "c"); patch.Patch != "" || patch.Contents == nil || patch.Message != nil {
		t.Fatalf("full contents within limit: %+v", patch)
	}
}

func TestPreviewBudgetCountsEncodedBytes(t *testing.T) {
	root := repository(t)
	write(t, root, "markup", strings.Repeat("<>", 20000)+"\n")
	write(t, root, "plain", "plain\n")
	request := collect(t, root)
	limits := defaultPreviewLimits
	limits.requestBytes = skeletonBytes(t, request) + 100<<10
	result := recollect(t, root, request, limits)
	if size := encodedBytes(t, result); size > limits.requestBytes {
		t.Fatalf("encoded request %d exceeds limit %d", size, limits.requestBytes)
	}
	if patch := patchFor(t, result, review.DiffAll, "markup"); patch.Patch != "" || patch.Message == nil || *patch.Message != overBudgetMessage {
		t.Fatalf("escaped markup must not fit by raw size: %+v", patch)
	}
	if patch := patchFor(t, result, review.DiffAll, "plain"); patch.Patch == "" || patch.Contents == nil {
		t.Fatalf("later file kept after skipped one: %+v", patch)
	}
}

func TestPipedFilesOverPreviewLimitRemainAccepted(t *testing.T) {
	raw := "diff --git a/large b/large\nnew file mode 100644\n--- /dev/null\n+++ b/large\n@@ -0,0 +1 @@\n+" + strings.Repeat("a", 3<<19) + "\n"
	request, err := CollectPatch(t.Context(), raw, "", Options{SourceID: "source", Hostname: "host"})
	if err != nil {
		t.Fatal(err)
	}
	patch := request.Scopes[0].Patches[request.Scopes[0].Snapshot.Files[0].ID]
	if patch.Patch != "" || patch.Message == nil || *patch.Message != "Diff exceeds the 1 MiB preview limit." {
		t.Fatalf("piped file over the preview limit: %+v", patch)
	}
}

// Embedding the stored contract exercises its scalar adapter without Git's
// collection-only metadata/bulk extension.
type scalarPreviewSource struct{ diffsource.Source }

func TestBulkPreviewAllocationMatchesScalar(t *testing.T) {
	root := repository(t)
	for index := range 20 {
		write(t, root, fmt.Sprintf("file-%02d", index), strings.Repeat("before\n", 100))
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	git(t, root, "switch", "-c", "feature")
	for index := range 20 {
		write(t, root, fmt.Sprintf("file-%02d", index), strings.Repeat("after\n", 100))
	}
	git(t, root, "commit", "-am", "feature")
	write(t, root, "file-00", strings.Repeat("working\n", 100))
	request := collect(t, root)
	request.Commits = nil
	request.CommitsTotal = 0
	source, err := diffsource.OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	// Populate raw endpoint metadata just as production collection does.
	for _, scope := range request.Scopes {
		if _, err := source.Snapshot(t.Context(), scope.Snapshot.Mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, limits := range []previewLimits{
		defaultPreviewLimits,
		{requestBytes: skeletonBytes(t, request) + 10000, patchBytes: 512, contentsBytes: 1024, files: 10},
		{requestBytes: skeletonBytes(t, request) + 2000, patchBytes: 1 << 20, contentsBytes: 1 << 20, files: 1000},
	} {
		bulk, err := withPreviews(t.Context(), source, request, limits)
		if err != nil {
			t.Fatal(err)
		}
		scalar, err := withPreviews(t.Context(), scalarPreviewSource{Source: source}, request, limits)
		if err != nil {
			t.Fatal(err)
		}
		bulkJSON, _ := json.Marshal(bulk)
		scalarJSON, _ := json.Marshal(scalar)
		if string(bulkJSON) != string(scalarJSON) {
			t.Fatalf("bulk allocator changed previews for limits %+v", limits)
		}
	}
}
