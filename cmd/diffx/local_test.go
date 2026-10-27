package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/localserver"
)

func TestLocalInputDefaultsToCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	input, err := acquireLocalInput(t.Context(), options{directory: root, repositorySet: true}, nil)
	if err != nil || input.Kind != "worktree" || !slices.Equal(input.Paths, []string{root}) {
		t.Fatalf("path input: %+v %v", input, err)
	}
	input, err = acquireLocalInput(t.Context(), options{directory: "."}, nil)
	if err != nil || input.Kind != "worktree" || !slices.Equal(input.Paths, []string{root}) {
		t.Fatalf("default input: %+v %v", input, err)
	}
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	values, err := parseOptions([]string{"--path", "one", "--path=two"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	input, err = acquireLocalInput(t.Context(), values, nil)
	if err != nil || !slices.Equal(input.Paths, []string{filepath.Join(root, "one"), filepath.Join(root, "two")}) {
		t.Fatalf("repeated paths: %+v %v", input, err)
	}
}

func TestLocalInputReadsPipeAndRedirectedFile(t *testing.T) {
	raw := []byte("a patch\n")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if _, err := writer.Write(raw); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	writer.Close()
	for _, stdin := range []*os.File{reader, testInputFile(t, raw)} {
		input, err := acquireLocalInput(t.Context(), options{directory: "."}, stdin)
		if err != nil || input.Kind != "capture" || !bytes.Equal(input.Raw, raw) || !filepath.IsAbs(input.SubmittedFrom) {
			t.Fatalf("patch input: %+v %v", input, err)
		}
	}
}

func TestLocalInputRejectsPathWithRedirectedStdin(t *testing.T) {
	for _, arguments := range [][]string{{"."}, {"--path", "."}} {
		values, err := parseOptions(arguments, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		stdin := testInputFile(t, []byte("patch"))
		if _, err := acquireLocalInput(t.Context(), values, stdin); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("accepted path with stdin: %v", err)
		}
		position, err := stdin.Seek(0, io.SeekCurrent)
		if err != nil || position != 0 {
			t.Fatalf("conflicting input consumed: %d %v", position, err)
		}
	}
}

func TestLocalPatchValidationBeforeStartup(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		raw  []byte
		want string
	}{
		{name: "invalid", raw: []byte("not a patch"), want: "No Git patch"},
		{name: "oversized", raw: bytes.Repeat([]byte("x"), diffsource.MaxInputBytes+1), want: "16 MiB"},
		{name: "base", args: []string{"--base", "HEAD"}, want: "base is only supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DIFFX_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
			err := run(t.Context(), test.args, testInputFile(t, test.raw), io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("patch validation: %v", err)
			}
		})
	}
}

func TestLocalInputScopesCheckoutPathsToTheirCheckout(t *testing.T) {
	root := cliRepository(t)
	linked := filepath.Join(t.TempDir(), "linked")
	producerGit(t, root, "worktree", "add", "-b", "other", linked)
	workspace := t.TempDir()
	t.Chdir(root)
	for _, test := range []struct {
		arguments []string
		want      localserver.Selection
		err       string
	}{
		{nil, localserver.SelectedCheckout, ""},
		{[]string{"."}, localserver.SelectedCheckout, ""},
		{[]string{linked}, localserver.SelectedCheckout, ""},
		{[]string{"--scan-worktrees"}, localserver.DiscoveredCheckouts, ""},
		{[]string{".", "--scan-worktrees"}, localserver.DiscoveredCheckouts, ""},
		{[]string{workspace}, localserver.DiscoveredCheckouts, ""},
		{[]string{workspace, "--scan-depth", "4"}, localserver.DiscoveredCheckouts, ""},
		{[]string{"--path", root}, localserver.DiscoveredCheckouts, ""},
		{[]string{"--path", root, "--path", linked}, localserver.DiscoveredCheckouts, ""},
		{[]string{"--scan-depth", "4"}, "", "--scan-depth bounds directory search"},
		{[]string{".", "--scan-worktrees", "--scan-depth", "4"}, "", "--scan-depth bounds directory search"},
	} {
		values, err := parseOptions(test.arguments, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		input, err := acquireLocalInput(t.Context(), values, nil)
		if test.err != "" {
			if err == nil || !strings.Contains(err.Error(), test.err) {
				t.Fatalf("%v: error %v, want %q", test.arguments, err, test.err)
			}
			continue
		}
		if err != nil || input.Selection != test.want {
			t.Fatalf("%v: selection %q %v, want %q", test.arguments, input.Selection, err, test.want)
		}
	}
	for selection, want := range map[localserver.Selection][]string{
		localserver.SelectedCheckout:    {root},
		localserver.DiscoveredCheckouts: {root, linked},
	} {
		values := options{directory: ".", sourceID: "test-source", trigger: "manual"}
		var roots []string
		input := initialInput{Kind: "worktree", Paths: []string{root}, Selection: selection}
		if _, err := collectWorkspaces(t.Context(), values, input, io.Discard, func(request ingestion.Request) error {
			roots = append(roots, request.Metadata.Root)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		slices.Sort(roots)
		slices.Sort(want)
		if !slices.Equal(roots, want) {
			t.Fatalf("%s collected %v, want %v", selection, roots, want)
		}
	}
}
