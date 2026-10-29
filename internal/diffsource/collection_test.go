package diffsource

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
)

func TestPatchFileBatchesPreserveFilesAndRenamePairsWithinNativeLimits(t *testing.T) {
	var files []review.ChangedFile
	for index := range 1000 {
		previous := fmt.Sprintf("%04d-%s-old", index, strings.Repeat("a", 180))
		files = append(files, review.ChangedFile{Path: fmt.Sprintf("%04d-%s-new", index, strings.Repeat("b", 180)), OldPath: &previous})
	}
	var flattened []review.ChangedFile
	for _, batch := range patchFileBatches(files) {
		bytes := 0
		for _, file := range batch {
			bytes += len(file.Path) + 1
			if file.OldPath != nil {
				bytes += len(*file.OldPath) + 1
			}
		}
		if bytes > patchPathspecBytes {
			t.Fatalf("native pathspec budget exceeded: %d", bytes)
		}
		flattened = append(flattened, batch...)
	}
	if !reflect.DeepEqual(flattened, files) {
		t.Fatal("batching changed file order or rename pairs")
	}
}

func TestCollectionLongPathBatchesPreserveScalarPreviews(t *testing.T) {
	root := comparisonRepository(t)
	for index := range 100 {
		writeScopeFile(t, root, fmt.Sprintf("%03d-%s", index, strings.Repeat("a", 180)), fmt.Sprintf("before %d\n", index))
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "long paths")
	for index := range 100 {
		writeScopeFile(t, root, fmt.Sprintf("%03d-%s", index, strings.Repeat("a", 180)), fmt.Sprintf("after %d\n", index))
	}
	source, err := OpenComparison(t.Context(), root, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	assertBulkParity(t, source)
}

func assertBulkParity(t *testing.T, source Source) {
	t.Helper()
	var snapshots []review.RepositoryDiff
	sources := make(map[string]Source)
	for _, mode := range source.Support().Scopes {
		snapshot, err := source.Snapshot(t.Context(), mode)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, snapshot)
		sources[snapshot.Revision] = source
	}
	listed, ok, err := Commits(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	// Compare batch metadata with independently read scalar snapshots, including
	// fingerprints and revisions, before installing the immutable batch cache.
	if ok {
		for _, commit := range listed.Commits {
			if commit.Merge() {
				continue
			}
			target, err := CommitSource(t.Context(), source, commit)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := target.Snapshot(t.Context(), review.DiffCommit)
			if err != nil {
				t.Fatal(err)
			}
			snapshots = append(snapshots, snapshot)
			sources[snapshot.Revision] = target
		}
		if err := PrepareCommitSnapshots(t.Context(), source, listed.Commits); err != nil {
			t.Fatal(err)
		}
		for _, snapshot := range snapshots {
			if snapshot.Mode != review.DiffCommit {
				continue
			}
			captured, err := sources[snapshot.Revision].Snapshot(t.Context(), snapshot.Mode)
			if err != nil || !reflect.DeepEqual(captured, snapshot) {
				t.Fatalf("batch manifest differs:\ngot %+v\nwant %+v\n%v", captured, snapshot, err)
			}
		}
	}
	reader := NewCollectionPreviews(source)
	if err := reader.PreparePatches(t.Context(), snapshots, 1<<20, 63<<20); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		for _, file := range snapshot.Files {
			scalar, err := sources[snapshot.Revision].Patch(t.Context(), snapshot.Mode, file, snapshot.Head)
			if err != nil {
				t.Fatal(err)
			}
			bulk, err := reader.Patch(t.Context(), snapshot, file)
			if err != nil || !reflect.DeepEqual(bulk, scalar) {
				t.Fatalf("%s %q patch:\ngot %+v\nwant %+v\n%v", snapshot.Mode, file.Path, bulk, scalar, err)
			}
			// Contents of non-text previews are intentionally not requested by collector.
			if file.Binary || file.Status == "U" || scalar.Message != nil && strings.HasPrefix(*scalar.Message, "Submodule") {
				continue
			}
			before, scalarErr := sources[snapshot.Revision].Contents(t.Context(), snapshot.Mode, file, snapshot.Head)
			after, bulkErr := reader.Contents(t.Context(), snapshot, file)
			if PreviewUnavailable(scalarErr) && PreviewUnavailable(bulkErr) {
				continue
			}
			if scalarErr != nil || bulkErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("%s %q contents: got %+v (%v), want %+v (%v)", snapshot.Mode, file.Path, after, bulkErr, before, scalarErr)
			}
		}
	}
}

func TestCollectionPreviewsMatchScalarGitSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX filenames and symlinks")
	}
	root := comparisonRepository(t)
	names := []string{"space name", "tab\tname", "line\nname", "quote\"name", "back\\name", "unicodé", "executable", "type", "rename old", "delete", "recreated", "empty"}
	for _, name := range names {
		writeScopeFile(t, root, name, "original "+name+"\n")
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "fixture base")
	testGit(t, root, "switch", "-c", "feature")
	for _, name := range names {
		if name != "empty" {
			writeScopeFile(t, root, name, "committed "+name+"\n")
		}
	}
	if err := os.Chmod(filepath.Join(root, "executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "type")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "type")); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "mv", "rename old", "rename new")
	testGit(t, root, "rm", "-f", "delete")
	writeScopeFile(t, root, "binary", "nul\x00bytes")
	writeScopeFile(t, root, ".gitattributes", "forced-text diff\n")
	writeScopeFile(t, root, "forced-text", "forced\x00text\n")
	head := strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD"))
	testGit(t, root, "add", ".")
	testGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
	testGit(t, root, "commit", "-m", "rich commit")
	testGit(t, root, "commit", "--allow-empty", "-m", "empty commit")
	testGit(t, root, "mv", "rename new", "staged rename")
	testGit(t, root, "rm", "--cached", "recreated")
	writeScopeFile(t, root, "recreated", "recreated after removal\n")
	writeScopeFile(t, root, "space name", "staged value\n")
	testGit(t, root, "add", "space name")
	writeScopeFile(t, root, "space name", "working value\n")
	writeScopeFile(t, root, "untracked\nname", "new value\n")
	head = strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD"))
	testGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
	testGit(t, root, "config", "diff.submodule", "log")
	for _, branch := range []string{"", "feature"} {
		t.Run("branch="+branch, func(t *testing.T) {
			source, err := OpenComparison(t.Context(), root, "auto", branch)
			if err != nil {
				t.Fatal(err)
			}
			assertBulkParity(t, source)
		})
	}
}

func TestCollectionPreviewsUnbornAndConflicts(t *testing.T) {
	t.Run("unborn", func(t *testing.T) {
		root := t.TempDir()
		testGit(t, root, "init", "-b", "custom")
		writeScopeFile(t, root, "new", "staged\n")
		testGit(t, root, "add", ".")
		writeScopeFile(t, root, "new", "working\n")
		writeScopeFile(t, root, "other", "untracked\n")
		source, err := OpenRepository(t.Context(), root)
		if err != nil {
			t.Fatal(err)
		}
		assertBulkParity(t, source)
	})
	t.Run("conflict", func(t *testing.T) {
		root := comparisonRepository(t)
		testGit(t, root, "switch", "-c", "feature")
		commitFile(t, root, "tracked", "feature\n", "feature")
		base := strings.TrimSpace(testGit(t, root, "rev-parse", "main:tracked"))
		ours := strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD:tracked"))
		_, err := runGitInput(t.Context(), root, 1024, strings.NewReader(fmt.Sprintf("0 %s\ttracked\n100644 %s 1\ttracked\n100644 %s 2\ttracked\n100644 %s 3\ttracked\n", strings.Repeat("0", 40), base, ours, base)), "update-index", "--index-info")
		if err != nil {
			t.Fatal(err)
		}
		source, err := OpenComparison(t.Context(), root, "auto", "")
		if err != nil {
			t.Fatal(err)
		}
		assertBulkParity(t, source)
	})
}

func TestCollectionPatchLimitContinuesAfterOversizedFile(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "-c", "feature")
	writeScopeFile(t, root, "a-large", strings.Repeat("x", 3<<20)+"\n")
	writeScopeFile(t, root, "z-later", "later\n")
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "large and small")
	source, listed := branchCommits(t, root)
	if err := PrepareCommitSnapshots(t.Context(), source, listed.Commits); err != nil {
		t.Fatal(err)
	}
	target, err := CommitSource(t.Context(), source, listed.Commits[0])
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := target.Snapshot(t.Context(), review.DiffCommit)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewCollectionPreviews(source)
	if err := reader.PreparePatches(t.Context(), []review.RepositoryDiff{snapshot}, 1<<20, 63<<20); err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		patch, err := reader.Patch(t.Context(), snapshot, file)
		if err != nil {
			t.Fatal(err)
		}
		if file.Path == "a-large" && (patch.Patch != "" || patch.Message == nil || *patch.Message != "Diff exceeds the 2 MiB preview limit.") {
			t.Fatalf("large patch: %+v", patch)
		}
		if file.Path == "z-later" && !strings.Contains(patch.Patch, "+later\n") {
			t.Fatalf("lost later patch: %+v", patch)
		}
	}
	if reader.patchBytes > reader.patchBudget {
		t.Fatal("patch cache exceeds bound")
	}
	_, err = reader.Contents(t.Context(), snapshot, snapshot.Files[0])
	if !PreviewUnavailable(err) {
		t.Fatalf("oversize blob must be skipped by size: %v", err)
	}
	contents, err := reader.Contents(t.Context(), snapshot, snapshot.Files[len(snapshot.Files)-1])
	if err != nil || contents.After != "later\n" {
		t.Fatalf("later contents: %+v %v", contents, err)
	}
}

