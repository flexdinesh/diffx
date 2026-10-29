package pgstore_test

import (
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore"
)

func TestPruneRunsOnOneReplicaAtATime(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url, store, db := queueDatabase(t)
	second, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	owner := queueOwner(t, db, "owner")
	expired := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM diffx.github_oauth_states`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := store.GitHub().PutOAuthState(ctx, "state", owner, 1); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(10)

	// Another replica's pass holds the lock: concurrent passes skip without error.
	holder, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(pgstore.PruneLock)); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, replica := range []*pgstore.Store{store, second, store, second} {
		group.Go(func() {
			if err := replica.PruneExpired(now); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if expired() != 1 {
		t.Fatal("pruned while another replica held the prune lock")
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Without a holder, concurrent passes neither fail nor deadlock, and one prunes.
	for _, replica := range []*pgstore.Store{store, second, store, second} {
		group.Go(func() {
			if err := replica.PruneExpired(now); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if expired() != 0 {
		t.Fatal("expired state survived")
	}
}
