package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostChecksUseDisposableSourceAndRetainFailures(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			root := snapshotTestRepository(t)
			bin := t.TempDir()
			// Preparation is replaced; the actual check and process lifecycle run.
			if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			artifacts := t.TempDir()
			script := "test \"$PWD\" != '" + root + "' && test -d \"$TMPDIR\" && printf evidence > host-output && printf host-evidence"
			if failure {
				script += "; exit 17"
			}
			group := checkGroup{Name: "static", Host: true, Commands: [][]string{{"sh", "-ec", script}}}
			err := executeHost(t.Context(), root, group, artifacts)
			if failure && (err == nil || !strings.Contains(err.Error(), "exit status 17")) {
				t.Fatalf("missing original failure: %v", err)
			}
			if !failure && err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "host-output")); !os.IsNotExist(err) {
				t.Fatalf("host check modified source: %v", err)
			}
			log, err := os.ReadFile(filepath.Join(artifacts, "static", "02.log"))
			if err != nil || string(log) != "host-evidence" {
				t.Fatalf("missing check output: %q, %v", log, err)
			}
		})
	}
}

func TestHostRuntimeChangesInvalidateResults(t *testing.T) {
	root := snapshotTestRepository(t)
	bin := t.TempDir()
	for _, name := range []string{"go", "node", "pnpm"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("runtime one"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	group := groups(true)[0]
	before, err := groupFingerprint(root, group)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte("runtime two"), 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := groupFingerprint(root, group)
	if err != nil || before == after {
		t.Fatalf("host runtime change reused success: %v", err)
	}
}
