package main

import (
	"slices"
	"testing"
)

func TestGroupInputContracts(t *testing.T) {
	for _, name := range []string{"docs/development.md", "docs-website/src/index.mdx", "README.md"} {
		if groupInput("go", name) {
			t.Errorf("Go checks include unrelated docs input %s", name)
		}
	}
	for _, group := range groups(true) {
		for _, name := range []string{"new-unclassified/file", "mise.toml", "mise.lock", "pnpm-lock.yaml", ".node-version", "tools/check-runner/pipeline.go"} {
			if !groupInput(group.Name, name) {
				t.Errorf("%s omits shared/unknown input %s", group.Name, name)
			}
		}
	}
	if !groupInput("docs", "docs-website/src/index.mdx") || !groupInput("static", "docs/development.md") {
		t.Fatal("documentation changes have no owning check")
	}
	for _, name := range []string{"docs/release.md", "LICENSE", "apps/desktop/resources/NOTICE.txt", "tools/native-tokens/themes/LICENSE-theme-2.0.0"} {
		if !groupInput("native-package", name) {
			t.Errorf("native archives omit required notice source %s", name)
		}
	}
}

func TestGroupFingerprintInputsAndHistory(t *testing.T) {
	root := snapshotTestRepository(t)
	goGroup := groups(true)[2]
	if goGroup.Name != "go" {
		t.Fatal("expected Go group")
	}
	baseline, err := groupFingerprint(root, goGroup)
	if err != nil {
		t.Fatal(err)
	}
	snapshotTestWrite(t, root, "docs/development.md", "documentation change")
	snapshotTestCommit(t, root)
	afterDocs, err := groupFingerprint(root, goGroup)
	if err != nil || baseline != afterDocs {
		t.Fatalf("docs/history invalidated Go: %v", err)
	}
	for _, name := range []string{"internal/review/change.go", "pnpm-lock.yaml", "mise.toml", "apps/desktop/go.mod", "apps/desktop/go.sum", "apps/desktop/mygo.json", "unknown-contract.txt"} {
		snapshotTestWrite(t, root, name, "changed")
		next, err := groupFingerprint(root, goGroup)
		if err != nil || next == baseline {
			t.Fatalf("%s did not invalidate Go: %v", name, err)
		}
		baseline = next
	}
	// Metadata changes cannot alter the result identity, even when the original
	// commit is retained separately as evidence.
	snapshotTestCommit(t, root)
	afterCommit, err := groupFingerprint(root, goGroup)
	if err != nil || baseline != afterCommit {
		t.Fatalf("commit identity invalidated contents: %v", err)
	}
}

func TestPRGroupsPreserveSuiteCoverage(t *testing.T) {
	var names []string
	for _, group := range groups(true) {
		names = append(names, group.Name)
		if len(group.Commands) == 0 {
			t.Errorf("empty group %s", group.Name)
		}
		if group.Name == "go" && !slices.Equal(group.Commands[1], []string{"mise", "run", "test:exec:race"}) {
			t.Fatal("PR validation dropped race coverage")
		}
		if group.Name == "browser" && (!group.Build || !group.Database || !group.Browser) {
			t.Fatal("browser checks missing fixture preparation")
		}
		if group.Name == "native" && (!group.Native || !group.Build || group.Host || group.Database || group.Browser || len(group.Commands) != 3 || !slices.Equal(group.Commands[1], []string{"mise", "run", "test:exec:native:race"}) || !slices.Equal(group.Commands[2], []string{"mise", "run", "test:exec:native:release"})) {
			t.Fatal("native checks must exercise their nested module and real sidecar with isolated race coverage")
		}
		if group.Name == "native-window" && (!group.Native || !group.Build || group.Host || group.Database || group.Browser || !slices.Equal(group.Commands[0], []string{"mise", "run", "test:exec:native:window"})) {
			t.Fatal("real native windows require an isolated native container and actual acceptance task")
		}
		if group.Name == "native-package" && (!group.Native || !group.Build || group.Host || group.Database || group.Browser || !slices.Equal(group.Commands[0], []string{"mise", "run", "test:exec:native:package"})) {
			t.Fatal("native package checks must build and inspect real target archives in isolation")
		}
		if group.Host != (group.Name == "static") {
			t.Fatalf("unexpected execution boundary for %s", group.Name)
		}
	}
	for _, name := range []string{"static", "contracts", "go", "unit", "browser", "conformance", "docs", "release", "generated", "native", "native-window", "native-package"} {
		if !slices.Contains(names, name) {
			t.Errorf("missing required group %s", name)
		}
	}
}
