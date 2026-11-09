package main

import (
	"context"
	"fmt"
	"sync"
)

// skippedTarget names the failed ancestor without repeating its whole chain;
// Unwrap keeps the cause (such as cancellation) inspectable.
type skippedTarget struct {
	target, ancestor string
	cause            error
}

func (err skippedTarget) Error() string {
	return fmt.Sprintf("image target %s skipped: %s failed", err.target, err.ancestor)
}

func (err skippedTarget) Unwrap() error { return err.cause }

type scheduleHooks struct {
	// build prepares one image target; it is called sequentially in targetOrder.
	build func(context.Context, string) error
	// execute runs one group after its image target, if any, is ready.
	execute func(context.Context, checkGroup) error
	// started and finished are called from group goroutines.
	started  func(checkGroup)
	finished func(checkGroup, error)
}

// schedule runs groups with at most limit executing at once. A failed group
// never cancels others; a failed image target fails only the groups that need
// it or a descendant target. It returns each group's outcome.
func schedule(ctx context.Context, pending []checkGroup, limit int, hooks scheduleHooks) map[string]error {
	needed := make(map[string]bool)
	for _, group := range pending {
		if group.Target != "" {
			needed[group.Target] = true
		}
	}
	ready := make(map[string]chan struct{})
	buildErrs := make(map[string]error)
	for target := range needed {
		ready[target] = make(chan struct{})
	}
	var buildMu sync.Mutex
	go func() {
		failed := make(map[string]error)
		for _, target := range targetOrder {
			if !needed[target] {
				continue
			}
			var err error
			for parent := targetParents[target]; parent != ""; parent = targetParents[parent] {
				if failed[parent] != nil {
					err = skippedTarget{target, parent, failed[parent]}
					break
				}
			}
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				err = hooks.build(ctx, target)
				if err != nil {
					err = fmt.Errorf("image target %s: %w", target, err)
				}
			}
			failed[target] = err
			buildMu.Lock()
			buildErrs[target] = err
			buildMu.Unlock()
			close(ready[target])
		}
	}()

	slots := make(chan struct{}, limit)
	results := make(map[string]error)
	var resultsMu sync.Mutex
	var wait sync.WaitGroup
	for _, group := range pending {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := runScheduled(ctx, group, ready, buildErrs, &buildMu, slots, hooks)
			resultsMu.Lock()
			results[group.Name] = err
			resultsMu.Unlock()
			hooks.finished(group, err)
		}()
	}
	wait.Wait()
	return results
}

func runScheduled(ctx context.Context, group checkGroup, ready map[string]chan struct{}, buildErrs map[string]error, buildMu *sync.Mutex, slots chan struct{}, hooks scheduleHooks) error {
	if group.Target != "" {
		select {
		case <-ready[group.Target]:
		case <-ctx.Done():
			return ctx.Err()
		}
		buildMu.Lock()
		err := buildErrs[group.Target]
		buildMu.Unlock()
		if err != nil {
			return err
		}
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slots }()
	// A free slot and cancellation can be ready together; never start late.
	if err := ctx.Err(); err != nil {
		return err
	}
	hooks.started(group)
	return hooks.execute(ctx, group)
}
