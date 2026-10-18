package collector

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	result, err := DiscoverWorkspaces(t.Context(), []string{workspace}, 8, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{root, linked}) {
		t.Fatalf("sources %v diagnostics %v err %v", discoveredPaths(result), result.Diagnostics, err)
	}
	if result.Sources[0].InputPath != workspace || result.Sources[0].Identity == result.Sources[1].Identity {
		t.Fatalf("unexpected sources: %#v", result.Sources)
	}
	identity := result.Sources[1].Identity
	git(t, root, "branch", "-m", "main", "master")
	result, err = DiscoverWorkspaces(t.Context(), []string{workspace}, 8, "machine", "auto")
	if err != nil || len(result.Sources) != 2 || result.Sources[1].Identity != identity {
		t.Fatalf("worktree identity changed: %#v %v %v", result.Sources, result.Diagnostics, err)
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
	result, err := DiscoverWorkspaces(t.Context(), []string{workspace}, 8, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{outer, visible}) {
		t.Fatalf("sources %v diagnostics %v err %v", discoveredPaths(result), result.Diagnostics, err)
	}
	hidden := filepath.Join(workspace, ".hidden")
	result, err = DiscoverWorkspaces(t.Context(), []string{hidden}, 8, "machine", "auto")
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
	result, err := DiscoverWorkspaces(t.Context(), []string{nested, workspace, root}, 8, "machine", "auto")
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
	result, err := DiscoverWorkspaces(context.Background(), []string{workspace}, 8, "machine", "auto")
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
	result, err := DiscoverWorkspaces(t.Context(), []string{workspace}, 8, "machine", "auto")
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
	result, err := DiscoverWorkspaces(t.Context(), []string{root}, 8, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{root}) || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0], "worktree unavailable") {
		t.Fatalf("missing worktree: %v %v %v", discoveredPaths(result), result.Diagnostics, err)
	}
}

func TestDiscoveryBoundsDepthAndReportsMissingInput(t *testing.T) {
	workspace := t.TempDir()
	deep := initialized(t, filepath.Join(workspace, "one", "two", "three"))
	result, err := DiscoverWorkspaces(context.Background(), []string{workspace}, 2, "machine", "auto")
	if err != nil || len(result.Sources) != 0 {
		t.Fatalf("deep checkout escaped discovery bound: %#v %v", result, err)
	}
	result, err = DiscoverWorkspaces(context.Background(), []string{workspace}, 3, "machine", "auto")
	if err != nil || !slices.Equal(discoveredPaths(result), []string{deep}) {
		t.Fatalf("checkout within depth missed: %#v %v", result, err)
	}
	result, err = DiscoverWorkspaces(context.Background(), []string{filepath.Join(workspace, "missing")}, 8, "machine", "auto")
	if err != nil || len(result.Sources) != 0 || len(result.Diagnostics) == 0 {
		t.Fatalf("missing path lacks diagnostics: %#v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverWorkspaces(ctx, []string{workspace}, 8, "machine", "auto"); err == nil {
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
	batch, err := CollectWorkspaces(t.Context(), []string{workspace}, 8, Options{SourceID: "machine", Hostname: "host", SubmissionID: "shared"})
	if err != nil || len(batch.Requests) != 2 || len(batch.Failures) != 0 {
		t.Fatalf("batch %#v err %v", batch, err)
	}
	if batch.Requests[0].Metadata.Root != first || batch.Requests[1].Metadata.Root != second {
		t.Fatalf("unexpected roots: %s %s", batch.Requests[0].Metadata.Root, batch.Requests[1].Metadata.Root)
	}
	if batch.Requests[0].SubmissionID == batch.Requests[1].SubmissionID || batch.Requests[0].SubmissionID == "shared" {
		t.Fatal("checkouts share a submission ID")
	}
	if batch.Requests[0].Metadata.TriggerRoot != workspace {
		t.Fatalf("trigger root %q", batch.Requests[0].Metadata.TriggerRoot)
	}
	if _, err := CollectWorkspaces(t.Context(), []string{t.TempDir()}, 8, Options{SourceID: "machine", Hostname: "host"}); err == nil || !strings.Contains(err.Error(), "no Git checkouts found") {
		t.Fatalf("empty workspace: %v", err)
	}
}

func TestCollectWorkspacesReportsFailedCheckoutWithoutBlockingOthers(t *testing.T) {
	workspace := t.TempDir()
	healthy := initialized(t, filepath.Join(workspace, "healthy"))
	broken := initialized(t, filepath.Join(workspace, "broken"))
	write(t, healthy, "file.txt", "after\n")
	// A base that cannot resolve fails only the checkout without that ref.
	git(t, healthy, "branch", "baseline")
	batch, err := CollectWorkspaces(t.Context(), []string{workspace}, 8, Options{SourceID: "machine", Hostname: "host", Base: "baseline"})
	if err != nil || len(batch.Requests) != 1 || batch.Requests[0].Metadata.Root != healthy {
		t.Fatalf("batch %#v err %v", batch, err)
	}
	if len(batch.Failures) != 1 || batch.Failures[0].Path != broken || batch.Failures[0].Detail == "" {
		t.Fatalf("failures %#v", batch.Failures)
	}
}
