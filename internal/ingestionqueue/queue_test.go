package ingestionqueue

import (
	"context"
	"errors"
	"fmt"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"testing"
	"time"
)

type recordingQueue struct {
	action, contextID string
	at                time.Time
	completeError     error
}

func (*recordingQueue) Accept(context.Context, string, ingestion.Request) (Job, error) {
	panic("unexpected admission")
}
func (*recordingQueue) Get(context.Context, string, string) (Job, error) { panic("unexpected status") }
func (*recordingQueue) Claim(context.Context, time.Duration) (Delivery, error) {
	return Delivery{}, ErrEmpty
}
func (*recordingQueue) Renew(context.Context, Delivery, time.Duration) error { return nil }
func (q *recordingQueue) Complete(_ context.Context, _ Delivery, id string) error {
	q.action = "complete"
	q.contextID = id
	return q.completeError
}
func (q *recordingQueue) Retry(_ context.Context, _ Delivery, at time.Time, _ string) error {
	q.action = "retry"
	q.at = at
	return nil
}
func (q *recordingQueue) Fail(context.Context, Delivery, string) error { q.action = "fail"; return nil }
func TestWorkerOutcomeContract(t *testing.T) {
	for _, c := range []struct {
		name    string
		attempt int
		failure error
		action  string
	}{
		{"committed", 1, nil, "complete"},
		{"transient", 1, errors.New("storage unavailable"), "retry"},
		{"permanent", 1, review.Error(400, "invalid observation"), "fail"},
		{"wrapped permanent", 1, fmt.Errorf("ingest: %w", review.Error(403, "Account unavailable")), "fail"},
		{"throttled", 1, review.Error(429, "Too many requests"), "retry"},
		{"timed out", 1, review.Error(408, "Request timeout"), "retry"},
		{"server failure", 1, review.Error(503, "source_unavailable: busy"), "retry"},
		{"exhausted", 5, errors.New("storage unavailable"), "fail"},
	} {
		t.Run(c.name, func(t *testing.T) {
			q := &recordingQueue{}
			before := time.Now()
			err := process(t.Context(), q, Delivery{OwnerID: "owner", Job: Job{ID: "job", Attempt: c.attempt}}, func(ctx context.Context, owner string, _ ingestion.Request) (string, error) {
				if owner != "owner" {
					t.Fatalf("wrong owner %s", owner)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("processing has no deadline")
				}
				return "context", c.failure
			})
			if err != nil || q.action != c.action {
				t.Fatalf("action=%s error=%v", q.action, err)
			}
			if c.action == "complete" && q.contextID != "context" {
				t.Fatal("lost committed context")
			}
			if c.action == "retry" && !q.at.After(before) {
				t.Fatal("retry not delayed")
			}
		})
	}
}
func TestWorkerCancellationAndLeaseFencing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	q := &recordingQueue{}
	err := process(ctx, q, Delivery{}, func(context.Context, string, ingestion.Request) (string, error) { cancel(); return "committed", nil })
	if err != nil || q.action != "" {
		t.Fatalf("shutdown changed job: %s %v", q.action, err)
	}
	q = &recordingQueue{completeError: ErrLeaseLost}
	err = process(t.Context(), q, Delivery{}, func(context.Context, string, ingestion.Request) (string, error) { return "committed", nil })
	if !errors.Is(err, ErrLeaseLost) || q.action != "complete" {
		t.Fatalf("lost lease retried: %s %v", q.action, err)
	}
}

// flakyQueue fails its first claim, then offers one job and stays empty.
type flakyQueue struct {
	recordingQueue
	claims    int
	completed chan struct{}
}

func (q *flakyQueue) Claim(context.Context, time.Duration) (Delivery, error) {
	q.claims++
	switch q.claims {
	case 1:
		return Delivery{}, errors.New("database is locked")
	case 2:
		return Delivery{OwnerID: "owner", Job: Job{ID: "job", Attempt: 1}}, nil
	}
	return Delivery{}, ErrEmpty
}

func (q *flakyQueue) Complete(context.Context, Delivery, string) error {
	close(q.completed)
	return nil
}

func TestWorkerSurvivesQueueStorageFailure(t *testing.T) {
	q := &flakyQueue{completed: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, q, func(context.Context, string, ingestion.Request) (string, error) { return "context", nil })
	}()
	select {
	case <-q.completed:
	case err := <-result:
		t.Fatalf("worker stopped after a storage failure: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not process the next job")
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

// wakingQueue is empty until ready, then offers one job.
type wakingQueue struct {
	recordingQueue
	claimed   chan struct{}
	ready     chan struct{}
	completed chan struct{}
	taken     bool
}

func (q *wakingQueue) Claim(context.Context, time.Duration) (Delivery, error) {
	select {
	case <-q.ready:
		if !q.taken {
			q.taken = true
			return Delivery{OwnerID: "owner", Job: Job{ID: "job", Attempt: 1}}, nil
		}
		return Delivery{}, ErrEmpty
	default:
		q.claimed <- struct{}{}
		return Delivery{}, ErrEmpty
	}
}

func (q *wakingQueue) Complete(context.Context, Delivery, string) error {
	close(q.completed)
	return nil
}

func TestWakeClaimsWithoutWaitingForPoll(t *testing.T) {
	q := &wakingQueue{claimed: make(chan struct{}, 1), ready: make(chan struct{}), completed: make(chan struct{})}
	wake := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, q, func(context.Context, string, ingestion.Request) (string, error) { return "context", nil },
			Wake(wake), func(worker *worker) { worker.poll = time.Hour })
	}()
	select {
	case <-q.claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never claimed")
	}
	close(q.ready)
	wake <- struct{}{}
	select {
	case <-q.completed:
	case <-time.After(5 * time.Second):
		t.Fatal("wake did not trigger a claim")
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}
