//go:build !postgres

package reviewstore

import (
	"testing"
	"time"
)

// openTestState opens the store behind path; an empty path is private.
func openTestState(_ testing.TB, path string, retention time.Duration) (*Store, error) {
	return OpenWithRetention(path, retention)
}

// rejectAnchorDeletion makes deleting an observation's anchor diff fail.
func rejectAnchorDeletion(t testing.TB, store *Store) {
	t.Helper()
	if _, err := store.db.Exec(`CREATE TRIGGER reject_anchor BEFORE DELETE ON diffs WHEN OLD.mode='all' BEGIN SELECT RAISE(ABORT,'anchor deletion failed'); END`); err != nil {
		t.Fatal(err)
	}
}
