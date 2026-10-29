package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

func TestAuthAttemptWindowsAreSharedAcrossReplicas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.URL(t)
	replicas := make([]*pgstore.Store, 2)
	for i := range replicas {
		store, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		replicas[i] = store
	}
	count := func(store *pgstore.Store, client string, at time.Time) int {
		t.Helper()
		attempts, err := store.CountAuthAttempt(ctx, client, at, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return attempts
	}
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range 6 {
		if got := count(replicas[i%2], "192.0.2.1", start.Add(time.Duration(i)*time.Second)); got != i+1 {
			t.Fatalf("attempt %d counted %d", i+1, got)
		}
	}
	if got := count(replicas[1], "192.0.2.2", start.Add(10*time.Second)); got != 1 {
		t.Fatalf("other client shares a window: %d", got)
	}
	// The window lasts a minute from the client's first attempt.
	if got := count(replicas[0], "192.0.2.1", start.Add(time.Minute-time.Second)); got != 7 {
		t.Fatalf("attempt inside window counted %d", got)
	}
	if got := count(replicas[1], "192.0.2.1", start.Add(time.Minute)); got != 1 {
		t.Fatalf("attempt after window counted %d", got)
	}
	if got := count(replicas[0], "192.0.2.1", start.Add(time.Minute+time.Second)); got != 2 {
		t.Fatalf("restarted window counted %d", got)
	}
	// Starting a window prunes ended ones.
	if got := count(replicas[0], "192.0.2.3", start.Add(2*time.Minute)); got != 1 {
		t.Fatalf("new client counted %d", got)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var clients []string
	rows, err := conn.Query(ctx, `SELECT client FROM diffx.auth_attempts ORDER BY client`)
	if err != nil {
		t.Fatal(err)
	}
	if clients, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0] != "192.0.2.3" {
		t.Fatalf("ended windows remain: %v", clients)
	}
}
