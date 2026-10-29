package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

// Queue is shared by every replica's worker: claims skip rows another worker
// holds, and each transition is fenced by its lease token in one statement.
type Queue struct{ store *Store }

func (store *Store) Queue() *Queue { return &Queue{store: store} }

func (queue *Queue) Accept(ctx context.Context, owner string, request ingestion.Request) (ingestionqueue.Job, error) {
	var job ingestionqueue.Job
	err := queue.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		var err error
		job, err = acceptJob(ctx, queries, owner, request)
		return err
	})
	if err != nil {
		return ingestionqueue.Job{}, err
	}
	return job, nil
}

func (queue *Queue) Get(ctx context.Context, owner, id string) (ingestionqueue.Job, error) {
	row, err := queue.store.queries.OwnedJob(ctx, pgdb.OwnedJobParams{OwnerID: owner, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ingestionqueue.Job{}, ErrNotFound
	}
	if err != nil {
		return ingestionqueue.Job{}, err
	}
	return ingestionqueue.Job{ID: row.ID, SubmissionID: row.SubmissionID, State: row.State, Attempt: int(row.Attempt), ContextID: row.ContextID, Detail: row.Detail}, nil
}

// Claim decodes within the claiming transaction, so an undecodable payload
// leaves the job unclaimed.
func (queue *Queue) Claim(ctx context.Context, duration time.Duration) (ingestionqueue.Delivery, error) {
	var delivery ingestionqueue.Delivery
	err := queue.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		now := time.Now()
		token := rand.Text()
		row, err := queries.ClaimJob(ctx, pgdb.ClaimJobParams{Now: now, LeaseToken: token, LeaseUntil: now.Add(duration)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ingestionqueue.ErrEmpty
		}
		if err != nil {
			return err
		}
		delivery.ID, delivery.OwnerID, delivery.SubmissionID = row.ID, row.OwnerID, row.SubmissionID
		delivery.Token, delivery.State, delivery.Attempt = token, "running", int(row.Attempt)
		return json.Unmarshal(row.Payload, &delivery.Request)
	})
	if err != nil {
		return ingestionqueue.Delivery{}, err
	}
	return delivery, nil
}

// jobFenced reports ErrLeaseLost unless the update matched the delivery's lease.
func jobFenced(count int64, err error) error {
	if err == nil && count != 1 {
		return ingestionqueue.ErrLeaseLost
	}
	return err
}

func (queue *Queue) Renew(ctx context.Context, delivery ingestionqueue.Delivery, duration time.Duration) error {
	now := time.Now()
	return jobFenced(queue.store.queries.RenewJob(ctx, pgdb.RenewJobParams{ID: delivery.ID, LeaseToken: delivery.Token, Now: now, LeaseUntil: now.Add(duration)}))
}

// Complete succeeds again when an earlier acknowledgement of the same result committed.
func (queue *Queue) Complete(ctx context.Context, delivery ingestionqueue.Delivery, id string) error {
	err := jobFenced(queue.store.queries.CompleteJob(ctx, pgdb.CompleteJobParams{ID: delivery.ID, LeaseToken: delivery.Token, Now: time.Now(), ContextID: id}))
	if errors.Is(err, ingestionqueue.ErrLeaseLost) {
		job, readErr := queue.Get(ctx, delivery.OwnerID, delivery.ID)
		if readErr == nil && job.State == "succeeded" && job.ContextID == id {
			return nil
		}
	}
	return err
}

func (queue *Queue) Retry(ctx context.Context, delivery ingestionqueue.Delivery, next time.Time, detail string) error {
	return jobFenced(queue.store.queries.RetryJob(ctx, pgdb.RetryJobParams{ID: delivery.ID, LeaseToken: delivery.Token, Now: time.Now(), AvailableAt: next, Detail: detail}))
}

func (queue *Queue) Fail(ctx context.Context, delivery ingestionqueue.Delivery, detail string) error {
	return jobFenced(queue.store.queries.FailJob(ctx, pgdb.FailJobParams{ID: delivery.ID, LeaseToken: delivery.Token, Now: time.Now(), Detail: detail}))
}

var _ ingestionqueue.Queue = (*Queue)(nil)
