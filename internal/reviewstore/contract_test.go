package reviewstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/storetest"
)

func TestContract(t *testing.T) {
	paths := make(map[*Store]string)
	open := func(t *testing.T, path string, retention time.Duration) *Store {
		t.Helper()
		store, err := OpenWithRetention(path, retention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		paths[store] = path
		return store
	}
	storetest.Run(t, storetest.Harness[*Store]{
		Open: func(t *testing.T, retention time.Duration) *Store {
			return open(t, filepath.Join(t.TempDir(), "state.db"), retention)
		},
		Owner:  func(t *testing.T, store *Store, name string) string { return testUser(t, store, name).ID },
		Expire: expire,
		Reopen: func(t *testing.T, store *Store, retention time.Duration) *Store {
			t.Helper()
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			return open(t, paths[store], retention)
		},
		Submitted: func(t *testing.T, store *Store, contextID string, at time.Time) {
			t.Helper()
			if _, err := store.db.Exec(`UPDATE contexts SET last_submitted_at=? WHERE id=?`, at.UnixMilli(), contextID); err != nil {
				t.Fatal(err)
			}
		},
	})
}

// expire sets the expiry of every diff of the context, scopes and commits.
func expire(t *testing.T, store *Store, contextID string, at time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE diffs SET expires_at=? WHERE id IN (SELECT diff_id FROM observation_scopes WHERE context_id=? UNION SELECT diff_id FROM observation_commits WHERE context_id=?)`, at.UnixMilli(), contextID, contextID); err != nil {
		t.Fatal(err)
	}
}
