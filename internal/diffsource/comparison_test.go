package diffsource

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/review"
)

func comparisonRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testGit(t, root, "init", "--initial-branch=main")
	testGit(t, root, "config", "user.name", "Test")
	testGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "initial")
	return root
}

func TestDefaultBaselinesUseRemoteHeadThenConventions(t *testing.T) {
	root := comparisonRepository(t)
	assertBaselines := func(label string, want ...string) {
		t.Helper()
		got, err := DefaultBaselines(t.Context(), root)
		if err != nil || strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%s: %q %v", label, got, err)
		}
	}
	assertBaselines("main fallback", "refs/heads/main")
	testGit(t, root, "branch", "trunk")
	testGit(t, root, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	testGit(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	assertBaselines("remote HEAD with local branch", "refs/heads/trunk", "refs/remotes/origin/trunk")
	testGit(t, root, "branch", "-D", "trunk")
	assertBaselines("remote HEAD without local branch", "refs/remotes/origin/trunk")
	testGit(t, root, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	testGit(t, root, "update-ref", "-d", "refs/remotes/origin/trunk")
	testGit(t, root, "branch", "-m", "main", "master")
	assertBaselines("master fallback", "refs/heads/master")
	testGit(t, root, "branch", "-m", "master", "custom")
	if _, err := DefaultBaselines(t.Context(), root); !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("missing default: %v", err)
	}
}

func TestDefaultBaselinesPreferDefaultBranchUpstream(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "remote", "add", "origin", "https://example.com/origin.git")
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, root, "remote", "add", "upstream", "https://example.com/upstream.git")
	testGit(t, root, "update-ref", "refs/remotes/upstream/main", "HEAD")
	testGit(t, root, "branch", "--set-upstream-to=upstream/main", "main")
	testGit(t, root, "switch", "-c", "feature")
	testGit(t, root, "branch", "--set-upstream-to=origin/main", "feature")
	got, err := DefaultBaselines(t.Context(), root)
	if err != nil || strings.Join(got, " ") != "refs/heads/main refs/remotes/upstream/main" {
		t.Fatalf("default branch upstream ignored: %q %v", got, err)
	}
}

func TestAutoComparisonUsesNearestDefaultBaseline(t *testing.T) {
	cases := []struct {
		name, want string
		arrange    func(t *testing.T, root string)
	}{
		{"branch from fetched remote", "refs/remotes/origin/main", func(t *testing.T, root string) {
			testGit(t, root, "switch", "-c", "remote")
			testGit(t, root, "commit", "--allow-empty", "-m", "upstream")
			testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
			testGit(t, root, "switch", "-c", "feature")
			testGit(t, root, "branch", "-D", "remote")
		}},
		{"branch from unpushed local", "refs/heads/main", func(t *testing.T, root string) {
			testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
			testGit(t, root, "commit", "--allow-empty", "-m", "unpushed")
			testGit(t, root, "switch", "-c", "feature")
		}},
		{"stale remote", "refs/heads/main", func(t *testing.T, root string) {
			testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
			testGit(t, root, "commit", "--allow-empty", "-m", "fetched then pulled")
			testGit(t, root, "switch", "-c", "feature")
		}},
		{"equal merge bases", "refs/heads/main", func(t *testing.T, root string) {
			testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
			testGit(t, root, "switch", "-c", "feature")
		}},
		{"diverged defaults, branch from remote", "refs/remotes/origin/main", func(t *testing.T, root string) {
			divergeDefaults(t, root)
			testGit(t, root, "switch", "-c", "feature", "refs/remotes/origin/main")
		}},
		{"diverged defaults, branch from local", "refs/heads/main", func(t *testing.T, root string) {
			divergeDefaults(t, root)
			testGit(t, root, "switch", "-c", "feature", "main")
		}},
		{"unrelated remote", "refs/heads/main", func(t *testing.T, root string) {
			testGit(t, root, "switch", "--orphan", "remote")
			testGit(t, root, "commit", "--allow-empty", "-m", "unrelated")
			testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
			testGit(t, root, "switch", "main")
			testGit(t, root, "branch", "-D", "remote")
			testGit(t, root, "switch", "-c", "feature")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := comparisonRepository(t)
			test.arrange(t, root)
			testGit(t, root, "commit", "--allow-empty", "-m", "feature")
			want := strings.TrimSpace(testGit(t, root, "merge-base", test.want, "HEAD"))
			for _, branch := range []string{"", "feature"} {
				source, err := OpenComparison(t.Context(), root, "auto", branch)
				if err != nil {
					t.Fatal(err)
				}
				comparison, ok := ComparisonInfo(source)
				if !ok || comparison.BaseRef != test.want || comparison.BaseOID != want {
					t.Fatalf("branch %q: %+v, want %s at %s", branch, comparison, test.want, want)
				}
			}
		})
	}
}

func divergeDefaults(t *testing.T, root string) {
	t.Helper()
	testGit(t, root, "switch", "-c", "remote")
	testGit(t, root, "commit", "--allow-empty", "-m", "upstream")
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, root, "switch", "main")
	testGit(t, root, "branch", "-D", "remote")
	testGit(t, root, "commit", "--allow-empty", "-m", "local")
}

