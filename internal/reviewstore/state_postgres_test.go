//go:build postgres

package reviewstore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport/postgrestest"
)

var testDatabases sync.Map

// openTestState maps each state path to its own Postgres database, so reopening
// a path reopens its data; an empty path is private.
func openTestState(t testing.TB, path string, retention time.Duration) (*Store, error) {
	url := postgrestest.URL(t)
	if path != "" && path != ":memory:" {
		existing, _ := testDatabases.LoadOrStore(path, url)
		if value, ok := existing.(string); ok {
			url = value
		}
	}
	return OpenPostgres(context.Background(), url, retention)
}

// rejectAnchorDeletion makes deleting an observation's anchor diff fail.
func rejectAnchorDeletion(t testing.TB, store *Store) {
	t.Helper()
	for _, statement := range []string{
		`CREATE FUNCTION reject_anchor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.mode='all' THEN RAISE EXCEPTION 'anchor deletion failed'; END IF; RETURN OLD; END $$`,
		`CREATE TRIGGER reject_anchor BEFORE DELETE ON diffs FOR EACH ROW EXECUTE FUNCTION reject_anchor()`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
