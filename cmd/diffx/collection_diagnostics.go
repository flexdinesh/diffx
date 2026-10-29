package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/diagnostics"
	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/hooks"
	"github.com/flexdinesh/diffx/internal/review"
	buildversion "github.com/flexdinesh/diffx/internal/version"
)

// Every factory invocation gets an independent report. The factory can be
// shared by discovery and concurrent checkout workers, but sessions cannot.
func withCollectionDiagnostics(ctx context.Context, values options, stderr io.Writer) context.Context {
	var metadataOnce sync.Once
	var metadata diagnostics.Metadata
	var output sync.Mutex
	return collector.WithObservationFactory(ctx, func(ctx context.Context, path string) (context.Context, func(error) error) {
		metadataOnce.Do(func() {
			gitPath, gitVersion := diffsource.GitRuntime(ctx)
			operation := "collection"
			if values.trigger == "agent-hook" {
				operation = "hook"
			}
			if values.progress != nil {
				operation = "sync"
			}
			build := buildversion.Build()
			metadata = diagnostics.Metadata{AppVersion: build.Version, BuildCommit: build.Commit, BuildDate: build.Date, BuildModified: build.Modified, Platform: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version(), GitPath: gitPath, GitVersion: strings.TrimSpace(gitVersion), Operation: operation}
		})
		session := &collectionDiagnostic{recorder: diagnostics.New(metadata), path: path}
		session.progress = func(stage string) {
			if values.debug {
				output.Lock()
				fmt.Fprintf(stderr, "diffx: collect · %s\n", stage)
				output.Unlock()
			}
		}
		ctx = collector.WithObserver(ctx, session)
		ctx = diffsource.WithCommandObserver(ctx, session)
		return ctx, func(err error) error {
			unchanged := errors.Is(err, collector.ErrUnchanged)
			outcome := diagnostics.Outcome{Status: "success"}
			if err != nil && !unchanged {
				outcome.Status, outcome.Primary = "failure", diagnosticFailure(err)
				outcome.Cancelled = errors.Is(err, context.Canceled)
			}
			session.mu.Lock()
			outcome.Cleanup = append(outcome.Cleanup, session.cleanup...)
			stage := session.failedStage
			session.mu.Unlock()
			if stage == "" {
				stage = "collection"
			}
			report := session.recorder.Finish(outcome)
			if (err == nil || unchanged) && !values.debug {
				return err
			}
			directory, saveErr := config.StateDir()
			if saveErr == nil {
				saveErr = diagnostics.Save(filepath.Join(directory, "diagnostics"), report)
			}
			output.Lock()
			defer output.Unlock()
			if saveErr != nil {
				fmt.Fprintf(stderr, "diffx: could not save collection diagnostics: %v\n", saveErr)
				return err
			}
			fmt.Fprintf(stderr, "diffx: diagnostics %s; export: diffx diagnostics export %s --output failure.json\n", report.ID, report.ID)
			if err == nil || unchanged {
				return err
			}
			code := "collection_failed"
			if errors.Is(err, context.DeadlineExceeded) {
				code = "collection_timeout"
			}
			if errors.Is(err, context.Canceled) {
				code = "collection_cancelled"
			}
			recordCollectorActivity(hooksDiagnosticActivity(report.ID, code))
			return &review.DiagnosticError{ID: report.ID, Code: code, Stage: stage, Cause: err}
		}
	})
}

type collectionDiagnostic struct {
	mu          sync.Mutex
	recorder    *diagnostics.Recorder
	path        string
	current     diagnostics.StageInfo
	failedStage string
	cleanup     []diagnostics.Failure
	progress    func(string)
}

func (session *collectionDiagnostic) BeginStage(event collector.Observation) func(error) {
	info := diagnostics.StageInfo{Name: event.Stage, Scope: string(event.Scope), Attempt: event.Attempt, Repository: session.path}
	session.mu.Lock()
	previous := session.current
	if info.Attempt == 0 {
		info.Attempt = previous.Attempt
	}
	session.current = info
	session.mu.Unlock()
	if session.progress != nil {
		session.progress(stageLabel(info))
	}
	finish := session.recorder.BeginStage(info)
	return func(err error) {
		var failure *diagnostics.Failure
		if err != nil && !errors.Is(err, collector.ErrUnchanged) {
			failure = diagnosticFailure(err)
		}
		finish(failure)
		session.mu.Lock()
		if failure != nil && session.failedStage == "" {
			session.failedStage = stageLabel(info)
		}
		session.current = previous
		session.mu.Unlock()
	}
}

