package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestEscapedDirectoryPreservesFlagDelimiter(t *testing.T) {
	values, err := parseOptions([]string{"--no-browser", "--", "-repository"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if values.directory != "-repository" || !values.noBrowser {
		t.Fatalf("options = %#v", values)
	}
}

func TestHelpDocumentsModesWithoutLegacyCommands(t *testing.T) {
	var output bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, nil, io.Discard, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "diffx sync") || strings.Contains(output.String(), "diffx service") || strings.Contains(output.String(), "runtime-dir") {
		t.Fatalf("help = %s", output.String())
	}
}

func TestHarnessOptionPreservesDirectoryAndMetadata(t *testing.T) {
	for _, arguments := range [][]string{
		{"--harness", "codex", ".", "--run-id", "session"},
		{".", "--harness", "codex", "--run-id", "session"},
		{".", "--harness=codex", "--run-id=session"},
	} {
		values, err := parseOptions(arguments, io.Discard)
		if err != nil || values.directory != "." || !values.repositorySet || values.agent != "codex" || values.runID != "session" {
			t.Fatalf("arguments %v: %#v, %v", arguments, values, err)
		}
	}
}

func TestOptionsRejectRemovedAgentFlag(t *testing.T) {
	if _, err := parseOptions([]string{"--agent", "codex"}, io.Discard); err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -agent") {
		t.Fatalf("removed flag: %v", err)
	}
}

func TestHelpDocumentsHarnessFlag(t *testing.T) {
	for _, arguments := range [][]string{{"--help"}, {"sync", "--help"}, {"hook", "--help"}} {
		var output bytes.Buffer
		if err := run(t.Context(), arguments, nil, &output, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "-harness") || strings.Contains(output.String(), "-agent") {
			t.Fatalf("help for %v: %s", arguments, output.String())
		}
	}
}

func TestWorkspaceOptionsParseAndStayLocal(t *testing.T) {
	values, err := parseOptions([]string{"--path", "one", "--path", "two", "--scan-depth", "3"}, io.Discard)
	if err != nil || len(values.paths) != 2 || values.directory != "one" || values.scanDepth != 3 || !values.scanDepthSet {
		t.Fatalf("workspace options: %#v %v", values, err)
	}
	values, err = parseOptions([]string{"repo"}, io.Discard)
	if err != nil || len(values.paths) != 1 || values.paths[0] != "repo" || values.scanDepth != 8 || values.scanWorktrees {
		t.Fatalf("positional path: %#v %v", values, err)
	}
	values, err = parseOptions([]string{"repo", "--scan-worktrees"}, io.Discard)
	if err != nil || !values.scanWorktrees || values.paths[0] != "repo" {
		t.Fatalf("scan worktrees: %#v %v", values, err)
	}
	for _, arguments := range [][]string{{"--scan-depth", "-1"}, {"--scan-depth", "33"}, {"--path", "one", "two"}} {
		if _, err := parseOptions(arguments, io.Discard); err == nil {
			t.Fatalf("accepted %v", arguments)
		}
	}
	for _, arguments := range [][]string{
		{"sync", "--path", "one", "--path", "two"},
		{"sync", "--scan-depth", "2"},
		{"sync", "--scan-worktrees"},
		{"dev", "--fixture", "patch.diff", "--scan-worktrees"},
		{"dev", "--fixture", "patch.diff", "--scan-depth", "2"},
	} {
		if err := run(t.Context(), arguments, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "only supported by local mode") {
			t.Fatalf("%v: %v", arguments, err)
		}
	}
}
