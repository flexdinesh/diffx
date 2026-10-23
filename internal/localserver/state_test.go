package localserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

func writeState(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE legacy(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
		t.Fatal(err)
	}
}

// startWithReset runs local startup to readiness, then stops it.
func startWithReset(t *testing.T, path string, reset StateReset) (bool, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready := false
	err := Run(ctx, Settings{Host: "127.0.0.1", Port: 0, State: path}, "", nil, reset,
		func(Status, *contextservice.Publication) {
			ready = true
			cancel()
		})
	return ready, err
}

func TestLocalStartupResetsOlderStateOnlyAfterConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	writeState(t, path, 10)
	var asked *reviewstore.StateError
	ready, err := startWithReset(t, path, StateReset{Confirm: func(failure *reviewstore.StateError) (bool, error) {
		asked = failure
		return false, nil
	}})
	var failure *reviewstore.StateError
	if ready || !errors.As(err, &failure) || !failure.Older() {
		t.Fatalf("declined reset should refuse older state: ready=%t err=%v", ready, err)
	}
	if asked == nil || asked.Version != 10 {
		t.Fatalf("confirmation not asked for older state: %#v", asked)
	}
	if entries, _ := filepath.Glob(path + ".schema*"); len(entries) != 0 {
		t.Fatalf("declined reset created backups: %v", entries)
	}

	saved := ""
	ready, err = startWithReset(t, path, StateReset{
		Confirm: func(*reviewstore.StateError) (bool, error) { return true, nil },
		Saved:   func(backup string) { saved = backup },
	})
	if !ready || err != nil {
		t.Fatalf("confirmed reset did not start: ready=%t err=%v", ready, err)
	}
	if _, err := os.Stat(saved); saved == "" || err != nil {
		t.Fatalf("backup not reported: %q %v", saved, err)
	}
}

func TestLocalStartupForcedResetReplacesCurrentStateButRefusesNewer(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.db")
	if ready, err := startWithReset(t, current, StateReset{}); !ready || err != nil {
		t.Fatalf("initial start: ready=%t err=%v", ready, err)
	}
	saved := ""
	if ready, err := startWithReset(t, current, StateReset{Force: true, Saved: func(backup string) { saved = backup }}); !ready || err != nil || saved == "" {
		t.Fatalf("forced reset of current state: ready=%t err=%v backup=%q", ready, err, saved)
	}

	newer := filepath.Join(dir, "newer.db")
	writeState(t, newer, 999)
	ready, err := startWithReset(t, newer, StateReset{Force: true})
	var failure *reviewstore.StateError
	if ready || !errors.As(err, &failure) || failure.Version != 999 {
		t.Fatalf("forced reset accepted newer state: ready=%t err=%v", ready, err)
	}
	if entries, _ := filepath.Glob(newer + ".schema*"); len(entries) != 0 {
		t.Fatalf("newer state backed up: %v", entries)
	}
}

func TestLocalStartupNewerStateRefusedWithoutAsking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	writeState(t, path, 999)
	asked := false
	ready, err := startWithReset(t, path, StateReset{Confirm: func(*reviewstore.StateError) (bool, error) {
		asked = true
		return true, nil
	}})
	if ready || asked || err == nil {
		t.Fatalf("newer state reached confirmation: ready=%t asked=%t err=%v", ready, asked, err)
	}
}
