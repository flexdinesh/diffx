package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/jackc/pgx/v5"
)

var ErrSubmissionConflict = reviewdata.ErrSubmissionConflict

// Queue capacity bounds pending admission work across every replica.
const (
	maxActiveJobs     = 1024
	maxActiveJobBytes = 256 << 20
)

// recordSubmission is shared by direct commits and durable queue acceptance.
// It returns the arrival sequence and the context of an already published submission.
func recordSubmission(ctx context.Context, queries *pgdb.Queries, owner string, request ingestion.Request, hash string) (int64, string, error) {
	if err := queries.InsertIngestionRecord(ctx, pgdb.InsertIngestionRecordParams{OwnerID: owner, SourceID: request.Metadata.SourceID, SubmissionID: request.SubmissionID, PayloadHash: hash}); err != nil {
		return 0, "", err
	}
	record, err := queries.IngestionRecord(ctx, pgdb.IngestionRecordParams{OwnerID: owner, SourceID: request.Metadata.SourceID, SubmissionID: request.SubmissionID})
	if err != nil {
		return 0, "", err
	}
	if record.PayloadHash != hash {
		return record.Sequence, record.ContextID, ErrSubmissionConflict
	}
	return record.Sequence, record.ContextID, nil
}

// acceptJob admits within the caller's transaction, so other state can commit
// atomically with durable admission.
func acceptJob(ctx context.Context, queries *pgdb.Queries, owner string, request ingestion.Request) (ingestionqueue.Job, error) {
	if err := ingestion.Validate(request); err != nil {
		return ingestionqueue.Job{}, err
	}
	raw, hash, err := ingestion.Payload(request)
	if err != nil {
		return ingestionqueue.Job{}, err
	}
	if len(raw) > ingestion.MaxRequestBytes {
		return ingestionqueue.Job{}, ingestionqueue.ErrFull
	}
	sequence, contextID, err := recordSubmission(ctx, queries, owner, request, hash)
	if err != nil {
		return ingestionqueue.Job{}, err
	}
	existing, err := queries.JobBySequence(ctx, sequence)
	if err == nil {
		return jobFromRow(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ingestionqueue.Job{}, err
	}
	usage, err := queries.ActiveJobUsage(ctx)
	if err != nil {
		return ingestionqueue.Job{}, err
	}
	if usage.Jobs >= maxActiveJobs || usage.Bytes+int64(len(raw)) > maxActiveJobBytes {
		return ingestionqueue.Job{}, ingestionqueue.ErrFull
	}
	state, payload := "queued", raw
	if contextID != "" {
		state, payload = "succeeded", []byte{}
	}
	id := rand.Text()
	if err := queries.InsertJob(ctx, pgdb.InsertJobParams{ID: id, Sequence: sequence, Payload: payload, State: state, AvailableAt: time.Now(), ContextID: contextID}); err != nil {
		return ingestionqueue.Job{}, err
	}
	return ingestionqueue.Job{ID: id, SubmissionID: request.SubmissionID, State: state, ContextID: contextID}, nil
}

func jobFromRow(row pgdb.JobBySequenceRow) ingestionqueue.Job {
	return ingestionqueue.Job{ID: row.ID, SubmissionID: row.SubmissionID, State: row.State, Attempt: int(row.Attempt), ContextID: row.ContextID, Detail: row.Detail}
}

const pullRequestNotification = "pull_request"

// insertNotification records push intent in the caller's admission transaction.
func insertNotification(ctx context.Context, queries *pgdb.Queries, ownerID, jobID, kind string, notification githubsync.Notification, now time.Time) error {
	payload, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	return queries.InsertNotification(ctx, pgdb.InsertNotificationParams{ID: id, OwnerID: ownerID, JobID: jobID, Kind: kind, Payload: payload, AvailableAt: now})
}

// inTx runs work in one transaction with queries bound to it.
func (store *Store) inTx(ctx context.Context, work func(pgx.Tx, *pgdb.Queries) error) error {
	return pgx.BeginFunc(ctx, store.pool, func(tx pgx.Tx) error {
		return work(tx, store.queries.WithTx(tx))
	})
}
