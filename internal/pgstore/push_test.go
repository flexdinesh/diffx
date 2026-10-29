package pgstore_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/webpush"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pushAccount(t *testing.T, db *pgxpool.Pool, name string) string {
	t.Helper()
	id := queueOwner(t, db, "id-"+name)
	execSQL(t, db, `INSERT INTO diffx.remote_accounts(user_id, username, password_hash) VALUES ($1, $2, '')`, id, name)
	return id
}

// pushNotification records intent as admission does, linked to job.
func pushNotification(t *testing.T, db *pgxpool.Pool, id, owner, job string, availableAt int64) {
	t.Helper()
	execSQL(t, db, `INSERT INTO diffx.push_notifications(id, owner_id, job_id, kind, payload, state, available_at, created_at)
		VALUES ($1, $2, $3, 'pull_request', $4, 'pending', $5, $5)`, id, owner, job, []byte(`{"title":"t","body":"b"}`), time.UnixMilli(availableAt))
}

func TestPushKeysLoadOrCreateConcurrently(t *testing.T) {
	t.Parallel()
	_, store, _ := queueDatabase(t)
	const callers = 16
	results := make([]webpush.Keys, callers)
	failures := make([]error, callers)
	var group sync.WaitGroup
	for i := range callers {
		group.Go(func() {
			generated := webpush.Keys{Public: fmt.Sprintf("public-%d", i), Private: fmt.Sprintf("private-%d", i)}
			results[i], failures[i] = store.Push().PushKeys(t.Context(), generated, 1)
		})
	}
	group.Wait()
	for i := range callers {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		if results[i] != results[0] || results[0].Public == "" || results[0].Public[len("public-"):] != results[0].Private[len("private-"):] {
			t.Fatalf("caller %d keys %+v, first %+v", i, results[i], results[0])
		}
	}
	again, err := store.Push().PushKeys(t.Context(), webpush.Keys{Public: "new", Private: "new"}, 2)
	if err != nil || again != results[0] {
		t.Fatalf("keys rotated: %+v %v", again, err)
	}
}

func TestPushSubscriptionsRebindAndCascadeWithAccounts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	push := store.Push()
	alice, bob := pushAccount(t, db, "alice"), pushAccount(t, db, "bob")
	device := pushservice.Subscription{OwnerID: alice, Endpoint: "https://push.test/a", P256dh: "key", Auth: "auth", Label: "Phone", CreatedAt: 1}
	first, err := push.PutSubscription(ctx, device)
	if err != nil || first.ID == "" || first.CreatedAt != 1 || first.LastSuccessAt != nil {
		t.Fatalf("register: %+v %v", first, err)
	}
	if err := push.RecordSuccess(ctx, first.ID, 5); err != nil {
		t.Fatal(err)
	}
	device.Label, device.P256dh, device.CreatedAt = "Renamed", "rotated", 2
	again, err := push.PutSubscription(ctx, device)
	if err != nil || again.ID != first.ID || again.CreatedAt != 1 || again.Label != "Renamed" || again.P256dh != "rotated" || again.LastSuccessAt == nil || *again.LastSuccessAt != 5 {
		t.Fatalf("same account re-registration: %+v %v", again, err)
	}
	if devices, err := push.Subscriptions(ctx, alice); err != nil || len(devices) != 1 || devices[0].ID != first.ID || devices[0].Label != "Renamed" || devices[0].P256dh != "rotated" {
		t.Fatalf("stored device: %+v %v", devices, err)
	}
	if err := push.DeleteSubscription(ctx, bob, first.ID); !errors.Is(err, pushservice.ErrNotFound) {
		t.Fatalf("another account deleted the device: %v", err)
	}
	device.OwnerID = bob
	moved, err := push.PutSubscription(ctx, device)
	if err != nil || moved.ID == first.ID || moved.OwnerID != bob || moved.CreatedAt != 2 || moved.LastSuccessAt != nil {
		t.Fatalf("rebind: %+v %v", moved, err)
	}
	if devices, err := push.Subscriptions(ctx, alice); err != nil || len(devices) != 0 {
		t.Fatalf("endpoint stayed with its previous account: %+v %v", devices, err)
	}

	job, err := store.Queue().Accept(ctx, bob, queueRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	pushNotification(t, db, "n-bob", bob, job.ID, 1)
	// Account deletion removes jobs first; they do not cascade from records.
	execSQL(t, db, `DELETE FROM diffx.ingestion_jobs WHERE sequence IN (SELECT sequence FROM diffx.ingestion_records WHERE owner_id = $1)`, bob)
	execSQL(t, db, `DELETE FROM diffx.users WHERE id = $1`, bob)
	if devices, err := push.Subscriptions(ctx, bob); err != nil || len(devices) != 0 {
		t.Fatalf("devices survived account deletion: %+v %v", devices, err)
	}
	if due, err := push.ClaimNotifications(ctx, 10, 10, 100); err != nil || len(due) != 0 {
		t.Fatalf("notifications survived account deletion: %+v %v", due, err)
	}
	if err := push.DeleteSubscription(ctx, alice, "missing"); !errors.Is(err, pushservice.ErrNotFound) {
		t.Fatalf("deleted a missing device: %v", err)
	}
}

