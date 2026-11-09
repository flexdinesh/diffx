package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/flexdinesh/diffx/internal/processlock"
)

// admit acquires one of two user-level slots shared across worktrees and
// repositories. The slot index names this run's Docker resources, so the
// holder may remove anything a dead run left under the same slot.
func admit(ctx context.Context) (int, func(), error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return 0, nil, err
	}
	directory := filepath.Join(cache, "local-validation", "slots")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return 0, nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		for slot := range 2 {
			lock, err := processlock.TryAcquire(filepath.Join(directory, fmt.Sprintf("%d.lock", slot)))
			if err == nil {
				return slot, func() { _ = lock.Close() }, nil
			}
			if !errors.Is(err, processlock.ErrLocked) {
				return 0, nil, err
			}
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func pruneArtifacts(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var complete []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(directory, entry.Name(), "complete")); err == nil {
			complete = append(complete, entry.Name())
		}
	}
	sort.Strings(complete)
	for len(complete) > 20 {
		if err := os.RemoveAll(filepath.Join(directory, complete[0])); err != nil {
			return err
		}
		complete = complete[1:]
	}
	return nil
}
