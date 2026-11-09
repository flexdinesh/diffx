package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type scheduleRecorder struct {
	mu       sync.Mutex
	running  int
	peak     int
	built    []string
	executed []string
	finished map[string]error
}

func (recorder *scheduleRecorder) hooks(buildErr map[string]error, groupErr map[string]error) scheduleHooks {
	recorder.finished = make(map[string]error)
	return scheduleHooks{
		build: func(_ context.Context, target string) error {
			recorder.mu.Lock()
			recorder.built = append(recorder.built, target)
			recorder.mu.Unlock()
			return buildErr[target]
		},
		execute: func(ctx context.Context, group checkGroup) error {
			recorder.mu.Lock()
			recorder.running++
			recorder.peak = max(recorder.peak, recorder.running)
			recorder.executed = append(recorder.executed, group.Name)
			recorder.mu.Unlock()
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
			}
			recorder.mu.Lock()
			recorder.running--
			recorder.mu.Unlock()
			return groupErr[group.Name]
		},
		started: func(checkGroup) {},
		finished: func(group checkGroup, err error) {
			recorder.mu.Lock()
			recorder.finished[group.Name] = err
			recorder.mu.Unlock()
		},
	}
}

func TestScheduleBoundsConcurrencyAndContinuesAfterFailure(t *testing.T) {
	pending := groups(true)
	failure := errors.New("go tests failed")
	recorder := &scheduleRecorder{}
	results := schedule(t.Context(), pending, 3, recorder.hooks(nil, map[string]error{"go": failure}))
	if recorder.peak > 3 {
		t.Fatalf("peak concurrency %d exceeds 3", recorder.peak)
	}
	if recorder.peak < 2 {
		t.Fatalf("groups did not run in parallel (peak %d)", recorder.peak)
	}
	if len(recorder.executed) != len(pending) {
		t.Fatalf("executed %v, want every group despite the go failure", recorder.executed)
	}
	for _, group := range pending {
		want := error(nil)
		if group.Name == "go" {
			want = failure
		}
		if !errors.Is(results[group.Name], want) && results[group.Name] != want {
			t.Errorf("%s result = %v, want %v", group.Name, results[group.Name], want)
		}
		if _, ok := recorder.finished[group.Name]; !ok {
			t.Errorf("%s never reported completion", group.Name)
		}
	}
	// Each needed target is built once, ancestors first.
	if !slices.Equal(recorder.built, []string{"source", "built", "built-docs", "desktop", "desktop-docs", "site"}) {
		t.Fatalf("built targets %v", recorder.built)
	}
}

func TestScheduleFailsOnlyGroupsDependingOnFailedTarget(t *testing.T) {
	pending := groups(true)
	recorder := &scheduleRecorder{}
	results := schedule(t.Context(), pending, 3, recorder.hooks(map[string]error{"built": errors.New("vite failed")}, nil))
	for _, group := range pending {
		dependsOnBuilt := false
		for target := group.Target; target != ""; target = targetParents[target] {
			dependsOnBuilt = dependsOnBuilt || target == "built"
		}
		err := results[group.Name]
		if dependsOnBuilt != (err != nil) {
			t.Errorf("%s (target %q) result = %v", group.Name, group.Target, err)
		}
		if dependsOnBuilt && slices.Contains(recorder.executed, group.Name) {
			t.Errorf("%s executed without its image", group.Name)
		}
	}
	for _, target := range []string{"built-docs", "desktop", "desktop-docs"} {
		if slices.Contains(recorder.built, target) {
			t.Errorf("descendant target %s rebuilt the failed stage", target)
		}
		group := map[string]string{"built-docs": "contracts", "desktop": "desktop", "desktop-docs": "desktop-package"}[target]
		if err := results[group]; err == nil || !strings.Contains(err.Error(), "image target "+target+" skipped") {
			t.Errorf("%s result %v does not report skipped target %s", group, err, target)
		}
	}
}

func TestScheduleStopsStartingGroupsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	recorder := &scheduleRecorder{}
	results := schedule(ctx, groups(true), 3, recorder.hooks(nil, nil))
	if len(recorder.executed) != 0 || len(recorder.built) != 0 {
		t.Fatalf("cancelled run executed %v and built %v", recorder.executed, recorder.built)
	}
	for name, err := range results {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s result = %v, want cancellation", name, err)
		}
	}
}
