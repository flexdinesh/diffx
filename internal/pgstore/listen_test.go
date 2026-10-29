package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore"
)

// listen runs a listener on store until the test ends.
func listen(t *testing.T, store *pgstore.Store) <-chan pgstore.Notification {
	t.Helper()
	listener, err := store.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	notifications := make(chan pgstore.Notification, 64)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- listener.Run(ctx, func(notification pgstore.Notification) { notifications <- notification })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return notifications
}

func expectNotification(t *testing.T, notifications <-chan pgstore.Notification, want pgstore.Notification) {
	t.Helper()
	select {
	case got := <-notifications:
		if got != want {
			t.Fatalf("notification = %+v, want %+v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no notification; want %+v", want)
	}
}

// Notifications are sent by the committing transaction, so another replica
// hears them in commit order and never hears a rolled-back change.
func TestContextNotificationsReachOtherReplicasOnlyOnCommit(t *testing.T) {
	t.Parallel()
	stores, owner, url := replicas(t, 2)
	notifications := listen(t, stores[1])
	first, err := stores[0].IngestContext(t.Context(), owner, request("first", "a"))
	if err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.ContextChanged, OwnerID: owner, ContextID: first.ContextID})

	// Hold the stream lock so deletion blocks after announcing itself, then roll it back.
	conn, waiter := identityConn(t, url), identityConn(t, url)
	var stream string
	if err := conn.QueryRow(t.Context(), `SELECT stream_key FROM diffx.observations WHERE context_id = $1`, first.ContextID).Scan(&stream); err != nil {
		t.Fatal(err)
	}
	hold, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hold.Exec(t.Context(), `SELECT pg_advisory_xact_lock(7469102, hashtext($1::text || '/' || $2::text))`, owner, stream); err != nil {
		t.Fatal(err)
	}
	deleting, cancel := context.WithCancel(t.Context())
	deleted := make(chan error, 1)
	go func() { deleted <- stores[0].DeleteContext(deleting, owner, first.ContextID) }()
	waitForLockWaiters(t, waiter, 1)
	cancel()
	if err := <-deleted; err == nil {
		t.Fatal("canceled deletion committed")
	}
	if err := hold.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}

	second, err := stores[0].IngestContext(t.Context(), owner, request("second", "b"))
	if err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.ContextChanged, OwnerID: owner, ContextID: second.ContextID})
	if err := stores[0].DeleteContext(t.Context(), owner, first.ContextID); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.ContextChanged, OwnerID: owner, ContextID: first.ContextID})
}

func TestQueuedJobsAndAccountRemovalNotifyReplicas(t *testing.T) {
	t.Parallel()
	stores, owner, _ := replicas(t, 2)
	notifications := listen(t, stores[1])
	if _, err := stores[0].Queue().Accept(t.Context(), owner, submission("job")); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.JobQueued})

	account, _ := member(t, stores[0], "member")
	now := time.Now()
	if err := stores[0].SetAccountDisabled(t.Context(), account.ID, true, now); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.AccountRemoved, OwnerID: account.ID})
	// Enabling announces nothing, so the next notification is the deletion.
	if err := stores[0].SetAccountDisabled(t.Context(), account.ID, false, now); err != nil {
		t.Fatal(err)
	}
	if err := stores[0].DeleteAccount(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.AccountRemoved, OwnerID: account.ID})
}

// A lost session reconnects and asks consumers to resync, since changes
// committed while it was gone were never delivered.
func TestListenerReconnectsWithResync(t *testing.T) {
	t.Parallel()
	stores, owner, url := replicas(t, 1)
	notifications := listen(t, stores[0])
	// Filter before terminating; WHERE conditions may evaluate in any order.
	var terminated int
	if err := identityConn(t, url).QueryRow(t.Context(), `WITH listening AS MATERIALIZED (SELECT pid FROM pg_stat_activity WHERE datname = current_database() AND query LIKE 'LISTEN %')
		SELECT COUNT(*) FROM listening WHERE pg_terminate_backend(pid)`).Scan(&terminated); err != nil {
		t.Fatal(err)
	}
	if terminated != 1 {
		t.Fatalf("terminated %d listening sessions", terminated)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.Resync})
	binding, err := stores[0].IngestContext(t.Context(), owner, request("after", "d"))
	if err != nil {
		t.Fatal(err)
	}
	expectNotification(t, notifications, pgstore.Notification{Kind: pgstore.ContextChanged, OwnerID: owner, ContextID: binding.ContextID})
}
