package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/diagnostics"
)

func TestDiagnosticsExportCommand(t *testing.T) {
	runtimeDirectory := t.TempDir()
	t.Setenv("DIFFX_RUNTIME_DIR", runtimeDirectory)
	report := diagnostics.New(diagnostics.Metadata{AppVersion: "dev", Operation: "sync"}).Finish(diagnostics.Outcome{Status: "failure", Primary: &diagnostics.Failure{Kind: "git"}})
	if err := diagnostics.Save(filepath.Join(runtimeDirectory, "diagnostics"), report); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"--latest", report.ID} {
		output := filepath.Join(t.TempDir(), "failure.json")
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), []string{"diagnostics", "export", selector, "--output", output}, nil, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var exported diagnostics.Report
		if err := json.Unmarshal(data, &exported); err != nil {
			t.Fatal(err)
		}
		if exported.ID != report.ID || exported.SchemaVersion != 1 {
			t.Fatal("wrong exported report")
		}
		if !strings.Contains(stdout.String(), "exported") || stderr.Len() != 0 {
			t.Fatalf("output: %q %q", stdout.String(), stderr.String())
		}
		if err := runDiagnostics([]string{"export", selector, "--output", output}, &stdout); err == nil {
			t.Fatal("overwrote output")
		}
	}
	// A different isolated runtime cannot export this session's report.
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	if err := runDiagnostics([]string{"export", report.ID, "--output", filepath.Join(t.TempDir(), "out")}, &bytes.Buffer{}); err == nil {
		t.Fatal("read another runtime's diagnostics")
	}
}

func TestDiagnosticsExportArguments(t *testing.T) {
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	for _, arguments := range [][]string{
		{}, {"import"}, {"export"}, {"export", "--latest"}, {"export", "--output", "out"},
		{"export", "../escape", "--output", "out"}, {"export", "id", "--latest", "--output", "out"},
		{"export", "id", "second", "--output", "out"}, {"export", "--unknown"},
	} {
		if err := runDiagnostics(arguments, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %q", arguments)
		}
	}
	for _, arguments := range [][]string{{"--help"}, {"export", "--help"}} {
		var stdout bytes.Buffer
		if err := runDiagnostics(arguments, &stdout); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "--output") {
			t.Fatal("missing diagnostics help")
		}
	}
	var help bytes.Buffer
	if _, err := parseOptions([]string{"--help"}, &help); err != errHelp {
		t.Fatal(err)
	}
	if !strings.Contains(help.String(), "diffx diagnostics export") {
		t.Fatal("missing top-level help")
	}
}
