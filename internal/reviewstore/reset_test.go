package reviewstore

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOlderSchemaRefusedWithoutReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('retain'); PRAGMA user_version=9")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		if store, err := Open(path); err == nil {
			store.Close()
			t.Fatal("accepted older schema")
		} else {
			for _, text := range []string{path, "schema 9 is older than supported schema 10", "supporting schema 9", "automatic reset is disabled", "back up", "SQLite sidecar files", "--state /path/to/new-state.db", "no saved reviews or server accounts"} {
				if !strings.Contains(err.Error(), text) {
					t.Fatalf("missing recovery guidance %q: %v", text, err)
				}
			}
		}
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM legacy").Scan(&value); err != nil || value != "retain" {
		t.Fatalf("older data changed: %q %v", value, err)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
		t.Fatalf("older schema version changed: %d %v", version, err)
	}
}

func TestCurrentSchemaSurvivesRestartAndFutureVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.User("owner", "owner")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := store.User("owner", "owner")
	if err != nil || retained.ID != user.ID {
		t.Fatalf("current schema reset on reopen: %+v %v", retained, err)
	}
	if _, err := store.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if newer, err := Open(path); err == nil {
		newer.Close()
		t.Fatal("accepted future schema")
	} else {
		for _, text := range []string{path, "supporting schema 999", "--state /path/to/new-state.db"} {
			if !strings.Contains(err.Error(), text) {
				t.Fatalf("missing recovery guidance %q: %v", text, err)
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatalf("future state changed: %d %v", count, err)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 999 {
		t.Fatalf("future schema version changed: %d %v", version, err)
	}
}

func TestUnversionedDatabaseRefusedWithRecoveryGuidance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES('retain')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("accepted unversioned database")
	} else if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "--state /path/to/new-state.db") {
		t.Fatalf("missing recovery guidance: %v", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM unrelated").Scan(&value); err != nil || value != "retain" {
		t.Fatalf("unversioned data changed: %q %v", value, err)
	}
}