func TestPushSubscriptionConcurrentRegistrationKeepsOneDevice(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	alice, bob := pushAccount(t, db, "alice"), pushAccount(t, db, "bob")
	const callers = 12
	results := make([]pushservice.Subscription, callers)
	failures := make([]error, callers)
	var group sync.WaitGroup
	for i := range callers {
		group.Go(func() {
			owner := alice
			if i%2 == 1 {
				owner = bob
			}
			results[i], failures[i] = store.Push().PutSubscription(ctx, pushservice.Subscription{OwnerID: owner, Endpoint: "https://push.test/shared", P256dh: "key", Auth: "auth", Label: "Phone", CreatedAt: int64(i)})
		})
	}
	group.Wait()
	for i := range callers {
		if failures[i] != nil {
			t.Fatalf("caller %d: %v", i, failures[i])
		}
	}
	var count int
	var id, owner string
	if err := db.QueryRow(ctx, `SELECT count(*) OVER (), id, owner_id FROM diffx.push_subscriptions`).Scan(&count, &id, &owner); err != nil || count != 1 {
		t.Fatalf("endpoint rows %d: %v", count, err)
	}
	found := false
	for _, result := range results {
		found = found || result.ID == id && result.OwnerID == owner
	}
	if !found {
		t.Fatalf("stored device %s of %s returned to no caller: %+v", id, owner, results)
	}
}

func TestClaimNotificationsLinkOwnedJobsAndEnabledAccounts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	push := store.Push()
	alice, bob := pushAccount(t, db, "alice"), pushAccount(t, db, "bob")
	local := queueOwner(t, db, "local")
	job, err := store.Queue().Accept(ctx, alice, queueRequest("a"))
	if err != nil {
		t.Fatal(err)
	}
	pushNotification(t, db, "n-alice", alice, job.ID, 10)
	// Bob's notification names Alice's job; it never resolves her publication.
	pushNotification(t, db, "n-bob", bob, job.ID, 20)
	pushNotification(t, db, "n-local", local, job.ID, 30)
	pushNotification(t, db, "n-later", alice, job.ID, 1_000)

	// Each claim's lease lapses at 30, so later claims at 30 see every row again.
	if limited, err := push.ClaimNotifications(ctx, 30, 30, 1); err != nil || len(limited) != 1 || limited[0].ID != "n-alice" || limited[0].Token == "" {
		t.Fatalf("limit: %+v %v", limited, err)
	}
	due, err := push.ClaimNotifications(ctx, 30, 30, 100)
	if err != nil || len(due) != 3 {
		t.Fatalf("due %+v %v", due, err)
	}
	if first := due[0]; first.ID != "n-alice" || first.OwnerID != alice || first.Kind != "pull_request" || first.CreatedAt != 10 || first.Attempt != 0 ||
		!first.Available || first.JobState != "queued" || first.ContextID != "" || string(first.Content) != `{"title":"t","body":"b"}` {
		t.Fatalf("notification %+v", first)
	}
	if foreign := due[1]; foreign.ID != "n-bob" || !foreign.Available || foreign.JobState != "" {
		t.Fatalf("foreign job resolved: %+v", foreign)
	}
	if accountless := due[2]; accountless.ID != "n-local" || accountless.Available {
		t.Fatalf("account without remote identity available: %+v", accountless)
	}

	execSQL(t, db, `INSERT INTO diffx.disabled_accounts(user_id, disabled_at) VALUES ($1, now())`, alice)
	delivery, err := store.Queue().Claim(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Queue().Complete(ctx, delivery, "context-a"); err != nil {
		t.Fatal(err)
	}
	due, err = push.ClaimNotifications(ctx, 30, 30, 100)
	if err != nil || len(due) != 3 || due[0].ID != "n-alice" || due[0].Available || due[0].JobState != "succeeded" || due[0].ContextID != "context-a" {
		t.Fatalf("disabled account or published job: %+v %v", due, err)
	}
}

func TestPushDeliveriesRecordEachDeviceOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	push := store.Push()
	alice, bob := pushAccount(t, db, "alice"), pushAccount(t, db, "bob")
	phone, err := push.PutSubscription(ctx, pushservice.Subscription{OwnerID: alice, Endpoint: "https://push.test/phone", P256dh: "k", Auth: "a", Label: "Phone", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := push.PutSubscription(ctx, pushservice.Subscription{OwnerID: alice, Endpoint: "https://push.test/laptop", P256dh: "k", Auth: "a", Label: "Laptop", CreatedAt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := push.PutSubscription(ctx, pushservice.Subscription{OwnerID: bob, Endpoint: "https://push.test/bob", P256dh: "k", Auth: "a", Label: "Bob", CreatedAt: 0}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Queue().Accept(ctx, alice, queueRequest("a"))
	if err != nil {
		t.Fatal(err)
	}
	pushNotification(t, db, "n", alice, job.ID, 1)

	pending, err := push.Undelivered(ctx, "n")
	if err != nil || len(pending) != 2 || pending[0].ID != phone.ID || pending[1].ID != laptop.ID {
		t.Fatalf("undelivered %+v %v", pending, err)
	}
	claims, err := push.ClaimNotifications(ctx, 1, 100, 10)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim %+v %v", claims, err)
	}
	claim := claims[0]
	if err := push.RecordDelivery(ctx, claim, phone.ID, true, 42); err != nil {
		t.Fatal(err)
	}
	if err := push.RecordDelivery(ctx, claim, phone.ID, false, 43); err != nil {
		t.Fatal(err)
	}
	if err := push.RecordDelivery(ctx, claim, laptop.ID, false, 44); err != nil {
		t.Fatal(err)
	}
	var states []string
	rows, err := db.Query(ctx, `SELECT state FROM diffx.push_deliveries ORDER BY state`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var state string
		if err := rows.Scan(&state); err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	if rows.Err() != nil || fmt.Sprint(states) != "[accepted refused]" {
		t.Fatalf("outcomes %v %v", states, rows.Err())
	}
	if pending, err := push.Undelivered(ctx, "n"); err != nil || len(pending) != 0 {
		t.Fatalf("recorded devices undelivered: %+v %v", pending, err)
	}
	devices, err := push.Subscriptions(ctx, alice)
	if err != nil || devices[0].LastSuccessAt == nil || *devices[0].LastSuccessAt != 42 || devices[1].LastSuccessAt != nil {
		t.Fatalf("last success: %+v %v", devices, err)
	}
	// A device removed while sending has no outcome to keep.
	if err := push.RecordDelivery(ctx, claim, "removed-device", true, 50); err != nil {
		t.Fatalf("removed device: %v", err)
	}

	// Retrying releases the lease, so the notification is due again at 500.
	if err := push.RetryNotification(ctx, claim, 3, 500); err != nil {
		t.Fatal(err)
	}
	if due, err := push.ClaimNotifications(ctx, 499, 1_000, 10); err != nil || len(due) != 0 {
		t.Fatalf("retry backoff ignored: %+v %v", due, err)
	}
	due, err := push.ClaimNotifications(ctx, 500, 1_000, 10)
	if err != nil || len(due) != 1 || due[0].Attempt != 3 || due[0].CreatedAt != 1 {
		t.Fatalf("retried: %+v %v", due, err)
	}
	if err := push.FinishNotification(ctx, claim, "sent"); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("released claim finished: %v", err)
	}
	if err := push.FinishNotification(ctx, due[0], "sent"); err != nil {
		t.Fatal(err)
	}
	if err := push.FinishNotification(ctx, due[0], "dropped"); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("finished twice: %v", err)
	}
	if err := push.RetryNotification(ctx, due[0], 4, 1); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("finished notification retried: %v", err)
	}
	var state string
	var attempt int
	if err := db.QueryRow(ctx, `SELECT state, attempt FROM diffx.push_notifications WHERE id = 'n'`).Scan(&state, &attempt); err != nil || state != "sent" || attempt != 3 {
		t.Fatalf("finished notification changed: %s %d %v", state, attempt, err)
	}
	if due, err := push.ClaimNotifications(ctx, 2_000, 3_000, 10); err != nil || len(due) != 0 {
		t.Fatalf("finished notification due: %+v %v", due, err)
	}
}

func TestPushClaimsFromReplicasNeverOverlap(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url, first, db := queueDatabase(t)
	second, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	alice := pushAccount(t, db, "alice")
	job, err := first.Queue().Accept(ctx, alice, queueRequest("a"))
	if err != nil {
		t.Fatal(err)
	}
	const notifications = 40
	for i := range notifications {
		pushNotification(t, db, fmt.Sprintf("n-%02d", i), alice, job.ID, int64(i))
	}
	// Held claims are skipped: the second replica receives exactly the rest.
	held, err := first.Push().ClaimNotifications(ctx, 100, 1_000, 5)
	if err != nil || len(held) != 5 || held[0].ID != "n-00" {
		t.Fatalf("first claim %+v %v", held, err)
	}
	rest, err := second.Push().ClaimNotifications(ctx, 100, 1_000, 100)
	if err != nil || len(rest) != notifications-5 || rest[0].ID != "n-05" {
		t.Fatalf("second claim %d %v", len(rest), err)
	}

	// Concurrent claims after the leases lapse split the rows without overlap.
	var mu sync.Mutex
	claimed := make(map[string]int)
	var group sync.WaitGroup
	for _, store := range []*pgstore.Store{first, second, first, second} {
		group.Go(func() {
			for {
				claims, err := store.Push().ClaimNotifications(ctx, 1_000, 2_000, 3)
				if err != nil {
					t.Error(err)
					return
				}
				if len(claims) == 0 {
					return
				}
				mu.Lock()
				for _, claim := range claims {
					claimed[claim.ID]++
				}
				mu.Unlock()
			}
		})
	}
	group.Wait()
	if len(claimed) != notifications {
		t.Fatalf("claimed %d of %d", len(claimed), notifications)
	}
	for id, count := range claimed {
		if count != 1 {
			t.Fatalf("%s claimed %d times", id, count)
		}
	}
}

func TestPushStaleLeaseIsFencedAndExpiredLeaseReclaimed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	push := store.Push()
	alice := pushAccount(t, db, "alice")
	phone, err := push.PutSubscription(ctx, pushservice.Subscription{OwnerID: alice, Endpoint: "https://push.test/phone", P256dh: "k", Auth: "a", Label: "Phone", CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Queue().Accept(ctx, alice, queueRequest("a"))
	if err != nil {
		t.Fatal(err)
	}
	pushNotification(t, db, "n", alice, job.ID, 1)
	stale, err := push.ClaimNotifications(ctx, 1, 100, 10)
	if err != nil || len(stale) != 1 {
		t.Fatalf("claim %+v %v", stale, err)
	}
	if early, err := push.ClaimNotifications(ctx, 99, 200, 10); err != nil || len(early) != 0 {
		t.Fatalf("claimed a held lease: %+v %v", early, err)
	}
	current, err := push.ClaimNotifications(ctx, 100, 200, 10)
	if err != nil || len(current) != 1 || current[0].ID != "n" || current[0].Token == stale[0].Token {
		t.Fatalf("expired lease not reclaimed: %+v %v", current, err)
	}
	if err := push.RecordDelivery(ctx, stale[0], phone.ID, true, 5); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("stale record: %v", err)
	}
	if err := push.RetryNotification(ctx, stale[0], 1, 1); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("stale retry: %v", err)
	}
	if err := push.FinishNotification(ctx, stale[0], "dropped"); !errors.Is(err, pushservice.ErrLeaseLost) {
		t.Fatalf("stale finish: %v", err)
	}
	if undelivered, err := push.Undelivered(ctx, "n"); err != nil || len(undelivered) != 1 {
		t.Fatalf("stale holder recorded an outcome: %+v %v", undelivered, err)
	}
	if err := push.RecordDelivery(ctx, current[0], phone.ID, true, 6); err != nil {
		t.Fatal(err)
	}
	if err := push.FinishNotification(ctx, current[0], "sent"); err != nil {
		t.Fatal(err)
	}
	var state string
	var attempt int
	if err := db.QueryRow(ctx, `SELECT state, attempt FROM diffx.push_notifications WHERE id = 'n'`).Scan(&state, &attempt); err != nil || state != "sent" || attempt != 0 {
		t.Fatalf("notification %s %d %v", state, attempt, err)
	}
}