func TestAutoComparisonUsesRemoteWithoutLocalDefault(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, root, "switch", "-c", "feature")
	testGit(t, root, "branch", "-D", "main")
	testGit(t, root, "commit", "--allow-empty", "-m", "feature")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if comparison, ok := ComparisonInfo(source); !ok || comparison.BaseRef != "refs/remotes/origin/main" {
		t.Fatalf("remote default ignored: %+v", comparison)
	}
}

func TestAutoComparisonDetectsMovedRemoteBaseline(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "switch", "-c", "feature")
	testGit(t, root, "commit", "--allow-empty", "-m", "upstream")
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	testGit(t, root, "commit", "--allow-empty", "-m", "feature")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	_, err = source.Snapshot(t.Context(), review.DiffAll)
	var problem *RequestError
	if !errors.As(err, &problem) || problem.Status != 409 {
		t.Fatalf("moved remote baseline accepted: %v", err)
	}
}

func TestComparisonFallbackDoesNotHideExplicitFailures(t *testing.T) {
	root := comparisonRepository(t)
	testGit(t, root, "branch", "-m", "main", "custom")
	source, err := OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, comparison := ComparisonInfo(source); comparison {
		t.Fatal("missing automatic baseline did not fall back to checkout")
	}
	if _, err := OpenComparison(t.Context(), root, "missing", ""); err == nil {
		t.Fatal("explicit absent base silently fell back")
	}
	if _, err := OpenComparison(t.Context(), root, "auto", "custom"); !errors.Is(err, ErrNoDefaultBranch) {
		t.Fatalf("object recovery fell back to unrelated checkout: %v", err)
	}
	if _, err := OpenComparison(t.Context(), root, "custom", "missing"); err == nil {
		t.Fatal("missing recovery branch accepted")
	}
	unborn := t.TempDir()
	testGit(t, unborn, "init", "--initial-branch=main")
	source, err = OpenComparison(t.Context(), unborn, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := source.Snapshot(t.Context(), review.DiffAll); err != nil || snapshot.Head != nil || len(snapshot.Files) != 0 {
		t.Fatalf("unborn automatic fallback: %+v %v", snapshot, err)
	}
}

func TestComparisonDetectsMovedRefsAndSameHeadBranchSwitch(t *testing.T) {
	for _, mutate := range []string{"target", "base", "branch"} {
		t.Run(mutate, func(t *testing.T) {
			root := comparisonRepository(t)
			main := strings.TrimSpace(testGit(t, root, "rev-parse", "HEAD"))
			testGit(t, root, "switch", "-c", "feature")
			testGit(t, root, "commit", "--allow-empty", "-m", "new head")
			source, err := OpenComparison(t.Context(), root, "main", "")
			if err != nil {
				t.Fatal(err)
			}
			switch mutate {
			case "target":
				testGit(t, root, "update-ref", "refs/heads/feature", main)
			case "base":
				testGit(t, root, "update-ref", "refs/heads/main", "HEAD")
			case "branch":
				testGit(t, root, "switch", "-c", "other")
			}
			_, err = source.Snapshot(t.Context(), review.DiffAll)
			var problem *RequestError
			if !errors.As(err, &problem) || problem.Status != 409 {
				t.Fatalf("moved comparison accepted: %v", err)
			}
		})
	}
}

func TestComparisonUnavailableAfterCapturedRefsDisappear(t *testing.T) {
	for _, removed := range []string{"base", "target", "default"} {
		t.Run(removed, func(t *testing.T) {
			root := comparisonRepository(t)
			testGit(t, root, "branch", "feature")
			base := "main"
			if removed == "default" {
				base = "auto"
			}
			source, err := OpenComparison(t.Context(), root, base, "feature")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.Snapshot(t.Context(), review.DiffAll); err != nil {
				t.Fatal(err)
			}
			if removed == "target" {
				testGit(t, root, "branch", "-D", "feature")
			} else {
				testGit(t, root, "switch", "feature")
				testGit(t, root, "branch", "-D", "main")
			}
			_, err = OpenComparison(t.Context(), root, base, "feature")
			if !errors.Is(err, ErrComparisonUnavailable) {
				t.Fatalf("missing %s ref cannot replay saved payload: %v", removed, err)
			}
		})
	}
}

func TestComparisonProcessFailuresAreNotUnavailable(t *testing.T) {
	root := comparisonRepository(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := OpenComparison(ctx, root, "main", "main"); !errors.Is(err, context.Canceled) || errors.Is(err, ErrComparisonUnavailable) {
		t.Fatalf("cancellation became unavailable ref: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Git wrapper requires a POSIX shell")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
case " $* " in
  *" rev-parse --verify "*) exit 7 ;;
esac
exec "$DIFFX_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, base := range []string{"main", "auto"} {
		if _, err := OpenComparison(t.Context(), root, base, "main"); err == nil || errors.Is(err, ErrComparisonUnavailable) {
			t.Fatalf("unexpected Git exit became unavailable ref for %s: %v", base, err)
		}
	}
}
