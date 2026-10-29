package pgstore_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/storetest"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// database is a test store's database, with a pool for test-only adjustments.
type database struct {
	url  string
	pool *pgxpool.Pool
}

func TestContract(t *testing.T) {
	t.Parallel()
	databases := make(map[*pgstore.Store]database)
	open := func(t *testing.T, selected database, retention time.Duration) *pgstore.Store {
		t.Helper()
		store, err := pgstore.Open(t.Context(), selected.url, retention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		databases[store] = selected
		return store
	}
	exec := func(t *testing.T, store *pgstore.Store, sql string, arguments ...any) {
		t.Helper()
		if _, err := databases[store].pool.Exec(context.Background(), sql, arguments...); err != nil {
			t.Fatal(err)
		}
	}
	storetest.Run(t, storetest.Harness[*pgstore.Store]{
		Open: func(t *testing.T, retention time.Duration) *pgstore.Store {
			url := pgtest.URL(t)
			config, err := pgstore.Config(url)
			if err != nil {
				t.Fatal(err)
			}
			pool, err := pgxpool.NewWithConfig(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pool.Close)
			return open(t, database{url: url, pool: pool}, retention)
		},
		Owner: func(t *testing.T, store *pgstore.Store, name string) string {
			id := rand.Text()
			exec(t, store, `INSERT INTO users(id, name, created_at) VALUES ($1, $2, now())`, id, name)
			return id
		},
		Expire: func(t *testing.T, store *pgstore.Store, contextID string, at time.Time) {
			exec(t, store, `UPDATE diffs SET expires_at=$1 WHERE id IN (SELECT diff_id FROM observation_scopes WHERE context_id=$2 UNION SELECT diff_id FROM observation_commits WHERE context_id=$2)`, time.UnixMilli(at.UnixMilli()), contextID)
		},
		Reopen: func(t *testing.T, store *pgstore.Store, retention time.Duration) *pgstore.Store {
			t.Helper()
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			return open(t, databases[store], retention)
		},
		Submitted: func(t *testing.T, store *pgstore.Store, contextID string, at time.Time) {
			exec(t, store, `UPDATE contexts SET last_submitted_at=$1 WHERE id=$2`, time.UnixMilli(at.UnixMilli()), contextID)
		},
	})
}
