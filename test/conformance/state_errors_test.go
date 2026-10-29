package conformance

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

func TestLocalPreservesIncompatibleStateAndAcceptsFreshPath(t *testing.T) {
	start := func(ctx context.Context, path string, ready func()) error {
		return localserver.Run(ctx, localserver.Settings{Host: "127.0.0.1", Port: 0, State: path}, "", nil, localserver.StateReset{},
			func(localserver.Status, *contextservice.Publication) { ready() })
	}
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
}

// A shared database migrated by a newer, incompatible release is refused
// rather than reset; an upgraded binary or a fresh database recovers.
func TestRemotePreservesIncompatibleDatabaseAndAcceptsFreshDatabase(t *testing.T) {
	start := func(ctx context.Context, databaseURL string, ready func()) error {
		return remoteserver.Run(ctx, remoteserver.Settings{Listen: "127.0.0.1:0", DatabaseURL: databaseURL}, func(string) { ready() })
	}
	databaseURL := pgtest.URL(t)
	store, err := pgstore.Open(t.Context(), databaseURL, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	retained := testsupport.RemoteAccount(t, store, "retain")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	setFloor := func(floor int) {
		t.Helper()
		conn, err := pgx.Connect(t.Context(), databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(t.Context())
		if _, err := conn.Exec(t.Context(), `UPDATE diffx.schema_compatibility SET floor=$1`, floor); err != nil {
			t.Fatal(err)
		}
	}
	setFloor(1000000)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = start(ctx, databaseURL, cancel)
	var refused *pgstore.SchemaError
	if !errors.As(err, &refused) || refused.Floor != 1000000 || !strings.Contains(err.Error(), "upgrade diffx-server") || strings.Contains(err.Error(), "--state") {
		t.Fatalf("missing upgrade guidance: %v", err)
	}
	reachedReady := false
	if err := start(ctx, pgtest.URL(t), func() {
		reachedReady = true
		cancel()
	}); err != nil || !reachedReady {
		t.Fatalf("fresh-database recovery failed: ready=%t, err=%v", reachedReady, err)
	}
	setFloor(1)
	store, err = pgstore.Open(t.Context(), databaseURL, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	accounts, err := store.IdentityAccounts(t.Context())
	if err != nil || len(accounts) != 1 || accounts[0].ID != retained.ID {
		t.Fatalf("refusal changed existing data: %#v, %v", accounts, err)
	}
}
