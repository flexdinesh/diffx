package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeOlderState(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('retain'); PRAGMA user_version=10"); err != nil {
		t.Fatal(err)
	}
	return path
}

// stopOnWrite stops the command once startup output is written.
type stopOnWrite struct{ stop context.CancelFunc }

func (writer stopOnWrite) Write(value []byte) (int, error) {
	writer.stop()
	return len(value), nil
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".schema*.bak")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestLocalOlderStateWithoutTerminalSuggestsResetAndKeepsData(t *testing.T) {
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	path := writeOlderState(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = run(t.Context(), []string{t.TempDir(), "--state", path, "--no-browser"}, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "schema 10 is older") || !strings.Contains(err.Error(), "rerun with --reset-state") {
		t.Fatalf("missing reset guidance: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || len(backups(t, path)) != 0 {
		t.Fatalf("refused state changed: %v", err)
	}
}

func TestLocalResetStateBacksUpOlderStateAndStarts(t *testing.T) {
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	path := writeOlderState(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	if err := run(ctx, []string{t.TempDir(), "--state", path, "--no-browser", "--reset-state"}, nil, stopOnWrite{cancel}, &stderr); err != nil {
		t.Fatalf("reset start: %v\n%s", err, stderr.String())
	}
	saved := backups(t, path)
	if len(saved) != 1 || !strings.Contains(stderr.String(), "diffx: started fresh; old state saved at "+saved[0]) {
		t.Fatalf("backup not reported: %v\n%s", saved, stderr.String())
	}
}

func TestFixtureResetStateUsesFlagWithoutPrompt(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "test", "fixtures", "sample.diff"))
	if err != nil {
		t.Fatal(err)
	}
	path := writeOlderState(t)
	if err := run(t.Context(), []string{"dev", "--fixture", fixture, "--state", path, "--no-browser"}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "rerun with --reset-state") {
		t.Fatalf("dev missing reset guidance: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := run(ctx, []string{"dev", "--fixture", fixture, "--state", path, "--no-browser", "--reset-state"}, nil, stopOnWrite{cancel}, io.Discard); err != nil {
		t.Fatalf("dev reset start: %v", err)
	}
	if len(backups(t, path)) != 1 {
		t.Fatal("dev reset did not back up state")
	}
}

func TestResetStateRejectedForMemoryAndRemoteSync(t *testing.T) {
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	if err := run(t.Context(), []string{t.TempDir(), "--state", "memory", "--reset-state", "--no-browser"}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "reset-state requires persistent state") {
		t.Fatalf("memory reset accepted: %v", err)
	}
	if err := run(t.Context(), []string{"sync", "--reset-state"}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "does not accept local server options") {
		t.Fatalf("sync accepted reset-state: %v", err)
	}
}
