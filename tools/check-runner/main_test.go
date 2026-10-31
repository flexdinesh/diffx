package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
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
				ownerDone <- runChecks(ownerCtx, root, "check:push", quiet, func(ctx context.Context, _ string, _, args []string, _, _ *os.File) error {
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
				}, noDatabase)
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
				waiterDone <- runChecks(ctx, root, "check", quiet, func(_ context.Context, _ string, _, args []string, _, _ *os.File) error {
					if len(commands) == 0 {
						close(firstBuild)
					}
					commands = append(commands, strings.Join(args, " "))
					return nil
				}, noDatabase)
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

// quiet reports like a default run, discarding status lines.
var quiet = reporter{status: io.Discard}

func noDatabase(context.Context, string) (string, func() error, error) {
	return "postgres://unused", func() error { return nil }, nil
}

// lastEnv returns the value a child process sees: the last duplicate wins.
func lastEnv(env []string, key string) string {
	value := ""
	for _, entry := range env {
		if name, current, ok := strings.Cut(entry, "="); ok && name == key {
			value = current
		}
	}
	return value
}

func TestSharedDatabaseLifecycle(t *testing.T) {
	for _, outcome := range []string{"completion", "failure", "cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv(testDatabaseEnv, "")
			root := repository(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			failure := errors.New("check failed")
			var starts, stops int
			var seen []string
			err := runChecks(ctx, root, "check", quiet, func(ctx context.Context, _ string, env, _ []string, _, _ *os.File) error {
				seen = append(seen, lastEnv(env, testDatabaseEnv))
				switch outcome {
				case "failure":
					return failure
				case "cancellation":
					cancel()
					return ctx.Err()
				}
				return nil
			}, func(_ context.Context, common string) (string, func() error, error) {
				starts++
				if want, err := commonDirectory(context.Background(), root); err != nil || common != want {
					t.Errorf("owner %q, want %q (%v)", common, want, err)
				}
				return "postgres://shared", func() error {
					stops++
					// The server is removed before another run can be admitted.
					lockCtx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
					defer stop()
					if err := withLock(lockCtx, root, func() error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("lock released before database stopped: %v", err)
					}
					return nil
				}, nil
			})
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
			if starts != 1 || stops != 1 {
				t.Fatalf("starts %d, stops %d; want one each", starts, stops)
			}
			if len(seen) == 0 {
				t.Fatal("no step ran")
			}
			for _, url := range seen {
				if url != "postgres://shared" {
					t.Fatalf("step saw %s=%q", testDatabaseEnv, url)
				}
			}
		})
	}
}

func TestPresetDatabaseIsUsed(t *testing.T) {
	t.Setenv(testDatabaseEnv, "postgres://preset")
	steps := 0
	err := runChecks(context.Background(), repository(t), "check", quiet, func(_ context.Context, _ string, env, _ []string, _, _ *os.File) error {
		steps++
		if url := lastEnv(env, testDatabaseEnv); url != "postgres://preset" {
			t.Errorf("step saw %q", url)
		}
		return nil
	}, func(context.Context, string) (string, func() error, error) {
		t.Error("started a database despite a preset URL")
		return "", nil, errors.New("unexpected start")
	})
	if err != nil {
		t.Fatal(err)
	}
	if steps == 0 {
		t.Fatal("no step ran")
	}
}

func TestDatabaseStartFailureStopsBeforeSteps(t *testing.T) {
	t.Setenv(testDatabaseEnv, "")
	unavailable := errors.New("docker unavailable")
	err := runChecks(context.Background(), repository(t), "check", quiet, func(context.Context, string, []string, []string, *os.File, *os.File) error {
		t.Error("step ran without a database")
		return nil
	}, func(context.Context, string) (string, func() error, error) {
		return "", nil, unavailable
	})
	if !errors.Is(err, unavailable) {
		t.Fatalf("start failure lost: %v", err)
	}
}

