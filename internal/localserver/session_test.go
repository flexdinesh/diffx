package localserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/controlapi"
	"github.com/flexdinesh/diffx/internal/ingestion"
	buildversion "github.com/flexdinesh/diffx/internal/version"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

func sessionRuntime(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "dx-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	t.Setenv("DIFFX_RUNTIME_DIR", directory)
	return directory
}

func sessionInputForTest(t *testing.T) Input {
	t.Helper()
	return Input{Selection: SelectedCheckout, Session: &localprotocol.Session{ProtocolVersion: localprotocol.Version, ScopeID: "scope-one", Input: localprotocol.Input{Kind: "checkout", Selection: "checkout", Paths: []string{t.TempDir()}}}, Collect: func(ctx context.Context, _ func(ingestion.Request) error) ([]CollectionFailure, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
}

func awaitSession(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("session did not complete")
		return nil
	}
}

func TestSessionAttachesToForegroundAndPreservesItOnReadinessFailure(t *testing.T) {
	runtime := sessionRuntime(t)
	input := sessionInputForTest(t)
	settings := Settings{Port: 0, State: filepath.Join(runtime, "state.db")}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan Status, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunForeground(ctx, settings, input, func(Status) (bool, error) { return false, errors.New("unexpected existing owner") }, StateReset{}, func(status Status, _ Published) { ready <- status })
	}()
	var status Status
	select {
	case status = <-ready:
	case err := <-done:
		t.Fatalf("foreground: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("foreground readiness timeout")
	}
	if status.Session == nil || status.Session.ScopeID != input.Session.ScopeID {
		t.Fatalf("foreground metadata: %+v", status)
	}
	err := RunSession(t.Context(), settings, input, SessionOptions{}, StateReset{}, func(ready localprotocol.Readiness) error {
		if ready.Owned || ready.InstanceID != status.InstanceID {
			t.Errorf("attached readiness: %+v", ready)
		}
		return errors.New("readiness failed")
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "readiness failed") {
		t.Fatalf("readiness result: %v", err)
	}
	if current, err := (&Client{RuntimeDirectory: runtime}).Status(t.Context()); err != nil || current.InstanceID != status.InstanceID || current.State != "running" {
		t.Fatalf("attached failure stopped foreground: %+v %v", current, err)
	}
	attachedCtx, stopAttached := context.WithCancel(t.Context())
	defer stopAttached()
	attached := make(chan struct{}, 1)
	attachedDone := make(chan error, 1)
	go func() {
		// Explicit attachment uses the running instance's effective settings.
		attachedDone <- RunSession(attachedCtx, Settings{Headless: true, RetentionDays: -1}, Input{}, SessionOptions{Attach: true}, StateReset{}, func(localprotocol.Readiness) error { attached <- struct{}{}; return nil }, io.Discard)
	}()
	select {
	case <-attached:
	case <-time.After(10 * time.Second):
		t.Fatal("attach timeout")
	}
	cancel()
	if err := awaitSession(t, done); err != nil {
		t.Fatal(err)
	}
	if err := awaitSession(t, attachedDone); err == nil || !strings.Contains(err.Error(), "attached local instance") {
		t.Fatalf("unexpected owner loss result: %v", err)
	}
}

func TestSessionReadinessFailureJoinsCollectionBeforeReleasingState(t *testing.T) {
	runtime := sessionRuntime(t)
	input := sessionInputForTest(t)
	var joined atomic.Bool
	started := make(chan struct{})
	input.Collect = func(ctx context.Context, _ func(ingestion.Request) error) ([]CollectionFailure, error) {
		close(started)
		<-ctx.Done()
		joined.Store(true)
		return nil, ctx.Err()
	}
	err := RunSession(t.Context(), Settings{Port: 0, State: filepath.Join(runtime, "state.db")}, input, SessionOptions{}, StateReset{}, func(localprotocol.Readiness) error { <-started; return errors.New("readiness failed") }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "readiness failed") || !joined.Load() {
		t.Fatalf("cleanup: joined=%t, err=%v", joined.Load(), err)
	}
	if status, err := (&Client{RuntimeDirectory: runtime}).Status(t.Context()); err != nil || status.State != "stopped" {
		t.Fatalf("ownership not released: %+v %v", status, err)
	}
}

func TestSessionCompatibilityAndEffectiveSettings(t *testing.T) {
	status := Status{State: "running", Version: buildversion.Number(), ProtocolVersion: controlapi.ProtocolVersion, Socket: "/tmp/socket", InstanceID: "instance", ViewerURL: "http://127.0.0.1:7981", Session: sessionInputForTest(t).Session}
	if err := compatibleSession(status); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Status){
		func(s *Status) { s.Session = nil },
		func(s *Status) { s.ProtocolVersion-- },
		func(s *Status) { s.Version = "incompatible" },
		func(s *Status) { s.ViewerURL = "" },
		func(s *Status) { s.ViewerURL = "http://192.0.2.1:7981" },
		func(s *Status) { s.ViewerURL = "http://127.0.0.1:7981/contexts/id" },
	} {
		copy := status
		mutate(&copy)
		if err := compatibleSession(copy); err == nil {
			t.Fatalf("accepted incompatible: %+v", copy)
		}
	}
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	settings, err := effectiveSettings(Settings{State: filepath.Join(alias, "new", "state.db"), WebDir: alias})
	if err != nil || settings.State != filepath.Join(root, "new", "state.db") || settings.WebDir != root || settings.RetentionDays != 7 {
		t.Fatalf("effective settings: %+v %v", settings, err)
	}
}
