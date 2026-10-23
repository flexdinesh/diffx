package diffsource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/review"
)

func writeScopeFile(t *testing.T, root, path, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func committedScopeSource(t *testing.T) (string, Source) {
	t.Helper()
	root := comparisonRepository(t)
	testGit(t, root, "switch", "-c", "feature")
	writeScopeFile(t, root, "tracked", "committed\n")
	testGit(t, root, "commit", "-am", "feature")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	return root, source
}

func TestCommittedScopeIgnoresIndexWorktreeAndUntrackedIdentity(t *testing.T) {
	root, source := committedScopeSource(t)
	snapshot, err := source.Snapshot(t.Context(), review.DiffCommitted)
	if err != nil {
		t.Fatal(err)
	}
	original, err := ContentHash(t.Context(), source, []review.RepositoryDiff{snapshot})
	if err != nil || original == "" {
		t.Fatalf("committed identity %q: %v", original, err)
	}
	for _, mutate := range []func(){
		func() { writeScopeFile(t, root, "tracked", "staged\n"); testGit(t, root, "add", "tracked") },
		func() { writeScopeFile(t, root, "tracked", "working\n"); writeScopeFile(t, root, "new", "untracked\n") },
		func() { testGit(t, root, "rm", "-f", "tracked") },
	} {
		mutate()
		next, err := source.Snapshot(t.Context(), review.DiffCommitted)
		if err != nil || !reflect.DeepEqual(next, snapshot) {
			t.Fatalf("checkout affected committed manifest: %+v, %v", next, err)
		}
		got, err := ContentHash(t.Context(), source, []review.RepositoryDiff{next})
		if err != nil || got != original {
			t.Fatalf("checkout affected committed identity: %q, %v", got, err)
		}
		file := next.Files[0]
		if file.IndexStatus != "" || file.WorktreeStatus != "" {
			t.Fatalf("committed scope leaked staging metadata: %+v", file)
		}
		contents, err := source.Contents(t.Context(), next.Mode, file, next.Head)
		if err != nil || contents.Before != "base\n" || contents.After != "committed\n" {
			t.Fatalf("checkout affected committed contents: %+v, %v", contents, err)
		}
		patch, err := source.Patch(t.Context(), next.Mode, file, next.Head)
		if err != nil || !strings.Contains(patch.Patch, "-base\n+committed\n") {
			t.Fatalf("checkout affected committed patch: %+v, %v", patch, err)
		}
	}
}

func TestUncommittedAndCommittedRenameEndpoints(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "-c", "feature")
	testGit(t, root, "mv", "tracked", "committed-name")
	testGit(t, root, "commit", "-m", "committed rename")
	testGit(t, root, "mv", "committed-name", "staged-name")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		mode          review.DiffMode
		before, after string
	}{
		{review.DiffAll, "tracked", "staged-name"},
		{review.DiffCommitted, "tracked", "committed-name"},
		{review.DiffUncommitted, "committed-name", "staged-name"},
		{review.DiffStaged, "committed-name", "staged-name"},
	} {
		snapshot, err := source.Snapshot(t.Context(), want.mode)
		if err != nil || len(snapshot.Files) != 1 {
			t.Fatalf("%s: %+v, %v", want.mode, snapshot, err)
		}
		file := snapshot.Files[0]
		if file.Status != "R" || file.Path != want.after || file.OldPath == nil || *file.OldPath != want.before {
			t.Fatalf("%s rename: %+v", want.mode, file)
		}
		contents, err := source.Contents(t.Context(), want.mode, file, snapshot.Head)
		if err != nil || contents.Before != "base\n" || contents.After != "base\n" {
			t.Fatalf("%s renamed contents: %+v, %v", want.mode, contents, err)
		}
		patch, err := source.Patch(t.Context(), want.mode, file, snapshot.Head)
		if err != nil || !strings.Contains(patch.Patch, "rename from "+want.before+"\nrename to "+want.after) {
			t.Fatalf("%s renamed patch: %+v, %v", want.mode, patch, err)
		}
	}
}

