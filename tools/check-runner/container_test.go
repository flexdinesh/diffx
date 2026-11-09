package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestMaterializedContextsMatchGroupInputs(t *testing.T) {
	snapshot := t.TempDir()
	files := []string{
		"go.mod", "go.sum", "mise.toml", "mise.lock", ".node-version", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml",
		"cmd/diffx/main.go", "apps/web/package.json", "apps/web/src/main.tsx", "apps/desktop/go.mod",
		"packages/ui/package.json", "tools/check-runner/Dockerfile", "unknown-contract.txt",
		"docs/validation.md", "README.md", "docs-website/package.json", "docs-website/src/index.mdx",
		// The server image's ignore file must reach containers without filtering contexts.
		".dockerignore",
	}
	for _, name := range files {
		snapshotTestWrite(t, snapshot, name, "content of "+name)
	}
	snapshotTestWrite(t, snapshot, ".git/HEAD", "ref: refs/heads/validation")
	mise := filepath.Join(t.TempDir(), "mise")
	if err := os.WriteFile(mise, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	contexts := t.TempDir()
	if err := materializeContexts(snapshot, contexts, mise); err != nil {
		t.Fatal(err)
	}
	contents := func(name string) []string {
		var paths []string
		root := filepath.Join(contexts, name)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(relative))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(paths)
		return paths
	}
	expect := func(name string, include func(string) bool) {
		t.Helper()
		var want []string
		for _, file := range files {
			if include(file) {
				want = append(want, contextTree+"/"+file)
			}
		}
		slices.Sort(want)
		if got := contents(name); !slices.Equal(got, want) {
			t.Errorf("%s context = %v, want %v", name, got, want)
		}
	}
	core := func(path string) bool { return groupInput("go", path) }
	site := func(path string) bool { return groupInput("docs", path) }
	expect("core", core)
	expect("extra", func(path string) bool { return groupInput("contracts", path) && !core(path) })
	expect("site-source", site)
	expect("manifests", func(path string) bool { return core(path) && isManifest(path) })
	expect("site-manifests", func(path string) bool { return site(path) && isManifest(path) })
	if got := contents("mise"); !slices.Equal(got, []string{"mise"}) {
		t.Errorf("mise context = %v", got)
	}
	if got := contents("main"); len(got) != 0 {
		t.Errorf("main context must stay empty: %v", got)
	}
	for _, name := range append(contextNames, "main") {
		if _, err := os.Lstat(filepath.Join(contexts, name, ".dockerignore")); !os.IsNotExist(err) {
			t.Errorf("%s context root has a .dockerignore BuildKit would apply: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(contexts, "core", contextTree, "cmd", "diffx", "main.go"))
	if err != nil || string(data) != "content of cmd/diffx/main.go" {
		t.Fatalf("core content = %q, %v", data, err)
	}
}

// Container inputs and fingerprints must share one predicate: every container
// group runs from the source variant whose exclusions equal its own.
func TestContainerGroupsRunFromTheirInputVariant(t *testing.T) {
	for _, group := range groups(true) {
		if group.Host {
			if group.Target != "" {
				t.Errorf("host group %s has image target %s", group.Name, group.Target)
			}
			continue
		}
		if _, ok := targetParents[group.Target]; !ok {
			t.Fatalf("group %s has unknown image target %q", group.Name, group.Target)
		}
		want := groupExclusions(sourceVariant(group.Target))
		got := groupExclusions(group.Name)
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("group %s excludes %v but target %s provides inputs excluding %v", group.Name, got, group.Target, want)
		}
		built := group.Target != "source" && group.Target != "site"
		if group.Build != built {
			t.Errorf("group %s Build=%v but target %s built=%v", group.Name, group.Build, group.Target, built)
		}
		desktop := strings.HasPrefix(group.Target, "desktop")
		if group.Desktop != desktop {
			t.Errorf("group %s Desktop=%v but target %s", group.Name, group.Desktop, group.Target)
		}
	}
	for _, target := range targetOrder {
		for parent := targetParents[target]; parent != ""; parent = targetParents[parent] {
			if slices.Index(targetOrder, parent) > slices.Index(targetOrder, target) {
				t.Errorf("target %s builds before its ancestor %s", target, parent)
			}
		}
	}
}

func TestDockerfileDefinesEveryTargetAndContext(t *testing.T) {
	data, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	// A context named like a stage makes BuildKit resolve the stage instead,
	// which fails as a circular dependency.
	for _, line := range strings.Split(dockerfile, "\n") {
		if _, stage, ok := strings.Cut(line, " AS "); ok && strings.HasPrefix(line, "FROM ") && slices.Contains(contextNames, strings.TrimSpace(stage)) {
			t.Errorf("build context %s shadows a Dockerfile stage", stage)
		}
	}
	for _, target := range targetOrder {
		if !strings.Contains(dockerfile, " AS "+target+"\n") {
			t.Errorf("Dockerfile lacks target %s", target)
		}
	}
	for _, name := range contextNames {
		if !strings.Contains(dockerfile, "from="+name+" ") && !strings.Contains(dockerfile, "--from="+name+" ") && !strings.Contains(dockerfile, "from="+name+",") {
			t.Errorf("Dockerfile never consumes context %s", name)
		}
	}
}

type fakeDocker struct {
	mu       sync.Mutex
	calls    [][]string
	outputs  map[string]string
	contexts []error
}

func (fake *fakeDocker) run(ctx context.Context, args ...string) ([]byte, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls = append(fake.calls, args)
	fake.contexts = append(fake.contexts, ctx.Err())
	return []byte(fake.outputs[strings.Join(args, " ")]), nil
}

func (fake *fakeDocker) called(prefix ...string) [][]string {
	var matches [][]string
	for _, call := range fake.calls {
		if len(call) >= len(prefix) && slices.Equal(call[:len(prefix)], prefix) {
			matches = append(matches, call)
		}
	}
	return matches
}

func TestSweepSlotRemovesOnlyItsOwnStaleResources(t *testing.T) {
	label := `{{.Label "com.docker.compose.project"}}`
	fake := &fakeDocker{outputs: map[string]string{
		"container ls --all --filter label=com.docker.compose.project --format " + label:         "diffx-check-s0-go\ndiffx-check-s1-go\ndiffx-check-s10-go\ndiffx\n",
		"network ls --filter label=com.docker.compose.project --format " + label:                 "diffx-check-s0-browser\ndiffx-check-s0-go\nother_default\n",
		"container ls --all --quiet --filter label=com.docker.compose.project=diffx-check-s0-go": "c1\nc2\n",
		"network ls --quiet --filter label=com.docker.compose.project=diffx-check-s0-browser":    "n1\n",
		"image ls --format {{.Repository}}:{{.Tag}} diffx-check":                                 "diffx-check:s0-built\ndiffx-check:s1-built\ndiffx-check:s0-source\n",
	}}
	if err := sweepSlot(t.Context(), fake.run, 0); err != nil {
		t.Fatal(err)
	}
	var swept []string
	for _, call := range fake.called("container", "ls", "--all", "--quiet", "--filter") {
		swept = append(swept, strings.TrimPrefix(call[5], "label=com.docker.compose.project="))
	}
	if !slices.Equal(swept, []string{"diffx-check-s0-go", "diffx-check-s0-browser"}) {
		t.Fatalf("swept projects %v", swept)
	}
	if removed := fake.called("container", "rm", "--force"); len(removed) != 1 || !slices.Equal(removed[0][3:], []string{"c1", "c2"}) {
		t.Fatalf("container removals %v", removed)
	}
	if removed := fake.called("network", "rm"); len(removed) != 1 || !slices.Equal(removed[0][2:], []string{"n1"}) {
		t.Fatalf("network removals %v", removed)
	}
	if removed := fake.called("image", "rm"); len(removed) != 1 || !slices.Equal(removed[0][2:], []string{"diffx-check:s0-built", "diffx-check:s0-source"}) {
		t.Fatalf("image removals %v", removed)
	}
	for _, call := range fake.calls {
		if slices.Contains(call, cacheVolume) || slices.Contains(call, "prune") {
			t.Fatalf("sweep touched shared resources: %v", call)
		}
	}
}

func TestTeardownSurvivesCancelledInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ctx.Err() == nil {
		t.Fatal("expected cancelled context")
	}
	fake := &fakeDocker{outputs: map[string]string{
		"container ls --all --quiet --filter label=com.docker.compose.project=diffx-check-s1-go": "c1\n",
	}}
	if err := teardownProject(fake.run, "diffx-check-s1-go"); err != nil {
		t.Fatal(err)
	}
	if len(fake.called("container", "rm", "--force", "c1")) != 1 {
		t.Fatalf("teardown did not remove container: %v", fake.calls)
	}
	for index, err := range fake.contexts {
		if err != nil {
			t.Fatalf("teardown call %v ran with a cancelled context", fake.calls[index])
		}
	}
}

func TestCheckEnvironmentDropsHostComposeSettings(t *testing.T) {
	got := checkEnvironment([]string{"PATH=/bin", "COMPOSE_FILE=other.yaml", "COMPOSE_PROFILES=x", "DIFFX_CHECK_IMAGE=stale", "DIFFX_VALIDATION_EXECUTION=stale", "HOME=/home"})
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/home"}) {
		t.Fatalf("environment = %v", got)
	}
}

func TestResourceNamesAreSlotScoped(t *testing.T) {
	if got := projectName(1, "go"); got != "diffx-check-s1-go" {
		t.Fatalf("project = %s", got)
	}
	if got := imageTag(0, "built-docs"); got != "diffx-check:s0-built-docs" {
		t.Fatalf("tag = %s", got)
	}
}
