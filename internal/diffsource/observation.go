package diffsource

import (
	"context"
	"io"
	"os/exec"
	"time"
)

type CommandResult struct {
	QueueWait    time.Duration
	Elapsed      time.Duration
	StdoutBytes  int64
	StderrBytes  int64
	ExitStatus   *int
	DeadlineKind string
	Err          error
}

// CommandObserver is supplied by composition, never by a global logger.
// Operations contain the Git subcommand only, never argv or process output.
type CommandObserver interface {
	BeginCommand(root, operation, scope string) func(CommandResult)
	Cleanup(string, error)
}

type commandObserverKey struct{}
type collectionDeadlineKey struct{}
type commandScopeKey struct{}

func commandScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, commandScopeKey{}, scope)
}

func WithCommandObserver(ctx context.Context, observer CommandObserver) context.Context {
	return context.WithValue(ctx, commandObserverKey{}, observer)
}

func WithCollectionDeadline(ctx, parent context.Context) context.Context {
	deadline, _ := ctx.Deadline()
	if previous, exists := parent.Deadline(); exists && deadline.Equal(previous) {
		return ctx
	}
	return context.WithValue(ctx, collectionDeadlineKey{}, deadline)
}

func beginCommand(ctx context.Context, root string, arguments []string) func(CommandResult) {
	if observer, ok := ctx.Value(commandObserverKey{}).(CommandObserver); ok {
		scope, _ := ctx.Value(commandScopeKey{}).(string)
		operation := "unknown"
		for _, argument := range arguments {
			switch argument {
			case "rev-parse", "rev-list", "patch-id", "status", "diff", "show", "log", "merge-base", "for-each-ref", "symbolic-ref", "config", "check-ref-format", "reflog", "ls-files", "ls-tree", "hash-object", "diff-tree", "cat-file":
				operation = argument
				return observer.BeginCommand(root, operation, scope)
			}
		}
		return observer.BeginCommand(root, operation, scope)
	}
	return func(CommandResult) {}
}

func observeCleanup(ctx context.Context, phase string, err error) {
	if err == nil {
		return
	}
	if observer, ok := ctx.Value(commandObserverKey{}).(CommandObserver); ok {
		observer.Cleanup(phase, err)
	}
}

func deadlineKind(parent, command context.Context) string {
	if command.Err() == nil {
		return "none"
	}
	if parent.Err() == context.Canceled {
		return "cancelled"
	}
	if parent.Err() == context.DeadlineExceeded {
		deadline, ok := parent.Value(collectionDeadlineKey{}).(time.Time)
		if current, exists := parent.Deadline(); ok && exists && current.Equal(deadline) {
			return "collection"
		}
		return "caller"
	}
	return "command"
}

type countedWriter struct {
	io.Writer
	bytes int64
}

func (writer *countedWriter) Write(value []byte) (int, error) {
	n, err := writer.Writer.Write(value)
	writer.bytes += int64(n)
	return n, err
}

// GitRuntime reads only build/runtime facts through the same bounded runner.
func GitRuntime(ctx context.Context) (path, version string) {
	path, _ = exec.LookPath("git")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	version, _ = runGit(ctx, "", 1024, "--version")
	return path, version
}