func TestCommittedBinaryAndSubmoduleUseObjectIdentity(t *testing.T) {
	root, _ := committedScopeSource(t)
	writeScopeFile(t, root, "binary", "binary\x00committed")
	head := strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD"))
	testGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
	testGit(t, root, "add", "binary")
	testGit(t, root, "commit", "-m", "binary and gitlink")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Snapshot(t.Context(), review.DiffCommitted)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ContentHash(t.Context(), source, []review.RepositoryDiff{snapshot})
	if err != nil || identity == "" || len(snapshot.Files) != 3 {
		t.Fatalf("committed nontext identity: %q, files %+v, %v", identity, snapshot.Files, err)
	}
	for _, file := range snapshot.Files {
		if file.Path == "tracked" {
			continue
		}
		patch, err := source.Patch(t.Context(), snapshot.Mode, file, snapshot.Head)
		if err != nil || patch.Message == nil || patch.Contents != nil {
			t.Fatalf("%s preview: %+v, %v", file.Path, patch, err)
		}
		if file.Path == "binary" && !strings.HasPrefix(*patch.Message, "Binary") || file.Path == "module" && !strings.HasPrefix(*patch.Message, "Submodule") {
			t.Fatalf("%s preview message: %s", file.Path, *patch.Message)
		}
	}
}

func TestEveryBranchScopeHonorsCancellationAndMovingRefs(t *testing.T) {
	root, source := committedScopeSource(t)
	writeScopeFile(t, root, "tracked", "staged\n")
	testGit(t, root, "add", "tracked")
	writeScopeFile(t, root, "tracked", "working\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, mode := range source.Support().Scopes {
		snapshot, err := source.Snapshot(t.Context(), mode)
		if err != nil || len(snapshot.Files) != 1 {
			t.Fatalf("%s snapshot: %+v, %v", mode, snapshot, err)
		}
		file := snapshot.Files[0]
		if _, err := source.Snapshot(ctx, mode); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s snapshot cancellation: %v", mode, err)
		}
		if _, err := source.Patch(ctx, mode, file, snapshot.Head); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s patch cancellation: %v", mode, err)
		}
		if _, err := source.Contents(ctx, mode, file, snapshot.Head); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s contents cancellation: %v", mode, err)
		}
		if _, err := ContentHash(ctx, source, []review.RepositoryDiff{snapshot}); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s identity cancellation: %v", mode, err)
		}
	}
	testGit(t, root, "update-ref", "refs/heads/main", "HEAD")
	for _, mode := range source.Support().Scopes {
		_, err := source.Snapshot(t.Context(), mode)
		var problem *RequestError
		if !errors.As(err, &problem) || problem.Status != 409 {
			t.Fatalf("%s accepted moving baseline: %v", mode, err)
		}
	}
}

func TestHeadFallbackScopesAndUnbornContents(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "--initial-branch=custom")
	writeScopeFile(t, root, "new", "staged\n")
	testGit(t, root, "add", "new")
	writeScopeFile(t, root, "new", "working\n")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Support().Scopes) != 3 {
		t.Fatalf("unborn comparison scopes: %+v", source.Support())
	}
	for _, want := range []struct {
		mode          review.DiffMode
		kind          review.ComparisonKind
		before, after string
	}{
		{review.DiffAll, review.ComparisonUncommitted, "", "working\n"},
		{review.DiffStaged, review.ComparisonStaged, "", "staged\n"},
		{review.DiffUnstaged, review.ComparisonUnstaged, "staged\n", "working\n"},
	} {
		snapshot, err := source.Snapshot(t.Context(), want.mode)
		if err != nil || snapshot.Head != nil || snapshot.ComparisonKind != want.kind || len(snapshot.Files) != 1 {
			t.Fatalf("unborn %s snapshot: %+v, %v", want.mode, snapshot, err)
		}
		contents, err := source.Contents(t.Context(), want.mode, snapshot.Files[0], snapshot.Head)
		if err != nil || contents.Before != want.before || contents.After != want.after {
			t.Fatalf("unborn %s contents: %+v, %v", want.mode, contents, err)
		}
	}
	for _, mode := range []review.DiffMode{review.DiffUncommitted, review.DiffCommitted} {
		if _, err := source.Snapshot(t.Context(), mode); err == nil {
			t.Fatalf("unborn checkout fabricated %s scope", mode)
		}
	}
}

func TestContentHashPreservesLegacyBytesWithoutDescriptors(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "--initial-branch=main")
	source, err := OpenRepository(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []review.RepositoryDiff
	for _, mode := range source.Support().Scopes {
		snapshot, err := source.Snapshot(t.Context(), mode)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, snapshot)
	}
	described, err := ContentHash(t.Context(), source, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	for i := range snapshots {
		snapshots[i].ComparisonKind = ""
	}
	legacy, err := ContentHash(t.Context(), source, snapshots)
	// git-content-v1 with an empty index and all/staged/unstaged empty scopes.
	if err != nil || legacy != "f53c6e16cb7e540c485c9fe40d07739d84b4e533a613b6aa612700fbe0b64491" || described == legacy {
		t.Fatalf("legacy identity %q, described %q: %v", legacy, described, err)
	}
}
