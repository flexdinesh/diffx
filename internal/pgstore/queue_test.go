package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queueDatabase opens a store and a raw pool on one fresh database; the pool
// sets up rows owned by other packages and simulates crashes.
func queueDatabase(t *testing.T) (string, *pgstore.Store, *pgxpool.Pool) {
	t.Helper()
	url := pgtest.URL(t)
	store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	db, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return url, store, db
}

func execSQL(t *testing.T, db *pgxpool.Pool, statement string, arguments ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), statement, arguments...); err != nil {
		t.Fatal(err)
	}
}

func queueOwner(t *testing.T, db *pgxpool.Pool, id string) string {
	t.Helper()
	execSQL(t, db, `INSERT INTO diffx.users(id, name, created_at) VALUES ($1, $1, now())`, id)
	return id
}

func queueRequest(submission string) ingestion.Request {
	metadata := ingestion.Metadata{SourceID: "source", Hostname: "agent-host", RepositoryKey: "repo-key", RepositoryName: "my-repo", CheckoutKey: "checkout", Root: "/deleted/checkout", WorktreeName: "sandbox", Branch: "feature", CollectedAt: 123, Trigger: "agent-hook"}
	snapshot := func(mode review.DiffMode, revision string, files []review.ChangedFile) review.RepositoryDiff {
		return review.RepositoryDiff{Mode: mode, Revision: revision, Files: files, Source: "local", Root: metadata.Root, Branch: metadata.Branch}
	}
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission, Metadata: metadata,
		Scopes: []ingestion.Scope{
			{Snapshot: snapshot(review.DiffAll, "all-revision", []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "all-fingerprint"}}), Patches: map[string]review.FilePatch{"file": {Patch: "all-patch", Contents: &review.FileContents{Before: "old", After: "new"}}}},
			{Snapshot: snapshot(review.DiffStaged, "staged-revision", []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "staged-fingerprint"}}), Patches: map[string]review.FilePatch{"file": {Patch: "staged-patch"}}},
			{Snapshot: snapshot(review.DiffUnstaged, "unstaged-revision", []review.ChangedFile{}), Patches: map[string]review.FilePatch{}},
		},
	}
}