func (session *collectionDiagnostic) Workload(event collector.Observation) {
	delta := diagnostics.Workload{Files: int64(event.Files), Commits: int64(event.Commits)}
	if event.Stage == "checkout" {
		delta.Checkouts = 1
	}
	if event.Attempt > 0 {
		delta.Attempts = 1
	}
	if event.Scope != "" {
		delta.Scopes = 1
	}
	session.recorder.AddWorkload(delta)
}

func (session *collectionDiagnostic) BeginCommand(root, operation, scope string) func(diffsource.CommandResult) {
	session.mu.Lock()
	info := session.current
	session.mu.Unlock()
	if info.Name == "" {
		info.Name = "discovery"
	}
	info.Repository = root
	if scope != "" {
		info.Scope = scope
	}
	finish := session.recorder.BeginCommand(diagnostics.CommandInfo{StageInfo: info, Operation: operation})
	return func(result diffsource.CommandResult) {
		failure := diagnosticFailure(result.Err)
		if failure != nil && failure.Kind == "internal" {
			failure.Kind, failure.Detail = "git", "command-failed"
		}
		finish(diagnostics.CommandResult{QueueWait: result.QueueWait, Elapsed: result.Elapsed, StdoutBytes: result.StdoutBytes, StderrBytes: result.StderrBytes, ExitStatus: result.ExitStatus, DeadlineKind: result.DeadlineKind, Failure: failure})
	}
}

func (session *collectionDiagnostic) Cleanup(phase string, err error) {
	if err == nil {
		return
	}
	failure := diagnosticFailure(err)
	failure.Phase = phase
	session.mu.Lock()
	if len(session.cleanup) < 20 {
		session.cleanup = append(session.cleanup, *failure)
	}
	session.mu.Unlock()
}

func stageLabel(info diagnostics.StageInfo) string {
	if info.Scope != "" {
		return info.Name + " (" + info.Scope + ")"
	}
	return info.Name
}

func diagnosticFailure(err error) *diagnostics.Failure {
	if err == nil {
		return nil
	}
	failure := &diagnostics.Failure{Kind: "internal", Detail: "unavailable"}
	for _, code := range []struct {
		value error
		name  string
	}{{syscall.EPERM, "EPERM"}, {syscall.EACCES, "EACCES"}, {syscall.ESRCH, "ESRCH"}, {syscall.ENOENT, "ENOENT"}, {syscall.EIO, "EIO"}} {
		if errors.Is(err, code.value) {
			failure.Code = code.name
			break
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		failure.Kind, failure.Detail = "timeout", "deadline-exceeded"
		failure.Code = ""
	case errors.Is(err, context.Canceled):
		failure.Kind, failure.Detail = "cancelled", "context-cancelled"
		failure.Code = ""
	case errors.Is(err, os.ErrPermission):
		failure.Kind, failure.Detail = "permission", "permission-denied"
	case errors.Is(err, diffsource.ErrNotRepository):
		failure.Kind, failure.Detail = "invalid-input", "not-a-repository"
	case errors.Is(err, os.ErrNotExist):
		failure.Kind, failure.Detail = "not-found", "not-found"
	default:
		var request *review.RequestError
		if errors.As(err, &request) {
			failure.Kind, failure.Detail = "git", "command-failed"
			if request.Status == 409 {
				failure.Detail = "conflict"
			}
			if request.Status == 413 {
				failure.Kind, failure.Detail = "limit", "budget-exceeded"
			}
		}
	}
	return failure
}

func hooksDiagnosticActivity(id, code string) hooks.Activity {
	return hooks.Activity{Stage: "collection", Status: "failed", DiagnosticID: id, Reason: code}
}

func diagnosticsWriter(values options) io.Writer {
	if values.diagnosticWriter != nil {
		return values.diagnosticWriter
	}
	return io.Discard
}
