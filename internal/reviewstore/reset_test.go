package reviewstore

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
	_, err = db.Exec("CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('retain'); PRAGMA user_version=10")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		if store, err := Open(path); err == nil {
			store.Close()
			t.Fatal("accepted older schema")
		} else {
			for _, text := range []string{path, "schema 10 is older than supported schema 15", "supporting schema 10", "automatic reset is disabled", "back up", "SQLite sidecar files", "--state /path/to/new-state.db", "no saved reviews or server accounts"} {
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
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatalf("older schema version changed: %d %v", version, err)
	}
}

func TestAdditiveSchemaUpgradesInPlace(t *testing.T) {
	// Schema 11 had no GitHub integration tables; neither it nor schema 12 had
	// administration or Google sign-in tables. Schemas before 14 had no
	// captured commit tables, and before 15 no push notification tables.
	push := "DROP TABLE push_deliveries; DROP TABLE push_notifications; DROP TABLE push_subscriptions; DROP TABLE push_keys; DROP TABLE github_follow_mutes; DROP TABLE github_pr_heads; "
	commits := "DROP TABLE observation_commit_ranges; DROP TABLE observation_commits; " + push
	administration := "DROP TABLE google_auth_states; DROP TABLE account_invites; DROP TABLE google_identities; DROP TABLE disabled_accounts; DROP TABLE admin_setup; DROP TABLE admin_account; " + commits
	for version, statements := range map[int]string{
		11: "DROP TABLE github_follows; DROP TABLE github_connections; DROP TABLE github_oauth_states; DROP TABLE github_deliveries; DROP TABLE github_pull_requests; " + administration,
		12: administration,
		13: commits,
		14: push,
	} {
		t.Run(fmt.Sprintf("schema %d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			user, err := store.User("owner", "owner")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(fmt.Sprintf("%sPRAGMA user_version=%d", statements, version)); err != nil {
				t.Fatal(err)
			}
			store.Close()
			store, err = Open(path)
			if err != nil {
				t.Fatalf("schema %d must upgrade in place: %v", version, err)
			}
			defer store.Close()
			if _, err := store.GitHub().Follows(t.Context(), user.ID); err != nil {
				t.Fatalf("upgrade did not create integration tables: %v", err)
			}
			if exists, err := store.AdminExists(t.Context()); err != nil || exists {
				t.Fatalf("upgrade did not create administration tables: %v %v", exists, err)
			}
			if _, _, err := store.ObservationCommits(t.Context(), user.ID, "missing"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("upgrade did not create commit tables: %v", err)
			}
			if devices, err := store.Push().Subscriptions(t.Context(), user.ID); err != nil || len(devices) != 0 {
				t.Fatalf("upgrade did not create push tables: %v %v", devices, err)
			}
			if head, _, err := store.GitHub().PullRequestHead(t.Context(), 1, 1); err != nil || head != "" {
				t.Fatalf("upgrade did not create pull request head table: %q %v", head, err)
			}
			var upgraded int
			if err := store.db.QueryRow("PRAGMA user_version").Scan(&upgraded); err != nil || upgraded != schemaVersion {
				t.Fatalf("upgraded version %d %v", upgraded, err)
			}
			again, err := store.User("owner", "owner")
			if err != nil || again.ID != user.ID {
				t.Fatalf("upgrade lost state: %+v %v", again, err)
			}
		})
	}
}

func TestFailedUpgradeRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// A conflicting object makes the schema 13 statements fail midway.
	if _, err := store.db.Exec("DROP TABLE github_follows; DROP TABLE github_oauth_states; CREATE VIEW github_follows AS SELECT 1 AS owner_id; PRAGMA user_version=11"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("conflicting upgrade succeeded")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 11 {
		t.Fatalf("failed upgrade advanced version: %d %v", version, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='github_oauth_states'").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("failed upgrade kept partial tables: %d %v", tables, err)
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

func writeSchema(t *testing.T, path, statements string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(statements); err != nil {
		t.Fatal(err)
	}
}

func TestResetBacksUpOlderStateBesideItAndKeepsLatestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	writeSchema(t, path, "CREATE TABLE legacy(value TEXT); INSERT INTO legacy VALUES('retain'); PRAGMA user_version=10")
	if err := os.WriteFile(path+"-shm", []byte("sidecar"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "state.db.notes")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Reset(path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first) != dir || !strings.HasPrefix(filepath.Base(first), "state.db.schema10-") || !strings.HasSuffix(first, ".bak") {
		t.Fatalf("unexpected backup path %q", first)
	}
	for _, moved := range []string{path, path + "-shm"} {
		if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s remained after reset: %v", moved, err)
		}
	}
	if data, err := os.ReadFile(first + "-shm"); err != nil || string(data) != "sidecar" {
		t.Fatalf("sidecar not moved with backup: %q %v", data, err)
	}
	backup, err := sql.Open("sqlite", first)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	var version int
	if err := backup.QueryRow("SELECT value FROM legacy").Scan(&value); err != nil || value != "retain" {
		t.Fatalf("backup lost data: %q %v", value, err)
	}
	if err := backup.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatalf("backup schema changed: %d %v", version, err)
	}
	backup.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("fresh state did not open: %v", err)
	}
	store.Close()
	second, err := Reset(path)
	if err != nil {
		t.Fatalf("current schema not reset: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(second), fmt.Sprintf("state.db.schema%d-", schemaVersion)) {
		t.Fatalf("unexpected backup path %q", second)
	}
	for _, pruned := range []string{first, first + "-shm"} {
		if _, err := os.Stat(pruned); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("earlier backup %s kept: %v", pruned, err)
		}
	}
	for _, kept := range []string{second, unrelated} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s removed: %v", kept, err)
		}
	}
}

func TestResetRefusesAmbiguousStateUnchanged(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, path string){
		"newer": func(t *testing.T, path string) {
			writeSchema(t, path, "CREATE TABLE future(value TEXT); PRAGMA user_version=999")
		},
		"unversioned": func(t *testing.T, path string) { writeSchema(t, path, "CREATE TABLE unrelated(value TEXT)") },
		"foreign": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("not a database file"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			prepare(t, path)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			backup, err := Reset(path)
			var failure *StateError
			if backup != "" || !errors.As(err, &failure) || failure.Older() || !strings.Contains(err.Error(), "--state /path/to/new-state.db") {
				t.Fatalf("reset accepted ambiguous state: %q %v", backup, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("ambiguous state changed: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".bak") {
					t.Fatalf("backup created for refused state: %s", entry.Name())
				}
			}
		})
	}
}

func TestResetWithoutStateAndWhileOwned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if backup, err := Reset(path); backup != "" || err != nil {
		t.Fatalf("missing state: %q %v", backup, err)
	}
	if _, err := Reset(""); err == nil {
		t.Fatal("reset accepted in-memory state")
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if backup, err := Reset(path); err == nil || backup != "" {
		t.Fatalf("reset state owned by an open store: %q %v", backup, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("owned state moved: %v", err)
	}
}

func TestOpenReportsTypedStateError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	writeSchema(t, path, "CREATE TABLE legacy(value TEXT); PRAGMA user_version=10")
	_, err := Open(path)
	var failure *StateError
	if !errors.As(err, &failure) || !failure.Older() || failure.Version != 10 || failure.Supported() != schemaVersion || failure.Path != path {
		t.Fatalf("older state not reported as resettable: %#v %v", failure, err)
	}
}
