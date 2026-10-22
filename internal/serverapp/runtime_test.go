package serverapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPruneFailureKeepsTaskRunning(t *testing.T) {
	var passes atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- pruneEvery(5*time.Millisecond, func(time.Time) error {
			if passes.Add(1) == 1 {
				return errors.New("database is locked")
			}
			return nil
		})(ctx)
	}()
	deadline := time.After(5 * time.Second)
	for passes.Load() < 2 {
		select {
		case err := <-result:
			t.Fatalf("prune task stopped after a failed pass: %v", err)
		case <-deadline:
			t.Fatal("prune task did not retry")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}
