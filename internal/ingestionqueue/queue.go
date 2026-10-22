// Package ingestionqueue owns durable admission and worker contracts.
package ingestionqueue

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

var ErrEmpty = errors.New("no ingestion jobs available")
var ErrLeaseLost = errors.New("ingestion lease lost")
var ErrFull = errors.New("ingestion queue full; retry later")

type Job = ingestion.Job

type Delivery struct {
	Job
	OwnerID string
	Token   string
	Request ingestion.Request
}

// Queue guarantees durable, idempotent acceptance and fenced job transitions.
// Completed jobs retain their identity even when the observation is deleted.
// An external broker adapter must atomically persist scheduling intent (outbox).
type Queue interface {
	Accept(context.Context, string, ingestion.Request) (Job, error)
	Get(context.Context, string, string) (Job, error)
	Claim(context.Context, time.Duration) (Delivery, error)
	Renew(context.Context, Delivery, time.Duration) error
	Complete(context.Context, Delivery, string) error
	Retry(context.Context, Delivery, time.Time, string) error
	Fail(context.Context, Delivery, string) error
}

type Ingest func(context.Context, string, ingestion.Request) (string, error)

// storageBackoff spaces out claims after a queue storage failure.
const storageBackoff = time.Second

// Run uses bounded processing and renews leases without coupling ingestion to
// the queue implementation. A crash after commit is recovered by ingestion replay.
// Queue storage failures are logged and retried after a backoff instead of
// stopping the server; an unacknowledged lease expires and is claimed again.
func Run(ctx context.Context, queue Queue, ingest Ingest) error {
	for ctx.Err() == nil {
		delivery, err := queue.Claim(ctx, 30*time.Second)
		wait := 200 * time.Millisecond
		if err == nil {
			err = process(ctx, queue, delivery, ingest)
			if err == nil || errors.Is(err, ErrLeaseLost) {
				continue
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if !errors.Is(err, ErrEmpty) {
			log.Printf("ingestion queue: %v", err)
			wait = storageBackoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
	return nil
}

func process(ctx context.Context, queue Queue, delivery Delivery, ingest Ingest) error {
	work, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				if err := queue.Renew(work, delivery, 30*time.Second); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	id, err := ingest(work, delivery.OwnerID, delivery.Request)
	cancel()
	<-done
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		return queue.Complete(ctx, delivery, id)
	}
	var problem *review.RequestError
	if review.Permanent(err) && errors.As(err, &problem) {
		return queue.Fail(ctx, delivery, problem.Detail)
	}
	if delivery.Attempt >= 5 {
		return queue.Fail(ctx, delivery, "Ingestion failed after five attempts")
	}
	return queue.Retry(ctx, delivery, time.Now().Add(time.Duration(1<<delivery.Attempt)*time.Second), "Temporary ingestion failure")
}
