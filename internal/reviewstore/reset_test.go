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
	"time"
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
			for _, text := range []string{path, "schema 10 is older than supported schema 16", "supporting schema 10", "automatic reset is disabled", "back up", "SQLite sidecar files", "--state /path/to/new-state.db", "no saved reviews"} {
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

// schema15Server recreates, with their keys, references and indexes, the
// server-only tables schema 15 and earlier created in every local database.
const schema15Server = `
CREATE TABLE ingestion_jobs (id TEXT PRIMARY KEY, sequence INTEGER NOT NULL UNIQUE REFERENCES ingestion_records(sequence), payload TEXT NOT NULL, state TEXT NOT NULL);
CREATE INDEX ingestion_jobs_ready ON ingestion_jobs(state);
CREATE TABLE remote_accounts (user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL);
CREATE TABLE browser_sessions (secret_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE);
CREATE INDEX browser_sessions_owner ON browser_sessions(user_id);
CREATE TABLE identity_tokens (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE, secret_hash TEXT NOT NULL UNIQUE);
CREATE INDEX identity_tokens_owner ON identity_tokens(user_id);
CREATE TABLE admin_account (singleton INTEGER PRIMARY KEY CHECK(singleton=1), user_id TEXT NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE);
CREATE TABLE admin_setup (singleton INTEGER PRIMARY KEY CHECK(singleton=1), token_hash TEXT NOT NULL);
CREATE TABLE disabled_accounts (user_id TEXT PRIMARY KEY REFERENCES remote_accounts(user_id) ON DELETE CASCADE);
CREATE TABLE google_identities (sub TEXT PRIMARY KEY, user_id TEXT NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE, email TEXT NOT NULL);
CREATE INDEX google_identities_email ON google_identities(email);
CREATE TABLE account_invites (id TEXT PRIMARY KEY, email TEXT NOT NULL, used_by TEXT REFERENCES remote_accounts(user_id) ON DELETE SET NULL);
CREATE INDEX account_invites_email ON account_invites(email);
CREATE TABLE google_auth_states (state_hash TEXT PRIMARY KEY, invite_id TEXT REFERENCES account_invites(id) ON DELETE CASCADE);
CREATE TABLE github_oauth_states (state_hash TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE);
CREATE TABLE github_connections (owner_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, user_id INTEGER NOT NULL);
CREATE INDEX github_connections_user ON github_connections(user_id);
CREATE TABLE github_follows (owner_id TEXT NOT NULL REFERENCES github_connections(owner_id) ON DELETE CASCADE, repository_id INTEGER NOT NULL, PRIMARY KEY(owner_id, repository_id));
CREATE INDEX github_follows_repository ON github_follows(repository_id);
CREATE TABLE github_deliveries (id TEXT PRIMARY KEY, received_at INTEGER NOT NULL);
CREATE TABLE github_pull_requests (repository_id INTEGER NOT NULL, number INTEGER NOT NULL, PRIMARY KEY(repository_id, number));
CREATE TABLE github_pr_heads (repository_id INTEGER NOT NULL, number INTEGER NOT NULL, PRIMARY KEY(repository_id, number), FOREIGN KEY(repository_id, number) REFERENCES github_pull_requests(repository_id, number) ON DELETE CASCADE);
CREATE TABLE github_follow_mutes (owner_id TEXT NOT NULL, repository_id INTEGER NOT NULL, PRIMARY KEY(owner_id, repository_id), FOREIGN KEY(owner_id, repository_id) REFERENCES github_follows(owner_id, repository_id) ON DELETE CASCADE);
CREATE TABLE push_keys (singleton INTEGER PRIMARY KEY CHECK(singleton=1), public_key TEXT NOT NULL);
CREATE TABLE push_subscriptions (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE);
CREATE INDEX push_subscriptions_owner ON push_subscriptions(owner_id);
CREATE TABLE push_notifications (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, job_id TEXT NOT NULL REFERENCES ingestion_jobs(id) ON DELETE CASCADE);
CREATE INDEX push_notifications_job ON push_notifications(job_id);
CREATE TABLE push_deliveries (notification_id TEXT NOT NULL REFERENCES push_notifications(id) ON DELETE CASCADE, subscription_id TEXT NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE, PRIMARY KEY(notification_id, subscription_id));
CREATE INDEX push_deliveries_subscription ON push_deliveries(subscription_id);
`

// serverObjects counts schema objects that only server state used.
func serverObjects(t *testing.T, db interface {
	QueryRow(string, ...any) *sql.Row
}) int {
	t.Helper()
	var count int
	query := `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name IN ('` + strings.Join(serverTables, "','") + `')`
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAdditiveSchemaUpgradesInPlace(t *testing.T) {
	// Schemas before 14 had no captured commit tables. Every schema from 11
	// through 15 created server-only tables, which schema 16 drops.
	commits := "DROP TABLE observation_commit_ranges; DROP TABLE observation_commits; "
	for version, statements := range map[int]string{11: commits, 12: commits, 13: commits, 14: "", 15: ""} {
		t.Run(fmt.Sprintf("schema %d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if count := serverObjects(t, store.db); count != 0 {
				t.Fatalf("fresh state created %d server objects", count)
			}
			user, err := store.User("owner", "owner")
			if err != nil {
				t.Fatal(err)
			}
			request := observationRequest("source", "kept")
			binding, err := store.Ingest(user.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(fmt.Sprintf("%s%sPRAGMA user_version=%d", statements, schema15Server, version)); err != nil {
				t.Fatal(err)
			}
			store.Close()
			store, err = Open(path)
			if err != nil {
				t.Fatalf("schema %d must upgrade in place: %v", version, err)
			}
			defer store.Close()
			if count := serverObjects(t, store.db); count != 0 {
				t.Fatalf("upgrade kept %d server objects", count)
			}
			if _, _, err := store.ObservationCommits(t.Context(), user.ID, "missing"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("upgrade did not create commit tables: %v", err)
			}
			var upgraded int
			if err := store.db.QueryRow("PRAGMA user_version").Scan(&upgraded); err != nil || upgraded != schemaVersion {
				t.Fatalf("upgraded version %d %v", upgraded, err)
			}
			again, err := store.User("owner", "owner")
			if err != nil || again.ID != user.ID {
				t.Fatalf("upgrade lost state: %+v %v", again, err)
			}
			if _, err := store.Context(t.Context(), user.ID, binding.ContextID, time.Now()); err != nil {
				t.Fatalf("upgrade lost observation: %v", err)
			}
			replay, err := store.Ingest(user.ID, request)
			if err != nil || replay.ContextID != binding.ContextID {
				t.Fatalf("upgrade lost retry identity: %+v %v", replay, err)
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
	// A conflicting object makes the schema 16 drops fail after the schema 14
	// statements ran.
	if _, err := store.db.Exec("DROP TABLE observation_commit_ranges; DROP TABLE observation_commits; CREATE VIEW github_follows AS SELECT 1 AS owner_id; PRAGMA user_version=13"); err != nil {
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
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 13 {
		t.Fatalf("failed upgrade advanced version: %d %v", version, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='observation_commits'").Scan(&tables); err != nil || tables != 0 {
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