func TestPostgresImageMatchesTestSupport(t *testing.T) {
	if postgresImage != pgtest.Image {
		t.Fatalf("image %s, want pgtest.Image %s", postgresImage, pgtest.Image)
	}
	if testDatabaseEnv != pgtest.EnvURL {
		t.Fatalf("env %s, want pgtest.EnvURL %s", testDatabaseEnv, pgtest.EnvURL)
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
		done <- runCommand(ctx, root, append(os.Environ(), "DIFFX_CHECK_TEST_LOCK_ROOT="+root), []string{os.Args[0], "-test.run=^TestLockHelper$"}, os.Stdout, os.Stderr)
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

func TestQuietRunLogsEachPhase(t *testing.T) {
	root := repository(t)
	stale := filepath.Join(root, logDirectory, "stale.log")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, stale, "previous run\n")
	var status strings.Builder
	phases := 0
	err := runChecks(context.Background(), root, "check", reporter{status: &status}, func(_ context.Context, _ string, _, args []string, stdout, _ *os.File) error {
		phases++
		_, err := fmt.Fprintln(stdout, "output of", args[len(args)-1])
		return err
	}, noDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.String(), "output of") {
		t.Errorf("phase output reached the terminal:\n%s", status.String())
	}
	if got := strings.Count(status.String(), "check-runner: ok "); got != phases {
		t.Errorf("%d status lines for %d phases:\n%s", got, phases, status.String())
	}
	if !strings.Contains(status.String(), "check-runner: ok mise run test:go (") {
		t.Errorf("no status line for test:go:\n%s", status.String())
	}
	log, err := os.ReadFile(filepath.Join(root, logDirectory, "11-test-go.log"))
	if err != nil || string(log) != "output of test:go\n" {
		t.Errorf("test:go log %q (%v)", log, err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale log kept: %v", err)
	}
}

func TestFailedPhasePrintsLogTailAndRerun(t *testing.T) {
	root := repository(t)
	failure := errors.New("tests failed")
	var status strings.Builder
	var commands []string
	err := runChecks(context.Background(), root, "check", reporter{status: &status}, func(_ context.Context, _ string, _, args []string, stdout, _ *os.File) error {
		commands = append(commands, args[len(args)-1])
		if args[len(args)-1] != "test:go" {
			return nil
		}
		for line := 1; line <= failureTail+50; line++ {
			if _, err := fmt.Fprintf(stdout, "line %d\n", line); err != nil {
				return err
			}
		}
		return failure
	}, noDatabase)
	if !errors.Is(err, failure) {
		t.Fatalf("failure lost: %v", err)
	}
	if commands[len(commands)-1] != "test:go" {
		t.Errorf("phases ran after the failure: %v", commands)
	}
	report := status.String()
	tail := report[strings.Index(report, "FAIL mise run test:go"):]
	for line := 51; line <= failureTail+50; line++ {
		if !strings.Contains(tail, fmt.Sprintf("line %d\n", line)) {
			t.Fatalf("tail misses line %d:\n%s", line, report)
		}
	}
	if strings.Contains(tail, "line 50\n") {
		t.Errorf("tail exceeds %d lines:\n%s", failureTail, report)
	}
	for _, want := range []string{
		"check-runner: log " + filepath.Join(logDirectory, "11-test-go.log") + "\n",
		"check-runner: rerun mise run test:go\n",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report misses %q:\n%s", want, report)
		}
	}
}

func TestVerboseRunStreamsPhases(t *testing.T) {
	root := repository(t)
	var status strings.Builder
	err := runChecks(context.Background(), root, "check", reporter{status: &status, verbose: true}, func(_ context.Context, _ string, _, _ []string, stdout, stderr *os.File) error {
		if stdout != os.Stdout || stderr != os.Stderr {
			t.Error("verbose phase output does not reach the terminal")
		}
		return nil
	}, noDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.String(), "check-runner: mise run test:go\n") {
		t.Errorf("no phase header:\n%s", status.String())
	}
	entries, err := os.ReadDir(filepath.Join(root, logDirectory))
	if err != nil || len(entries) != 0 {
		t.Errorf("verbose run wrote logs: %v (%v)", entries, err)
	}
}

func TestLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	for _, test := range []struct{ content, want string }{
		{"", ""},
		{"a\nb\nc\n", "b\nc\n"},
		{"a\nb\nc", "b\nc\n"},
		{"a\n", "a\n"},
	} {
		writeFile(t, path, test.content)
		if got, err := lastLines(path, 2); err != nil || got != test.want {
			t.Errorf("lastLines(%q) = %q (%v), want %q", test.content, got, err, test.want)
		}
	}
}
