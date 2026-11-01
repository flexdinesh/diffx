package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotTestRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initializeSnapshotGit(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	return root
}

func snapshotTestWrite(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshotTestCommit(t *testing.T, root string) {
	t.Helper()
	if _, err := snapshotGit(t.Context(), root, "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotGit(t.Context(), root, "-c", "user.name=Test", "-c", "user.email=test@localhost", "-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "Change"); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedSnapshotIndependentAndComplete(t *testing.T) {
	root := snapshotTestRepository(t)
	snapshotTestWrite(t, root, ".gitattributes", "source.txt export-ignore\n")
	snapshotTestWrite(t, root, "run.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(root, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	snapshotTestCommit(t, root)
	snapshot, cleanup, err := captureSnapshot(t.Context(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if data, err := os.ReadFile(filepath.Join(snapshot.Root, "source.txt")); err != nil || string(data) != "committed\n" {
		t.Fatalf("export-ignore lost source: %q, %v", data, err)
	}
	if target, err := os.Readlink(filepath.Join(snapshot.Root, "link")); err != nil || target != "source.txt" {
		t.Fatalf("symlink: %q, %v", target, err)
	}
	if info, err := os.Stat(filepath.Join(snapshot.Root, "run.sh")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable mode lost: %v", err)
	}
	common, err := snapshotGit(t.Context(), snapshot.Root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || strings.TrimSpace(string(common)) != filepath.Join(snapshot.Root, ".git") {
		t.Fatalf("snapshot points outside itself: %q, %v", common, err)
	}
	snapshotTestWrite(t, root, "source.txt", "host edited\n")
	if data, _ := os.ReadFile(filepath.Join(snapshot.Root, "source.txt")); string(data) != "committed\n" {
		t.Fatal("host mutation changed snapshot")
	}
	if err := checkSnapshotCurrent(t.Context(), root, snapshot, true); err == nil {
		t.Fatal("dirty source still certified")
	}
	snapshotTestWrite(t, snapshot.Root, "source.txt", "generated drift\n")
	if status, err := snapshotGit(t.Context(), snapshot.Root, "status", "--porcelain"); err != nil || !strings.Contains(string(status), "source.txt") {
		t.Fatalf("synthetic Git does not detect generated drift: %q, %v", status, err)
	}
}

func TestCommittedSnapshotRejectsUntrackedFiles(t *testing.T) {
	root := snapshotTestRepository(t)
	snapshotTestWrite(t, root, "untracked.txt", "new code")
	_, cleanup, err := captureSnapshot(t.Context(), root, true)
	defer cleanup()
	if err == nil {
		t.Fatal("accepted untracked code")
	}
}

func TestWorkingSnapshotIncludesChangesAndExcludesIgnored(t *testing.T) {
	root := snapshotTestRepository(t)
	snapshotTestWrite(t, root, ".gitignore", "ignored/\n")
	snapshotTestCommit(t, root)
	snapshotTestWrite(t, root, "source.txt", "working content")
	snapshotTestWrite(t, root, "new.txt", "untracked content")
	snapshotTestWrite(t, root, "ignored/output", "ignored content")
	snapshot, cleanup, err := captureSnapshot(t.Context(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for name, want := range map[string]string{"source.txt": "working content", "new.txt": "untracked content"} {
		if data, err := os.ReadFile(filepath.Join(snapshot.Root, name)); err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(snapshot.Root, "ignored")); !os.IsNotExist(err) {
		t.Fatal("ignored files imported")
	}
	if err := checkSnapshotCurrent(t.Context(), root, snapshot, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "source.txt")); err != nil {
		t.Fatal(err)
	}
	if err := checkSnapshotCurrent(t.Context(), root, snapshot, false); err == nil {
		t.Fatal("deletion did not invalidate snapshot")
	}
}

func TestSnapshotIdentitySurvivesHistoryOnlyChange(t *testing.T) {
	root := snapshotTestRepository(t)
	before, cleanup, err := captureSnapshot(t.Context(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	snapshotTestCommit(t, root)
	after, cleanupAfter, err := captureSnapshot(t.Context(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupAfter()
	if before.Commit == after.Commit || before.Identity != after.Identity {
		t.Fatalf("history-only change: before=%+v after=%+v", before, after)
	}
	if err := checkSnapshotCurrent(t.Context(), root, before, true); err == nil {
		t.Fatal("HEAD mutation during validation must prevent certification")
	}
	beforeGit, _ := snapshotGit(t.Context(), before.Root, "rev-parse", "HEAD")
	afterGit, _ := snapshotGit(t.Context(), after.Root, "rev-parse", "HEAD")
	if string(beforeGit) != string(afterGit) {
		t.Fatal("synthetic Git baseline depends on source history")
	}
	beforeMetadata, err := fingerprintFiles(filepath.Join(before.Root, ".git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	afterMetadata, err := fingerprintFiles(filepath.Join(after.Root, ".git"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if beforeMetadata != afterMetadata {
		t.Fatal("synthetic Git metadata includes invocation-specific content")
	}
}

func TestFingerprintIncludesPathModeSymlinkAndContents(t *testing.T) {
	root := t.TempDir()
	snapshotTestWrite(t, root, "source", "first")
	first, err := fingerprintFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshotTestWrite(t, root, ".git/config", "ignored metadata")
	unchanged, _ := fingerprintFiles(root, nil)
	if first != unchanged {
		t.Fatal("Git metadata affected content identity")
	}
	for _, change := range []func() error{
		func() error { return os.Chmod(filepath.Join(root, "source"), 0o755) },
		func() error { return os.Rename(filepath.Join(root, "source"), filepath.Join(root, "renamed")) },
		func() error { return os.WriteFile(filepath.Join(root, "renamed"), []byte("second"), 0o755) },
		func() error { return os.Symlink("renamed", filepath.Join(root, "link")) },
	} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		next, err := fingerprintFiles(root, nil)
		if err != nil || next == first {
			t.Fatalf("change did not invalidate: %v", err)
		}
		first = next
	}
}
