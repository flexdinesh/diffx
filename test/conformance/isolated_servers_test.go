package conformance

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/localserver"
)

func TestIsolatedCheckoutServers(t *testing.T) {
	type instance struct {
		client *localserver.Client
		status localserver.Status
		done   chan error
		root   string
	}
	start := func(name string) instance {
		t.Helper()
		harness := newServiceHarness(t)
		// Exercise runtime-derived config and persistent state, regardless of
		// the invoking developer's explicit config/environment overrides.
		environment := make([]string, 0, len(harness.environment))
		for _, entry := range harness.environment {
			key, _, _ := strings.Cut(entry, "=")
			if strings.EqualFold(key, "DIFFX_CONFIG_PATH") || strings.HasPrefix(strings.ToUpper(key), "DIFFX_LOCAL_") {
				continue
			}
			environment = append(environment, entry)
		}
		root := createWorktree(t, name)
		logPath := filepath.Join(t.TempDir(), "server.log")
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = log.Close() })
		command := exec.Command(serviceBinary, root, "--port", "0", "--no-browser")
		command.Dir, command.Env = root, environment
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		t.Cleanup(func() { _ = command.Process.Kill() })
		client := &localserver.Client{RuntimeDirectory: harness.runtimeDir}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			status, err := client.Status(t.Context())
			if err == nil && status.State == "running" {
				if status.PID != command.Process.Pid || status.Settings.State != filepath.Join(harness.runtimeDir, "state.db") {
					t.Fatalf("wrong process or runtime-derived state: %+v", status)
				}
				if code := statusOf(t, http.MethodPost, status.BrowserURL+"/api/v2/collections?initial=true"); code != http.StatusOK {
					t.Fatalf("initial collection: %d", code)
				}
				return instance{client: client, status: status, done: done, root: root}
			}
			select {
			case err := <-done:
				output, _ := os.ReadFile(logPath)
				t.Fatalf("server exited: %v\n%s", err, output)
			default:
			}
			time.Sleep(20 * time.Millisecond)
		}
		output, _ := os.ReadFile(logPath)
		t.Fatalf("server unavailable\n%s", output)
		return instance{}
	}
	readCheckout := func(server instance) string {
		t.Helper()
		var page contextservice.Page
		requestJSON(t, server.status.BrowserURL+"/api/v2/contexts", &page)
		if len(page.Contexts) != 1 || page.Contexts[0].Observation == nil || page.Contexts[0].Observation.Root != server.root {
			t.Fatalf("wrong checkout collected: %+v", page)
		}
		return page.Contexts[0].ID
	}
	stop := func(server instance) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		if err := server.client.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-server.done:
			if err != nil {
				t.Fatalf("server did not exit gracefully: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("server did not stop")
		}
		if _, err := os.Stat(server.status.Settings.State); err != nil {
			t.Fatalf("persistent state missing after stop: %v", err)
		}
	}

	first, second := start("first-isolated"), start("second-isolated")
	if first.status.BrowserURL == second.status.BrowserURL || first.status.Settings.State == second.status.Settings.State {
		t.Fatal("servers share a listener or database")
	}
	firstID, secondID := readCheckout(first), readCheckout(second)
	for _, pair := range []struct {
		server instance
		other  string
	}{{first, secondID}, {second, firstID}} {
		if code := statusOf(t, http.MethodGet, pair.server.status.BrowserURL+"/api/v2/contexts/"+pair.other); code != http.StatusNotFound {
			t.Fatalf("other server's context visible: %d", code)
		}
	}
	stop(first)
	status, err := second.client.Status(t.Context())
	if err != nil || status.InstanceID != second.status.InstanceID || status.State != "running" {
		t.Fatalf("stopping first affected second: %+v %v", status, err)
	}
	if code := statusOf(t, http.MethodPost, second.status.BrowserURL+"/api/v2/collections"); code != http.StatusOK {
		t.Fatalf("surviving server cannot collect: %d", code)
	}
	if id := readCheckout(second); id != secondID {
		t.Fatalf("surviving server lost review: %s != %s", id, secondID)
	}
	stop(second)
}
