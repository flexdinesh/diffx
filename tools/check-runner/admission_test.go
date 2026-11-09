package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdmissionBoundsConcurrentRunsAndCancellation(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	firstSlot, first, err := admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	secondSlot, second, err := admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	if firstSlot == secondSlot {
		t.Fatalf("concurrent runs share slot %d; their Docker resources would collide", firstSlot)
	}
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, release, err := admit(waiting); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("third invocation admitted: %v", err)
	}
	first()
	available, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	thirdSlot, third, err := admit(available)
	if err != nil {
		t.Fatal("released slot was not reusable:", err)
	}
	if thirdSlot != firstSlot {
		t.Fatalf("reused slot %d, want released slot %d", thirdSlot, firstSlot)
	}
	third()
}

func TestAdmissionDoesNotAcquireForCancelledInvocation(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, release, err := admit(ctx)
	if release != nil {
		release()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled invocation acquired slot: %v", err)
	}
}

func TestArtifactRetentionPreservesActiveAndLatestTwenty(t *testing.T) {
	root := t.TempDir()
	for index := range 25 {
		name := fmt.Sprintf("20261101T%06d-run", index)
		directory := filepath.Join(root, name)
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "complete"), []byte("done"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(root, "0000-active")
	if err := os.Mkdir(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pruneArtifacts(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 21 {
		t.Fatalf("retained %d directories: %v", len(entries), err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("active run removed:", err)
	}
	for index := range 25 {
		_, err := os.Stat(filepath.Join(root, fmt.Sprintf("20261101T%06d-run", index)))
		if index < 5 && !os.IsNotExist(err) {
			t.Errorf("old completed run %d retained", index)
		}
		if index >= 5 && err != nil {
			t.Errorf("recent run %d removed: %v", index, err)
		}
	}
}
