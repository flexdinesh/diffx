package conformance

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/remoteserver"
)

func TestBothModesPreserveIncompatibleStateAndAcceptFreshPath(t *testing.T) {
	for name, start := range map[string]func(context.Context, string, func()) error{
		"local": func(ctx context.Context, path string, ready func()) error {
			return localserver.Run(ctx, localserver.Settings{Host: "127.0.0.1", Port: 0, State: path}, "", nil,
				func(localserver.Status, *contextservice.Publication) { ready() })
		},
		"remote": func(ctx context.Context, path string, ready func()) error {
			return remoteserver.Run(ctx, remoteserver.Settings{Listen: "127.0.0.1:0", State: path, SignupEnabled: true},
				func(string) { ready() })
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('retain'); PRAGMA user_version=9"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err = start(ctx, path, cancel)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "--state /path/to/new-state.db") {
				t.Fatalf("missing recovery guidance: %v", err)
			}
			reachedReady := false
			if err := start(ctx, filepath.Join(filepath.Dir(path), "new-state.db"), func() {
				reachedReady = true
				cancel()
			}); err != nil || !reachedReady {
				t.Fatalf("fresh-path recovery failed: ready=%t, err=%v", reachedReady, err)
			}
			db, err = sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var value string
			if err := db.QueryRow("SELECT value FROM legacy").Scan(&value); err != nil || value != "retain" {
				t.Fatalf("recovery changed old data: %q, %v", value, err)
			}
		})
	}
}
