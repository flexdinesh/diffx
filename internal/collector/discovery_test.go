package collector

import (
	"context"
	"errors"
	"fmt"
	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func committed(t *testing.T, root string) string {
	t.Helper()
	write(t, root, "file.txt", "before\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	return root
}

func initialized(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "--initial-branch=main")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.com")
	return committed(t, root)
}

func discoveredPaths(result Discovery) []string {
	paths := make([]string, 0, len(result.Sources))
	for _, source := range result.Sources {
		paths = append(paths, source.Path)
	}
	return paths
}

func TestDiscoveryFindsRepositoriesAndAllTheirWorktrees(t *testing.T) {
	workspace := t.TempDir()
	root := initialized(t, filepath.Join(workspace, "group", "project"))
	// Linked worktrees outside the scanned workspace still belong to the repository.
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{root, linked}) {
		t.Fatalf("sources %v diagnostics %v err %v", discoveredPaths(result), result.Diagnostics, err)
	}
	if result.Sources[0].InputPath != workspace || result.Sources[0].Identity == result.Sources[1].Identity {
		t.Fatalf("unexpected sources: %#v", result.Sources)
	}
	identity := result.Sources[1].Identity
	git(t, root, "branch", "-m", "main", "master")
	result, err = DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || len(result.Sources) != 2 || result.Sources[1].Identity != identity {
		t.Fatalf("worktree identity changed: %#v %v %v", result.Sources, result.Diagnostics, err)
	}
}

func TestDiscoveryWithoutLinkedWorktreesSelectsOnlyTheContainingCheckout(t *testing.T) {
	root := initialized(t, filepath.Join(t.TempDir(), "project"))
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	nested := filepath.Join(linked, "src")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{root: root, nested: linked} {
		result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{input}, Depth: 8}, "machine", "auto")
		if err != nil || !slices.Equal(discoveredPaths(result), []string{want}) || result.Sources[0].InputPath != input {
			t.Fatalf("input %s: sources %v diagnostics %v err %v", input, discoveredPaths(result), result.Diagnostics, err)
		}
	}
}

func TestDiscoveryStopsAtRepositoriesAndSkipsHiddenDirectories(t *testing.T) {
	workspace := t.TempDir()
	outer := initialized(t, filepath.Join(workspace, "outer"))
	initialized(t, filepath.Join(outer, "nested"))
	initialized(t, filepath.Join(workspace, ".hidden", "project"))
	initialized(t, filepath.Join(workspace, "node_modules", "package"))
	visible := initialized(t, filepath.Join(workspace, "visible"))
	if err := os.Symlink(filepath.Join(workspace, ".hidden"), filepath.Join(workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{outer, visible}) {
		t.Fatalf("sources %v diagnostics %v err %v", discoveredPaths(result), result.Diagnostics, err)
	}
	hidden := filepath.Join(workspace, ".hidden")
	result, err = DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{hidden}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{filepath.Join(hidden, "project")}) {
		t.Fatalf("explicit hidden root skipped: %v %v %v", discoveredPaths(result), result.Diagnostics, err)
	}
}

func TestDiscoveryResolvesNestedPathAndDeduplicatesOverlappingInputs(t *testing.T) {
	workspace := t.TempDir()
	root := initialized(t, filepath.Join(workspace, "project"))
	nested := filepath.Join(root, "src")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{nested, workspace, root}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{root}) || result.Sources[0].InputPath != nested {
		t.Fatalf("overlapping inputs: %#v %v %v", result.Sources, result.Diagnostics, err)
	}
}

func TestDiscoveryIncludesIndependentClonesWithSameRemote(t *testing.T) {
	original := committed(t, repository(t))
	workspace := t.TempDir()
	first, second := filepath.Join(workspace, "first"), filepath.Join(workspace, "second")
	git(t, workspace, "clone", original, first)
	git(t, workspace, "clone", original, second)
	result, err := DiscoverWorkspaces(context.Background(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{first, second}) || result.Sources[0].Identity == result.Sources[1].Identity {
		t.Fatalf("clones lost distinct checkout identities: %#v %v %v", result.Sources, result.Diagnostics, err)
	}
}

func TestDiscoveryReportsCopiedGitIdentity(t *testing.T) {
	workspace := t.TempDir()
	original := committed(t, repository(t))
	if _, err := SourceIdentity(t.Context(), original, "machine", "", "auto"); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(workspace, "first"), filepath.Join(workspace, "second")
	if err := os.CopyFS(first, os.DirFS(original)); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(second, os.DirFS(original)); err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || len(result.Sources) != 1 {
		t.Fatalf("copied identity discovery: %#v %v %v", result.Sources, result.Diagnostics, err)
	}
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic, "identity copied") && strings.Contains(diagnostic, first) && strings.Contains(diagnostic, second) {
			return
		}
	}
	t.Fatalf("copied identities silently merged: %v", result.Diagnostics)
}

func TestDiscoveryReportsMissingWorktree(t *testing.T) {
	root := committed(t, repository(t))
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverWorkspaces(t.Context(), Workspaces{Paths: []string{root}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{root}) || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0], "worktree unavailable") {
		t.Fatalf("missing worktree: %v %v %v", discoveredPaths(result), result.Diagnostics, err)
	}
}

