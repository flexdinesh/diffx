package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

func cliRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "feature"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", output, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "initial"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", output, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNonRepositoryWorkspaceFailsCollectionWithoutConsumingStdin(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	values := options{directory: ".", sourceID: "test-source", trigger: "manual"}
	input, err := acquireLocalInput(values, stdin)
	if err != nil {
		t.Fatal(err)
	}
	if input.Kind != "worktree" || len(input.Paths) != 1 || input.Paths[0] != root || len(input.Raw) != 0 {
		t.Fatalf("workspace input: %#v", input)
	}
	publications := 0
	_, err = collectWorkspaces(t.Context(), values, input.Paths, io.Discard, func(ingestion.Request) error {
		publications++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "no Git checkouts found") {
		t.Fatalf("empty workspace collection: %v", err)
	}
	if publications != 0 {
		t.Fatalf("empty workspace published %d observations", publications)
	}
	position, err := stdin.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 {
		t.Fatalf("stdin consumed: %d %v", position, err)
	}
}

func TestPipedInputKeepsSubmissionDirectoryWithoutGitIdentity(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	patch := []byte("diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n")
	values := options{directory: ".", sourceID: "test-source", trigger: "manual"}
	input, err := acquireLocalInput(values, testInputFile(t, patch))
	if err != nil {
		t.Fatal(err)
	}
	request, err := collectInitialInput(t.Context(), values, input)
	if err != nil {
		t.Fatal(err)
	}
	if request.Metadata.RepositoryKey != "" || request.Metadata.Branch != "" || request.Metadata.Root != root || len(request.Scopes) != 1 || request.Scopes[0].Snapshot.Source != "stdin" {
		t.Fatalf("pipe metadata: %#v", request)
	}
}

func TestCollectorOptionsRejectAmbiguousInputsBeforeStartup(t *testing.T) {
	cases := [][]string{
		{".", "--server", "http://example.test"}, {"change"}, {"watch"},
		{"sync", "--server", "http://example.test", "--state", "memory"},
		{"sync", "--trigger", "unknown"},
		{"sync", "--fixture", "input.diff"},
		{"pipe", "--capture", "saved"},
		{"sync", "--config", "{}"},
		{"sync", "--host", "0.0.0.0"},
		{"service", "start", "--server", "http://example.test"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := run(t.Context(), args, nil, io.Discard, io.Discard); err == nil {
				t.Fatalf("accepted %v", args)
			}
		})
	}
}

func TestHelpDoesNotExposeEnvironmentCredentials(t *testing.T) {
	t.Setenv("DIFFX_REMOTE_TOKEN", "secret-token")
	t.Setenv("DIFFX_REMOTE_URL", "https://secret-user:secret-password@example.test")
	var output bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, nil, io.Discard, &output); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-token", "secret-user", "secret-password"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("help exposed %s", secret)
		}
	}
}
