package processlock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContextCancellationDoesNotWaitForOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.lock")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		lock, err := AcquireContext(ctx, path)
		if lock != nil {
			lock.Close()
		}
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lock result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled lock waited for owner")
	}
	if _, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		t.Fatal("cancelled waiter changed owner", err)
	}
}

func TestLockExcludesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if other, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("second acquire = %v; want ErrLocked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file removed: %v", err)
	}
	second, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
