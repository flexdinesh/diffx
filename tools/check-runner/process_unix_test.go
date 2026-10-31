//go:build !windows

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Full checks supply built assets; exercise the actual Node fixture launcher
// so a detached fixture cannot silently escape cancellation ownership.
func TestFixtureCancellation(t *testing.T) {
	binary := os.Getenv("DIFFX_FIXTURE_BINARY")
	if binary == "" {
		t.Skip("requires full-check fixture build")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "url")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	script := `import { startFixtureProcess } from './apps/web/test/fixture-server.ts';
import { writeFileSync } from 'node:fs';
const fixture = await startFixtureProcess({ binary: process.env.DIFFX_FIXTURE_BINARY });
writeFileSync(process.env.DIFFX_FIXTURE_READY, fixture.target);
await new Promise(() => {});`
	go func() {
		done <- runCommand(ctx, root, append(os.Environ(), "DIFFX_CHECK_RUNNER=1", "DIFFX_FIXTURE_READY="+ready), []string{"node", "--input-type=module", "--eval", script}, os.Stdout, os.Stderr)
	}()
	var target string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(ready); err == nil {
			target = strings.TrimSpace(string(data))
			break
		}
		select {
		case err := <-done:
			t.Fatalf("fixture launcher exited early: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if target == "" {
		t.Fatal("fixture never became ready")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation not propagated: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fixture cancellation hung")
	}
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get(target + "/api/v2/health"); err == nil {
		_ = response.Body.Close()
		t.Fatal("fixture still serves after runner cleanup")
	}
}

func TestCommandHelper(t *testing.T) {
	switch os.Getenv("DIFFX_CHECK_TEST_HELPER") {
	case "failure":
		os.Exit(23)
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestCommandHelper$")
		child.Env = append(os.Environ(), "DIFFX_CHECK_TEST_HELPER=heartbeat")
		if err := child.Start(); err != nil {
			os.Exit(24)
		}
		_ = child.Wait()
		os.Exit(0)
	case "heartbeat":
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
		for {
			if err := os.WriteFile(os.Getenv("DIFFX_CHECK_TEST_HEARTBEAT"), []byte(time.Now().String()), 0o600); err != nil {
				os.Exit(25)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestCommandExitCode(t *testing.T) {
	err := runCommand(context.Background(), t.TempDir(), append(os.Environ(), "DIFFX_CHECK_TEST_HELPER=failure"), []string{os.Args[0], "-test.run=^TestCommandHelper$"}, os.Stdout, os.Stderr)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("want exit 23, got %v", err)
	}
}

func TestCancellationStopsDescendantsBeforeUnlock(t *testing.T) {
	root := repository(t)
	heartbeat := filepath.Join(t.TempDir(), "heartbeat")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- withLock(ctx, root, func() error {
			return runCommand(ctx, root, append(os.Environ(), "DIFFX_CHECK_TEST_HELPER=parent", "DIFFX_CHECK_TEST_HEARTBEAT="+heartbeat), []string{os.Args[0], "-test.run=^TestCommandHelper$"}, os.Stdout, os.Stderr)
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if contents, err := os.ReadFile(heartbeat); err == nil && len(contents) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation not propagated: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := withLock(lockCtx, root, func() error {
		before, err := os.ReadFile(heartbeat)
		if err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		after, err := os.ReadFile(heartbeat)
		if err != nil {
			return err
		}
		if string(before) != string(after) {
			t.Error("descendant continued running after lock release")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
