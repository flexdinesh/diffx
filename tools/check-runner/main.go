// check-runner serializes full local checks across a repository's worktrees.
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
	report := reporter{status: os.Stderr, verbose: os.Getenv(verboseEnv) == "1"}
	return runChecks(ctx, root, args[0], report, runCommand, startDockerPostgres)
}

// verboseEnv streams every phase to the terminal instead of its log file.
const verboseEnv = "DIFFX_CHECK_VERBOSE"

// failureTail is how many trailing log lines a failed phase prints.
const failureTail = 100

// logDirectory holds one log per phase of the latest run, relative to the root.
var logDirectory = filepath.Join("dist", "check-logs")

// command runs one phase. Output goes to files, never pipes: a pipe would keep
// the phase running until descendants that inherited it exit.
type command func(ctx context.Context, directory string, env, args []string, stdout, stderr *os.File) error

// reporter decides what the caller sees: a status line per phase with the
// failing phase's log tail, or every phase's output streamed.
type reporter struct {
	status  io.Writer
	verbose bool
}

func runChecks(ctx context.Context, root, mode string, report reporter, execute command, database startDatabase) error {
	return withLock(ctx, root, func() (result error) {
		if err := os.RemoveAll(filepath.Join(root, logDirectory)); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(root, logDirectory), 0o755); err != nil {
			return err
		}
		// Bounds apply to Go subprocesses too, including fixture builds in tests.
		env := append(os.Environ(), "GOMAXPROCS=2", "GOFLAGS="+os.Getenv("GOFLAGS")+" -p=2", "pnpm_config_workspace_concurrency=1", "DIFFX_CHECK_RUNNER=1")
		// Go, conformance and browser suites share one server instead of each
		// test process starting a container. A preset server is used as is.
		if os.Getenv(testDatabaseEnv) == "" {
			common, err := commonDirectory(ctx, root)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "check-runner: starting shared test Postgres")
			url, stop, err := database(ctx, common)
			if err != nil {
				return err
			}
			defer func() { result = errors.Join(result, stop()) }()
			env = append(env, testDatabaseEnv+"="+url)
		}
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
		env = append(env, "DIFFX_FIXTURE_BINARY="+filepath.Join(root, "dist", "diffx"), "DIFFX_FIXTURE_SERVER_BINARY="+filepath.Join(root, "dist", "diffx-server"), "DIFFX_FIXTURE_WEB_DIR="+filepath.Join(root, "apps", "web", "dist"))
		for index, phase := range phases {
			log := filepath.Join(logDirectory, fmt.Sprintf("%02d-%s.log", index+1, strings.ReplaceAll(phase[len(phase)-1], ":", "-")))
			if err := report.phase(ctx, root, env, phase, log, execute); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(phase, " "), err)
			}
		}
		return nil
	})
}

func (r reporter) phase(ctx context.Context, root string, env, phase []string, log string, execute command) error {
	name := strings.Join(phase, " ")
	if r.verbose {
		fmt.Fprintln(r.status, "check-runner:", name)
		return execute(ctx, root, env, phase, os.Stdout, os.Stderr)
	}
	file, err := os.Create(filepath.Join(root, log))
	if err != nil {
		return err
	}
	start := time.Now()
	err = errors.Join(execute(ctx, root, env, phase, file, file), file.Close())
	elapsed := time.Since(start).Round(time.Second)
	if err == nil {
		fmt.Fprintf(r.status, "check-runner: ok %s (%s)\n", name, elapsed)
		return nil
	}
	tail, tailErr := lastLines(filepath.Join(root, log), failureTail)
	fmt.Fprintf(r.status, "check-runner: FAIL %s (%s), last %d log lines:\n%s", name, elapsed, failureTail, tail)
	fmt.Fprintln(r.status, "check-runner: log", log)
	fmt.Fprintln(r.status, "check-runner: rerun", name)
	return errors.Join(err, tailErr)
}

// lastLines returns up to n trailing lines of a file, newline-terminated.
func lastLines(path string, n int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	text := strings.Join(lines, "")
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, nil
}
