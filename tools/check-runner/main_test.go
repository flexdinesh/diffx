package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFullPipelineAdmission(t *testing.T) {
	for _, outcome := range []string{"completion", "failure", "cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			root := repository(t)
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			ownerCtx, cancelOwner := context.WithCancel(ctx)
			defer cancelOwner()
			atCompletion := make(chan struct{})
			release := make(chan struct{})
			ownerDone := make(chan error, 1)
			failure := errors.New("final check failed")
			go func() {
				generatedChecks := 0
				ownerDone <- runChecks(ownerCtx, root, "check:push", func(ctx context.Context, _ string, _, args []string) error {
					if args[len(args)-1] != "check:generated" {
						return nil
					}
					generatedChecks++
					if generatedChecks != 2 {
						return nil
					}
					close(atCompletion)
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-release:
						if outcome == "failure" {
							return failure
						}
						return nil
					}
				})
			}()
			select {
			case <-atCompletion:
			case <-ctx.Done():
				t.Fatal("owner did not reach final check")
			}
			firstBuild := make(chan struct{})
			waiterDone := make(chan error, 1)
			var commands []string
			go func() {
				waiterDone <- runChecks(ctx, root, "check", func(_ context.Context, _ string, _, args []string) error {
					if len(commands) == 0 {
						close(firstBuild)
					}
					commands = append(commands, strings.Join(args, " "))
					return nil
				})
			}()
			select {
			case <-firstBuild:
				t.Error("waiter's build started before owner completed final check")
			case <-time.After(150 * time.Millisecond):
			}
			if outcome == "cancellation" {
				cancelOwner()
			} else {
				close(release)
			}
			err := <-ownerDone
			switch outcome {
			case "completion":
				if err != nil {
					t.Fatal(err)
				}
			case "failure":
				if !errors.Is(err, failure) {
					t.Fatalf("failure lost: %v", err)
				}
			case "cancellation":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			}
			if err := <-waiterDone; err != nil {
				t.Fatal(err)
			}
			// A queued invocation runs its own complete pipeline, even after success.
			if len(commands) == 0 || commands[0] != "mise run --force build" {
				t.Fatalf("waiter did not start with its own build: %v", commands)
			}
			for _, required := range []string{"check:static", "check:go:static", "test:contracts:rules", "docs:check", "test:js", "test:release", "test:go", "check:generated"} {
				if !slices.Contains(commands, "mise run "+required) {
					t.Errorf("waiter skipped %s", required)
				}
			}
			if !slices.Contains(commands, "pnpm test:conformance") {
				t.Error("waiter skipped API conformance")
			}
		})
	}
}

func TestLockHelper(t *testing.T) {
	root := os.Getenv("DIFFX_CHECK_TEST_LOCK_ROOT")
	if root == "" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := withLock(ctx, root, func() error {
		if err := os.WriteFile(filepath.Join(root, "ready"), nil, 0o600); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnotherProcessQueuesUntilOwnerStops(t *testing.T) {
	root := repository(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runCommand(ctx, root, append(os.Environ(), "DIFFX_CHECK_TEST_LOCK_ROOT="+root), []string{os.Args[0], "-test.run=^TestLockHelper$"})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitCtx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	if err := withLock(waitCtx, root, func() error {
		t.Error("entered while another process owned lock")
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not stop")
	}
	ctx, stop = context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := withLock(ctx, root, func() error { return nil }); err != nil {
		t.Fatalf("owner left lock held: %v", err)
	}
}

func repository(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", directory)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return directory
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedCheckoutsShareLockAndWaiterCanCancel(t *testing.T) {
	root := repository(t)
	linked := t.TempDir()
	admin := filepath.Join(root, ".git", "worktrees", "linked")
	if err := os.MkdirAll(admin, 0o700); err != nil {
		t.Fatal(err)
	}
	// Minimal linked-checkout metadata; no branch or worktree commands needed.
	writeFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFile(t, filepath.Join(linked, ".git"), "gitdir: "+admin+"\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withLock(ctx, root, func() error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("owner did not acquire lock")
	}
	waitCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stop()
	err := withLock(waitCtx, linked, func() error {
		t.Error("linked checkout entered while owner held lock")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait cancellation: %v", err)
	}
	// Another repository must remain independent.
	if err := withLock(ctx, repository(t), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := withLock(ctx, linked, func() error { return nil }); err != nil {
		t.Fatalf("lock not reusable after cancelled waiter: %v", err)
	}
}

func TestFailureReleasesLock(t *testing.T) {
	root := repository(t)
	failure := errors.New("check failed")
	if err := withLock(context.Background(), root, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("failure not propagated: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := withLock(ctx, root, func() error { return nil }); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}
