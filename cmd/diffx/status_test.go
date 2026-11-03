package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/localserver"
)

// shortRuntime keeps the socket path below sun_path for long test names.
func shortRuntime(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("DIFFX_RUNTIME_DIR", dir)
	return dir
}

func TestStatusReportsStoppedWithExitCode(t *testing.T) {
	shortRuntime(t)
	var output bytes.Buffer
	err := runStatus(t.Context(), []string{"--json"}, &output, io.Discard)
	var exit exitStatus
	if !errors.As(err, &exit) || exit != 3 {
		t.Fatalf("stopped exit: %v", err)
	}
	if output.String() != "{\"state\":\"stopped\"}\n" {
		t.Fatalf("stopped report: %s", output.String())
	}
}

func TestStatusReportsTheRunningInstanceContract(t *testing.T) {
	dir := shortRuntime(t)
	for _, headless := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		ready := make(chan localserver.Status, 1)
		done := make(chan error, 1)
		go func() {
			done <- localserver.RunForeground(ctx, localserver.Settings{Port: -1, Headless: headless, State: "memory"}, localserver.Input{}, nil, localserver.StateReset{},
				func(status localserver.Status, _ localserver.Published) { ready <- status })
		}()
		var started localserver.Status
		select {
		case started = <-ready:
		case err := <-done:
			t.Fatalf("startup: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("startup timed out")
		}
		var output bytes.Buffer
		if err := runStatus(t.Context(), []string{"--json"}, &output, io.Discard); err != nil {
			t.Fatal(err)
		}
		var report map[string]any
		if err := json.Unmarshal(output.Bytes(), &report); err != nil {
			t.Fatalf("report %s: %v", output.String(), err)
		}
		want := map[string]any{
			"state": "running", "protocolVersion": float64(6), "instanceId": started.InstanceID, "pid": float64(os.Getpid()),
			"version": started.Version, "socket": localserver.SocketPath(dir), "viewerUrl": started.ViewerURL, "observations": float64(0),
		}
		if len(report) != len(want) {
			t.Fatalf("report fields %v, want %v", report, want)
		}
		for key, value := range want {
			if report[key] != value {
				t.Fatalf("%s = %v, want %v (headless=%t)", key, report[key], value, headless)
			}
		}
		if (started.ViewerURL == "") != headless {
			t.Fatalf("viewer %q with headless=%t", started.ViewerURL, headless)
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("instance did not stop")
		}
	}
}