func TestQueueRestartReplayAndLeaseFencing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url, store, db := queueDatabase(t)
	owner := queueOwner(t, db, "queue")
	request := queueRequest("first")
	request.Metadata.ActivityAt = 98
	job, err := store.Queue().Accept(ctx, owner, request)
	if err != nil || job.State != "queued" || job.SubmissionID != "first" {
		t.Fatalf("accept: %+v %v", job, err)
	}
	original, err := store.Queue().Claim(ctx, time.Minute)
	if err != nil || original.ID != job.ID || original.OwnerID != owner || original.Attempt != 1 || original.State != "running" || original.Token == "" {
		t.Fatalf("claim: %+v %v", original, err)
	}
	if err := store.Queue().Renew(ctx, original, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after commit, before queue acknowledgement.
	execSQL(t, db, `UPDATE diffx.ingestion_jobs SET lease_until = '-infinity'`)
	if err := store.Queue().Renew(ctx, original, time.Minute); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
		t.Fatalf("expired renewal: %v", err)
	}
	store.Close()
	store, err = pgstore.Open(ctx, url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered, err := store.Queue().Claim(ctx, time.Minute)
	if err != nil || recovered.ID != job.ID || recovered.Attempt != 2 || recovered.Token == original.Token {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	_, want, _ := ingestion.Payload(request)
	if _, got, err := ingestion.Payload(recovered.Request); err != nil || got != want || recovered.Request.Metadata.ActivityAt != 98 {
		t.Fatalf("payload changed: %+v %v", recovered.Request, err)
	}
	if err := store.Queue().Complete(ctx, original, "context-1"); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
		t.Fatalf("old lease: %v", err)
	}
	if err := store.Queue().Complete(ctx, recovered, "context-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Queue().Complete(ctx, recovered, "context-1"); err != nil {
		t.Fatalf("repeated acknowledgement: %v", err)
	}
	if err := store.Queue().Complete(ctx, recovered, "context-2"); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
		t.Fatalf("acknowledged a different result: %v", err)
	}
	var payload []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM diffx.ingestion_jobs WHERE id = $1`, job.ID).Scan(&payload); err != nil || len(payload) != 0 {
		t.Fatalf("completed payload kept: %d %v", len(payload), err)
	}
	same, err := store.Queue().Accept(ctx, owner, request)
	if err != nil || same.ID != job.ID || same.State != "succeeded" || same.ContextID != "context-1" || same.Attempt != 2 {
		t.Fatalf("duplicate acceptance: %+v %v", same, err)
	}
	status, err := store.Queue().Get(ctx, owner, job.ID)
	if err != nil || status != same {
		t.Fatalf("status: %+v %v", status, err)
	}
	request.Metadata.Hostname = "different"
	if _, err := store.Queue().Accept(ctx, owner, request); !errors.Is(err, pgstore.ErrSubmissionConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	other := queueOwner(t, db, "other")
	if _, err := store.Queue().Get(ctx, other, job.ID); !errors.Is(err, pgstore.ErrNotFound) {
		t.Fatalf("ownership: %v", err)
	}
}

func TestQueueClaimsInAcceptanceOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	owner := queueOwner(t, db, "queue-order")
	var accepted []string
	for _, submission := range []string{"b", "a", "c"} {
		job, err := store.Queue().Accept(ctx, owner, queueRequest(submission))
		if err != nil {
			t.Fatal(err)
		}
		accepted = append(accepted, job.ID)
	}
	for _, id := range accepted {
		delivery, err := store.Queue().Claim(ctx, time.Minute)
		if err != nil || delivery.ID != id {
			t.Fatalf("claim %s: %+v %v", id, delivery, err)
		}
	}
	if _, err := store.Queue().Claim(ctx, time.Minute); !errors.Is(err, ingestionqueue.ErrEmpty) {
		t.Fatalf("held leases claimed: %v", err)
	}
}

func TestQueueBackoffAndTerminalFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	owner := queueOwner(t, db, "queue-failures")
	job, err := store.Queue().Accept(ctx, owner, queueRequest("job"))
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := store.Queue().Claim(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	next := time.UnixMilli(time.Now().Add(time.Hour).UnixMilli())
	if err := store.Queue().Retry(ctx, delivery, next, "temporary"); err != nil {
		t.Fatal(err)
	}
	var available time.Time
	if err := db.QueryRow(ctx, `SELECT available_at FROM diffx.ingestion_jobs WHERE id = $1`, job.ID).Scan(&available); err != nil || !available.Equal(next) {
		t.Fatalf("retry time %v, want %v: %v", available, next, err)
	}
	if status, err := store.Queue().Get(ctx, owner, job.ID); err != nil || status.State != "retrying" || status.Detail != "temporary" {
		t.Fatalf("retry status: %+v %v", status, err)
	}
	if err := store.Queue().Fail(ctx, delivery, "late"); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
		t.Fatalf("released lease transitioned: %v", err)
	}
	if _, err := store.Queue().Claim(ctx, time.Minute); !errors.Is(err, ingestionqueue.ErrEmpty) {
		t.Fatalf("backoff ignored: %v", err)
	}
	execSQL(t, db, `UPDATE diffx.ingestion_jobs SET available_at = '-infinity'`)
	delivery, err = store.Queue().Claim(ctx, time.Minute)
	if err != nil || delivery.Attempt != 2 {
		t.Fatalf("retry claim: %+v %v", delivery, err)
	}
	if err := store.Queue().Fail(ctx, delivery, "invalid"); err != nil {
		t.Fatal(err)
	}
	status, err := store.Queue().Get(ctx, owner, job.ID)
	if err != nil || status.State != "failed" || status.Detail != "invalid" {
		t.Fatalf("failure status: %+v %v", status, err)
	}
	if _, err := store.Queue().Claim(ctx, time.Minute); !errors.Is(err, ingestionqueue.ErrEmpty) {
		t.Fatalf("failed job retried: %v", err)
	}
}

func TestQueueFencesStaleTokens(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	owner := queueOwner(t, db, "queue-fence")
	job, err := store.Queue().Accept(ctx, owner, queueRequest("job"))
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := store.Queue().Claim(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stale := delivery
	stale.Token = "stale"
	for name, transition := range map[string]func() error{
		"renew":    func() error { return store.Queue().Renew(ctx, stale, time.Minute) },
		"complete": func() error { return store.Queue().Complete(ctx, stale, "context") },
		"retry":    func() error { return store.Queue().Retry(ctx, stale, time.Now(), "temporary") },
		"fail":     func() error { return store.Queue().Fail(ctx, stale, "invalid") },
	} {
		if err := transition(); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
			t.Fatalf("stale %s: %v", name, err)
		}
	}
	if status, err := store.Queue().Get(ctx, owner, job.ID); err != nil || status.State != "running" {
		t.Fatalf("stale token changed the job: %+v %v", status, err)
	}
	if err := store.Queue().Complete(ctx, delivery, "context"); err != nil {
		t.Fatal(err)
	}
}

func TestQueueConcurrentClaimsDeliverEachJobOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_, store, db := queueDatabase(t)
	owner := queueOwner(t, db, "queue-concurrent")
	const jobs, workers = 40, 16
	accepted := make(map[string]bool, jobs)
	for i := range jobs {
		job, err := store.Queue().Accept(ctx, owner, queueRequest(fmt.Sprintf("job-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		accepted[job.ID] = true
	}
	claimAll := func() []ingestionqueue.Delivery {
		var mutex sync.Mutex
		var claimed []ingestionqueue.Delivery
		var group sync.WaitGroup
		failures := make(chan error, workers)
		for range workers {
			group.Go(func() {
				for {
					delivery, err := store.Queue().Claim(ctx, time.Minute)
					if errors.Is(err, ingestionqueue.ErrEmpty) {
						return
					}
					if err != nil {
						failures <- err
						return
					}
					mutex.Lock()
					claimed = append(claimed, delivery)
					mutex.Unlock()
				}
			})
		}
		group.Wait()
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		return claimed
	}
	check := func(claimed []ingestionqueue.Delivery, attempt int) {
		t.Helper()
		seen := make(map[string]bool, len(claimed))
		for _, delivery := range claimed {
			if seen[delivery.ID] || !accepted[delivery.ID] || delivery.Attempt != attempt {
				t.Fatalf("delivery %+v claimed twice or unknown (attempt %d)", delivery.Job, attempt)
			}
			seen[delivery.ID] = true
		}
		if len(seen) != jobs {
			t.Fatalf("claimed %d of %d jobs", len(seen), jobs)
		}
	}
	first := claimAll()
	check(first, 1)
	// Every lease expires at once; each job is reclaimed by exactly one worker.
	execSQL(t, db, `UPDATE diffx.ingestion_jobs SET lease_until = '-infinity'`)
	second := claimAll()
	check(second, 2)
	for _, delivery := range first {
		if err := store.Queue().Complete(ctx, delivery, "context"); !errors.Is(err, ingestionqueue.ErrLeaseLost) {
			t.Fatalf("expired lease completed: %v", err)
		}
	}
	var group sync.WaitGroup
	failures := make(chan error, len(second))
	for _, delivery := range second {
		group.Go(func() { failures <- store.Queue().Complete(ctx, delivery, "context-"+delivery.ID) })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
}