func TestCollectionBulkCancellationKillsAndJoinsProcessTree(t *testing.T) {
	root := installGitTestHelper(t)
	t.Setenv("DIFFX_GIT_TEST_HELPER", "hang")
	source := &gitSource{root: root, collection: newCollectionObjects()}
	snapshot := review.RepositoryDiff{Mode: review.DiffAll, Head: Message(strings.Repeat("a", 40)), Files: []review.ChangedFile{{ID: "file", Path: "file", Fingerprint: "fingerprint", Status: "M"}}}
	reader := NewCollectionPreviews(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- reader.PreparePatches(ctx, []review.RepositoryDiff{snapshot}, 1<<20, 63<<20) }()
	ready := waitGitHelperReady(t, root)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("batch parser/process failed to join")
	}
	assertGitHelperStopped(t, ready)
	if len(gitProcesses.active) != 0 || len(gitProcesses.admitted) != 0 {
		t.Fatal("batch retained Git admission")
	}
}

func TestCollectionBlobCacheEvictsWithoutChangingContents(t *testing.T) {
	root := comparisonRepository(t)
	for index := range 20 {
		writeScopeFile(t, root, fmt.Sprintf("file-%02d", index), "before "+strings.Repeat("b", (1<<20)-20))
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "base blobs")
	testGit(t, root, "switch", "-c", "feature")
	for index := range 20 {
		writeScopeFile(t, root, fmt.Sprintf("file-%02d", index), fmt.Sprintf("after %d ", index)+strings.Repeat("a", (1<<20)-20))
	}
	testGit(t, root, "commit", "-am", "large blobs")
	source, err := OpenComparison(t.Context(), root, "auto", "feature")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Snapshot(t.Context(), review.DiffAll)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewCollectionPreviews(source)
	files := append(append([]review.ChangedFile{}, snapshot.Files...), snapshot.Files[0])
	for _, file := range files {
		got, err := reader.Contents(t.Context(), snapshot, file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := source.Contents(t.Context(), snapshot.Mode, file, snapshot.Head)
		if err != nil || got != want {
			t.Fatalf("blob changed after eviction for %q: %v", file.Path, err)
		}
		if reader.blobBytes > collectionBlobBytes {
			t.Fatalf("blob cache retained %d bytes", reader.blobBytes)
		}
	}
}

func TestCollectionFrameLimitKillsContinuousWriter(t *testing.T) {
	root := installGitTestHelper(t)
	t.Setenv("DIFFX_GIT_TEST_HELPER", "continuous-output")
	started := time.Now()
	err := runGitStream(t.Context(), root, nil, func(reader *bufio.Reader) error { _, err := readDelimited(reader, 0, 1024); return err }, "ignored")
	if !errors.Is(err, errOutputLimit) {
		t.Fatalf("framing limit lost: %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("malformed batch retained child until timeout")
	}
	if len(gitProcesses.active) != 0 || len(gitProcesses.admitted) != 0 {
		t.Fatal("malformed batch retained admission")
	}
}

func TestCollectionRootCommitMatchesScalar(t *testing.T) {
	root, source := committedScopeSource(t)
	oid := strings.TrimSpace(testGit(t, root, "rev-list", "--max-parents=0", "HEAD"))
	commit := review.Commit{OID: oid, Parents: []string{}}
	target, err := CommitSource(t.Context(), source, commit)
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := target.Snapshot(t.Context(), review.DiffCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := PrepareCommitSnapshots(t.Context(), source, []review.Commit{commit}); err != nil {
		t.Fatal(err)
	}
	bulk, err := target.Snapshot(t.Context(), review.DiffCommit)
	if err != nil || !reflect.DeepEqual(bulk, scalar) {
		t.Fatalf("root snapshot changed: %+v, %+v, %v", bulk, scalar, err)
	}
	reader := NewCollectionPreviews(source)
	if err := reader.PreparePatches(t.Context(), []review.RepositoryDiff{bulk}, 1<<20, 63<<20); err != nil {
		t.Fatal(err)
	}
	for _, file := range bulk.Files {
		got, err := reader.Patch(t.Context(), bulk, file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := target.Patch(t.Context(), scalar.Mode, file, scalar.Head)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("root patch changed: %+v, %+v, %v", got, want, err)
		}
	}
}