func TestDiscoveryBoundsDepthAndReportsMissingInput(t *testing.T) {
	workspace := t.TempDir()
	deep := initialized(t, filepath.Join(workspace, "one", "two", "three"))
	result, err := DiscoverWorkspaces(context.Background(), Workspaces{Paths: []string{workspace}, Depth: 2, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || len(result.Sources) != 0 {
		t.Fatalf("deep checkout escaped discovery bound: %#v %v", result, err)
	}
	result, err = DiscoverWorkspaces(context.Background(), Workspaces{Paths: []string{workspace}, Depth: 3, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{deep}) {
		t.Fatalf("checkout within depth missed: %#v %v", result, err)
	}
	result, err = DiscoverWorkspaces(context.Background(), Workspaces{Paths: []string{filepath.Join(workspace, "missing")}, Depth: 8, LinkedWorktrees: true}, "machine", "auto")
	if err != nil || len(result.Sources) != 0 || len(result.Diagnostics) == 0 {
		t.Fatalf("missing path lacks diagnostics: %#v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverWorkspaces(ctx, Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, "machine", "auto"); err == nil {
		t.Fatal("canceled discovery succeeded")
	}
}

func TestCollectWorkspacesCollectsEachCheckoutIndependently(t *testing.T) {
	workspace := t.TempDir()
	first := initialized(t, filepath.Join(workspace, "first"))
	second := initialized(t, filepath.Join(workspace, "second"))
	write(t, first, "file.txt", "after\n")
	write(t, second, "file.txt", "after\n")
	git(t, second, "add", ".")
	var requests []ingestion.Request
	batch, err := CollectWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, Options{SourceID: "machine", Hostname: "host", SubmissionID: "shared"}, func(input ingestion.Request) error { requests = append(requests, input); return nil })
	slices.SortFunc(requests, func(a, b ingestion.Request) int { return strings.Compare(a.Metadata.Root, b.Metadata.Root) })
	if err != nil || len(requests) != 2 || len(batch.Failures) != 0 {
		t.Fatalf("batch %#v err %v", batch, err)
	}
	if requests[0].Metadata.Root != first || requests[1].Metadata.Root != second {
		t.Fatalf("unexpected roots: %s %s", requests[0].Metadata.Root, requests[1].Metadata.Root)
	}
	if requests[0].SubmissionID == requests[1].SubmissionID || requests[0].SubmissionID == "shared" {
		t.Fatal("checkouts share a submission ID")
	}
	if requests[0].Metadata.TriggerRoot != workspace {
		t.Fatalf("trigger root %q", requests[0].Metadata.TriggerRoot)
	}
	if _, err := CollectWorkspaces(t.Context(), Workspaces{Paths: []string{t.TempDir()}, Depth: 8, LinkedWorktrees: true}, Options{SourceID: "machine", Hostname: "host"}, func(ingestion.Request) error { return nil }); err == nil || !strings.Contains(err.Error(), "no Git checkouts found") {
		t.Fatalf("empty workspace: %v", err)
	}
}

func TestCollectWorkspacesCanonicalizesInputWithinItsCheckout(t *testing.T) {
	root := initialized(t, filepath.Join(t.TempDir(), "project"))
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(linked, alias); err != nil {
		t.Fatal(err)
	}
	var requests []ingestion.Request
	if _, err := CollectWorkspaces(t.Context(), Workspaces{Paths: []string{alias}, Depth: 8, LinkedWorktrees: true}, Options{SourceID: "machine", Hostname: "host"}, func(input ingestion.Request) error { requests = append(requests, input); return nil }); err != nil || len(requests) != 2 {
		t.Fatalf("requests %d err %v", len(requests), err)
	}
	// Canonical checkout roots must contain the input to identify the launch checkout.
	for _, request := range requests {
		if request.Metadata.TriggerRoot != linked {
			t.Fatalf("symlinked input %q not canonical", request.Metadata.TriggerRoot)
		}
	}
}

func TestCollectWorkspacesReportsFailedCheckoutWithoutBlockingOthers(t *testing.T) {
	workspace := t.TempDir()
	healthy := initialized(t, filepath.Join(workspace, "healthy"))
	broken := initialized(t, filepath.Join(workspace, "broken"))
	write(t, healthy, "file.txt", "after\n")
	// A base that cannot resolve fails only the checkout without that ref.
	git(t, healthy, "branch", "baseline")
	var requests []ingestion.Request
	batch, err := CollectWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, Options{SourceID: "machine", Hostname: "host", Base: "baseline"}, func(input ingestion.Request) error { requests = append(requests, input); return nil })
	if err != nil || len(requests) != 1 || requests[0].Metadata.Root != healthy {
		t.Fatalf("batch %#v err %v", batch, err)
	}
	if len(batch.Failures) != 1 || batch.Failures[0].Path != broken || batch.Failures[0].Detail == "" {
		t.Fatalf("failures %#v", batch.Failures)
	}
}

func TestCollectWorkspacesPublishesBeforeOtherCheckoutFinishes(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"first", "second"} {
		root := initialized(t, filepath.Join(workspace, name))
		write(t, root, "file.txt", "after\n")
	}
	blocked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var observed int
	options := Options{SourceID: "machine", Hostname: "host", OnSource: func(ingestion.Metadata, diffsource.Comparison) {
		observed++
		if observed == 2 {
			close(blocked)
			<-release
		}
	}}
	published := make(chan ingestion.Request, 2)
	done := make(chan error, 1)
	go func() {
		_, err := CollectWorkspaces(t.Context(), Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, options, func(input ingestion.Request) error {
			published <- input
			return nil
		})
		done <- err
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("second checkout did not block")
	}
	select {
	case input := <-published:
		if len(input.Scopes) == 0 || len(input.Scopes[0].Snapshot.Files) != 1 {
			t.Fatalf("incomplete published checkout: %#v", input)
		}
	case <-done:
		t.Fatal("collection finished while checkout was held")
	case <-time.After(5 * time.Second):
		t.Fatal("ready checkout waited for held checkout")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collection did not finish")
	}
	if len(published) != 1 {
		t.Fatal("remaining checkout was not published")
	}
}

func TestCollectWorkspacesJoinsWorkersAfterCancellationAndPublishFailure(t *testing.T) {
	for _, failPublish := range []bool{false, true} {
		t.Run(fmt.Sprintf("publish-failure-%v", failPublish), func(t *testing.T) {
			workspace := t.TempDir()
			for _, name := range []string{"first", "second", "third", "fourth", "fifth"} {
				root := initialized(t, filepath.Join(workspace, name))
				write(t, root, "file.txt", "after\n")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			blocked, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var observed int
			var exited atomic.Bool
			options := Options{SourceID: "machine", OnSource: func(ingestion.Metadata, diffsource.Comparison) {
				observed++
				if observed == 2 {
					close(blocked)
					<-release
					exited.Store(true)
				}
			}}
			publishFailure := errors.New("publication failed")
			published := make(chan struct{}, 1)
			done := make(chan error, 1)
			go func() {
				_, err := CollectWorkspaces(ctx, Workspaces{Paths: []string{workspace}, Depth: 8, LinkedWorktrees: true}, options, func(ingestion.Request) error {
					<-blocked
					published <- struct{}{}
					if failPublish {
						return publishFailure
					}
					return nil
				})
				done <- err
			}()
			select {
			case <-blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("checkout did not block")
			}
			select {
			case <-published:
			case <-time.After(5 * time.Second):
				t.Fatal("first checkout did not publish")
			}
			if !failPublish {
				cancel()
			}
			select {
			case err := <-done:
				t.Fatalf("returned before worker joined: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-done:
				want := context.Canceled
				if failPublish {
					want = publishFailure
				}
				if !errors.Is(err, want) {
					t.Fatalf("collection error %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("collection did not join workers")
			}
			if !exited.Load() {
				t.Fatal("blocked worker did not exit")
			}
			if len(published) != 0 {
				t.Fatal("published another checkout after stopping")
			}
		})
	}
}

func TestCollectWorkspacesPublishesBeforeWorkspaceDiscoveryFinishes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git wrapper requires a POSIX shell")
	}
	workspace := t.TempDir()
	first := initialized(t, filepath.Join(workspace, "first"))
	second := initialized(t, filepath.Join(workspace, "second"))
	write(t, first, "file.txt", "first change\n")
	write(t, second, "file.txt", "second change\n")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	blocked, release := filepath.Join(bin, "blocked"), filepath.Join(bin, "release")
	script := `#!/bin/sh
if [ "$PWD" = "$DIFFX_TEST_SLOW_CHECKOUT" ]; then
  case " $* " in
    *" rev-parse --show-toplevel "*)
      printf 'blocked\n' > "$DIFFX_TEST_DISCOVERY_BLOCKED"
      while [ ! -f "$DIFFX_TEST_DISCOVERY_RELEASE" ]; do sleep 0.01; done
      ;;
  esac
fi
exec "$DIFFX_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_TEST_REAL_GIT", realGit)
	t.Setenv("DIFFX_TEST_SLOW_CHECKOUT", second)
	t.Setenv("DIFFX_TEST_DISCOVERY_BLOCKED", blocked)
	t.Setenv("DIFFX_TEST_DISCOVERY_RELEASE", release)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	published := make(chan ingestion.Request, 2)
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_, err := CollectWorkspaces(ctx, Workspaces{Paths: []string{first, second}, Depth: 8, LinkedWorktrees: true}, Options{SourceID: "machine"}, func(input ingestion.Request) error {
			published <- input
			return nil
		})
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		_ = os.WriteFile(release, nil, 0o600)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("collection did not stop")
		}
	})
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(blocked); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("later checkout discovery did not block")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case input := <-published:
		if input.Metadata.Root != first {
			t.Fatalf("unexpected first checkout: %s", input.Metadata.Root)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ready checkout waited for later discovery")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collection did not finish")
	}
	if len(published) != 1 {
		t.Fatal("second checkout was not published")
	}
}