func TestStreamIDResolvesTheContextsCurrentHead(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	alice, bob := pushAccount(t, db, "alice"), pushAccount(t, db, "bob")
	observe := func(id, metadata string, expiresAt *time.Time) {
		execSQL(t, db, `INSERT INTO diffx.diffs(id, owner_id, kind, mode, created_at, expires_at) VALUES ($1, $2, 'observation', 'all', now(), $3)`, id, alice, expiresAt)
		execSQL(t, db, `INSERT INTO diffx.contexts(id, owner_id, kind, anchor_diff_id, created_at, last_submitted_at) VALUES ($1, $2, 'observation', $1, now(), now())`, id, alice)
		execSQL(t, db, `INSERT INTO diffx.observations(context_id, owner_id, source_id, submission_id, payload_hash, metadata, stream_key, comparison_policy)
			VALUES ($1, $2, 'source', $1, 'hash', $3, $1, 'branch:main')`, id, alice, metadata)
	}
	expires := time.UnixMilli(1_000)
	observe("current", `{"repositoryKey":"repo","checkoutKey":"checkout","branch":"label","branchId":"branch-id"}`, &expires)
	observe("headless", `{"repositoryKey":"repo","checkoutKey":"other","branch":"label"}`, nil)
	execSQL(t, db, `INSERT INTO diffx.observation_stream_heads(owner_id, source_id, repository_key, checkout_key, branch, comparison_policy, context_id, collected_at)
		VALUES ($1, 'source', 'repo', 'checkout', 'branch-id', 'branch:main', 'current', 1)`, alice)

	push := store.Push()
	stream, err := push.StreamID(ctx, alice, "current", 999)
	if want := ingestion.StreamID("source", "repo", "checkout", "branch-id", "branch:main"); err != nil || stream != want {
		t.Fatalf("stream %q %v, want %q", stream, err, want)
	}
	for name, lookup := range map[string]func() (string, error){
		"expired":  func() (string, error) { return push.StreamID(ctx, alice, "current", 1_000) },
		"headless": func() (string, error) { return push.StreamID(ctx, alice, "headless", 0) },
		"foreign":  func() (string, error) { return push.StreamID(ctx, bob, "current", 0) },
		"missing":  func() (string, error) { return push.StreamID(ctx, alice, "missing", 0) },
	} {
		if stream, err := lookup(); !errors.Is(err, pushservice.ErrNotFound) {
			t.Fatalf("%s: %q %v", name, stream, err)
		}
	}
}
