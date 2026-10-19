package collector

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/diffsource"
)

func activityTime(t *testing.T, path string, timestamp time.Time) {
	t.Helper()
	if err := os.Chtimes(path, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionActivityIncludesCommitAndCheckoutChanges(t *testing.T) {
	committed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Setenv("GIT_AUTHOR_DATE", committed.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", committed.Format(time.RFC3339))
	root := repository(t)
	write(t, root, "tracked", "base\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	git(t, root, "switch", "-c", "feature")
	assertActivity := func(want time.Time) {
		t.Helper()
		request := collect(t, root)
		if request.Metadata.ActivityAt != want.UnixMilli() {
			t.Fatalf("activity %d, want %d", request.Metadata.ActivityAt, want.UnixMilli())
		}
	}
	assertActivity(committed)
	write(t, root, "tracked", "edited\n")
	edited := committed.Add(time.Hour)
	activityTime(t, filepath.Join(root, "tracked"), edited)
	assertActivity(edited)
	git(t, root, "add", ".")
	_, gitDir, err := diffsource.RepositoryIdentity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	staged := edited.Add(time.Hour)
	activityTime(t, filepath.Join(gitDir, "index"), staged)
	assertActivity(staged)
	assertActivity(staged) // Collection must not refresh staged index activity.
	git(t, root, "mv", "tracked", "renamed")
	renamed := staged.Add(time.Hour)
	activityTime(t, filepath.Join(root, "renamed"), renamed)
	activityTime(t, root, renamed)
	activityTime(t, filepath.Join(gitDir, "index"), staged)
	assertActivity(renamed)
	if err := os.Remove(filepath.Join(root, "renamed")); err != nil {
		t.Fatal(err)
	}
	deleted := renamed.Add(time.Hour)
	activityTime(t, root, deleted)
	assertActivity(deleted)
}

func TestObjectCollectionActivityUsesCapturedTargetCommit(t *testing.T) {
	target := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Setenv("GIT_AUTHOR_DATE", target.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", target.Format(time.RFC3339))
	root := branchRepository(t)
	git(t, root, "switch", "main")
	other := target.Add(24 * time.Hour)
	t.Setenv("GIT_AUTHOR_DATE", other.Format(time.RFC3339))
	t.Setenv("GIT_COMMITTER_DATE", other.Format(time.RFC3339))
	git(t, root, "commit", "--allow-empty", "-m", "new unrelated HEAD")
	write(t, root, "tracked", "unrelated staged\n")
	git(t, root, "add", ".")
	write(t, root, "tracked", "unrelated unstaged\n")
	write(t, root, "untracked", "unrelated\n")
	options := branchOptions()
	options.Branch = "feature"
	request, err := Collect(t.Context(), root, options)
	if err != nil || request.Metadata.ActivityAt != target.UnixMilli() {
		t.Fatalf("target activity %d, want %d: %v", request.Metadata.ActivityAt, target.UnixMilli(), err)
	}
}

func TestUnbornCollectionActivityUsesUntrackedFiles(t *testing.T) {
	root := repository(t)
	if got := collect(t, root).Metadata.ActivityAt; got != 0 {
		t.Fatalf("empty unborn checkout activity: %d", got)
	}
	write(t, root, "untracked", "new\n")
	edited := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	activityTime(t, filepath.Join(root, "untracked"), edited)
	if got := collect(t, root).Metadata.ActivityAt; got != edited.UnixMilli() {
		t.Fatalf("untracked unborn activity: %d, want %d", got, edited.UnixMilli())
	}
}

func TestActivityFailureDoesNotDiscardValidDiff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git wrapper requires POSIX shell")
	}
	root := branchRepository(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
case " $* " in
  *" --format=%ct "*) exit 9 ;;
esac
exec "$DIFFX_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	request := collect(t, root)
	if request.Metadata.ActivityAt != 0 || len(request.Scopes[0].Snapshot.Files) != 1 {
		t.Fatalf("advisory metadata failure lost diff: %+v", request)
	}
}
