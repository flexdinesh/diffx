package reviewstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/processlock"
)

func TestDatabaseOwnershipLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, processlock.ErrLocked) {
		t.Fatalf("concurrent database owner accepted: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("ownership not released: %v", err)
	}
	_ = second.Close()
}

func TestUnsupportedLegacyFilePreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	raw := []byte(`{"version":1,"sessions":{"old":{}}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("unsupported legacy state opened")
	} else if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "--state /path/to/new-state.db") {
		t.Fatalf("missing state path or recovery guidance: %v", err)
	}
	preserved, err := os.ReadFile(path)
	if err != nil || string(preserved) != string(raw) {
		t.Fatalf("legacy state changed: %q, %v", preserved, err)
	}
}
