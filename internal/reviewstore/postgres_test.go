//go:build postgres

package reviewstore

import (
	"context"
	"database/sql"
	"maps"
	neturl "net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport/postgrestest"
)

func TestPostgresSchemaMatchesSQLite(t *testing.T) {
	sqlite, err := OpenSQLite("", DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	postgres, err := OpenPostgres(t.Context(), postgrestest.URL(t), DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer postgres.Close()

	want := map[string]string{}
	tables, err := sqlite.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	names := scanStrings(t, tables)
	for _, table := range names {
		columns, err := sqlite.db.Query(`SELECT name || CASE WHEN "notnull" OR pk THEN ' not null' ELSE '' END FROM pragma_table_info(?) ORDER BY name`, table)
		if err != nil {
			t.Fatal(err)
		}
		want[table] = strings.Join(scanStrings(t, columns), ",")
	}
	got := map[string]string{}
	rows, err := postgres.db.Query(`SELECT table_name, string_agg(column_name || CASE WHEN is_nullable='NO' THEN ' not null' ELSE '' END, ',' ORDER BY column_name)
		FROM information_schema.columns WHERE table_schema=current_schema() AND table_name<>'diffx_schema' GROUP BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, columns string
		if err := rows.Scan(&table, &columns); err != nil {
			t.Fatal(err)
		}
		got[table] = columns
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range slices.Sorted(maps.Keys(want)) {
		if got[table] != want[table] {
			t.Errorf("table %s columns:\npostgres %s\nsqlite   %s", table, got[table], want[table])
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			t.Errorf("postgres table %s is absent from SQLite", table)
		}
	}
}

func scanStrings(t *testing.T, rows *sql.Rows) []string {
	t.Helper()
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func TestPostgresOwnershipIsExclusiveAndReopens(t *testing.T) {
	url := postgrestest.URL(t)
	first, err := OpenPostgres(t.Context(), url, DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := OpenPostgres(t.Context(), url, DefaultRetention); err == nil {
		second.Close()
		t.Fatal("second owner opened an owned database")
	} else if parsed, parseErr := neturl.Parse(url); parseErr != nil {
		t.Fatal(parseErr)
	} else if password, ok := parsed.User.Password(); ok && strings.Contains(err.Error(), password) {
		t.Fatalf("ownership error exposes the password: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPostgres(t.Context(), url, DefaultRetention)
	if err != nil {
		t.Fatalf("reopen after owner closed: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLostOwnershipStopsHolding(t *testing.T) {
	store, err := OpenPostgres(t.Context(), postgrestest.URL(t), DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ready(t.Context()); err != nil {
		t.Fatalf("ready owner: %v", err)
	}
	holding := make(chan error, 1)
	go func() { holding <- store.HoldOwnership(t.Context()) }()
	if _, err := store.db.Exec(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND granted AND pid<>pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	if err := store.Ready(t.Context()); err == nil {
		t.Fatal("ready after losing the ownership session")
	}
	select {
	case err := <-holding:
		if err == nil {
			t.Fatal("HoldOwnership returned without an error after losing ownership")
		}
	case <-time.After(3 * ownershipInterval):
		t.Fatal("HoldOwnership kept running after losing ownership")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.HoldOwnership(ctx); err != nil {
		t.Fatalf("canceled hold: %v", err)
	}
}
