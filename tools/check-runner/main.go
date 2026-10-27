// check-runner serializes full local checks across a repository's worktrees.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/flexdinesh/diffx/internal/processlock"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := run(ctx, os.Args[1:])
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "check-runner:", err)
	if ctx.Err() != nil {
		os.Exit(130)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		os.Exit(exit.ExitCode())
	}
	os.Exit(1)
}

func commonDirectory(ctx context.Context, directory string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = directory
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Git common directory: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func withLock(ctx context.Context, directory string, action func() error) (result error) {
	common, err := commonDirectory(ctx, directory)
	if err != nil {
		return err
	}
	path := filepath.Join(common, "diffx-checks.lock")
	waiting := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		lock, err := processlock.TryAcquire(path)
		if err == nil {
			defer func() { result = errors.Join(result, lock.Close()) }()
			fmt.Fprintln(os.Stderr, "check-runner: acquired", path)
			return action()
		}
		if !errors.Is(err, processlock.ErrLocked) {
			return err
		}
		if !waiting {
			fmt.Fprintln(os.Stderr, "check-runner: waiting for", path)
			waiting = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "check" && args[0] != "check:push") {
		return errors.New("usage: check-runner check|check:push")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	return runChecks(ctx, root, args[0], runCommand)
}

func runChecks(ctx context.Context, root, mode string, command func(context.Context, string, []string, []string) error) error {
	return withLock(ctx, root, func() error {
		// Bounds apply to Go subprocesses too, including fixture builds in tests.
		env := append(os.Environ(), "GOMAXPROCS=2", "GOFLAGS="+os.Getenv("GOFLAGS")+" -p=2", "pnpm_config_workspace_concurrency=1", "DIFFX_CHECK_RUNNER=1")
		goTest := "test:go"
		if mode == "check:push" {
			goTest = "test:race"
		}
		phases := [][]string{
			{"mise", "run", "--force", "build"},
			{"mise", "run", "plugins:build"},
			{"mise", "run", "check:static"},
			{"mise", "run", "check:go:static"},
			{"mise", "run", "check:generated"},
			{"pnpm", "test:conformance"},
			{"mise", "run", "test:contracts:rules"},
			{"mise", "run", "docs:check"},
			{"mise", "run", "test:js"},
			{"mise", "run", "test:release"},
			{"mise", "run", goTest},
			{"mise", "run", "check:generated"},
		}
		env = append(env, "DIFFX_FIXTURE_BINARY="+filepath.Join(root, "dist", "diffx"), "DIFFX_FIXTURE_WEB_DIR="+filepath.Join(root, "apps", "web", "dist"))
		for _, phase := range phases {
			fmt.Fprintln(os.Stderr, "check-runner:", strings.Join(phase, " "))
			if err := command(ctx, root, env, phase); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(phase, " "), err)
			}
		}
		return nil
	})
}
