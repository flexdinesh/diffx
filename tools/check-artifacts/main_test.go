package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsTrackedBuildOutputs(t *testing.T) {
	for _, name := range []string{"dist/diffx", "apps/web/dist/index.html", "apps/console/dist/index.html", "internal/webui/dist/index.html", "internal/consoleui/dist/index.html", "docs-website/dist/public/index.html", "diffx", "diffx-server", "diffx-console.exe", "diffx-fetcher", "diffx-native", "apps/desktop/diffx-native", "apps/desktop/build/Diffx.app/Contents/MacOS/diffx-native", "apps/desktop/.mygo/app", "apps/desktop/test-results/capture.png"} {
		t.Run(name, func(t *testing.T) {
			root := artifactRepository(t)
			artifactFile(t, root, name)
			artifactGit(t, root, "add", "--force", "--", name)
			if err := checkArtifacts(root); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("tracked build output must fail with its path: %v", err)
			}
		})
	}
}

func TestAllowsGeneratedContractsPluginsAndUntrackedBuilds(t *testing.T) {
	root := artifactRepository(t)
	for _, name := range []string{"packages/plugin-opencode/dist/index.js", "packages/plugin-pi/dist/index.js", "packages/api/src/schema.d.ts", "internal/pgstore/pgdb/models.go", "install.sh", "cmd/diffx/main.go", "apps/desktop/internal/design/tokens_generated.go", "apps/desktop/internal/preferences/themes_generated.go"} {
		artifactFile(t, root, name)
		artifactGit(t, root, "add", "--", name)
	}
	artifactFile(t, root, "internal/webui/dist/index.html")
	artifactFile(t, root, "dist/diffx")
	if err := checkArtifacts(root); err != nil {
		t.Fatal(err)
	}
}

func artifactRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	artifactGit(t, root, "init", "--quiet")
	return root
}

func artifactFile(t *testing.T, root, name string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func artifactGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
