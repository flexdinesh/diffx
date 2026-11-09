package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const isolationPostgresImage = "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5"

// This acceptance test drives real Compose projects. It must remain opt-in:
// the ordinary Go suite runs inside a check container without a Docker socket.
func TestComposeExecutionIsolation(t *testing.T) {
	if os.Getenv("DIFFX_COMPOSE_INTEGRATION") != "1" {
		t.Skip("set DIFFX_COMPOSE_INTEGRATION=1 on a host with Docker Compose")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	if err := verifyDocker(ctx, dockerOutput); err != nil {
		t.Fatal(err)
	}
	if err := ensureCacheVolume(ctx, dockerOutput); err != nil {
		t.Fatal(err)
	}
	compose, err := filepath.Abs("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	nonce := isolationNonce(t)
	first, second := "diffx-check-isolation-"+nonce+"-a", "diffx-check-isolation-"+nonce+"-b"
	for _, project := range []string{first, second} {
		t.Cleanup(func() { _ = teardownProject(dockerOutput, project) })
	}
	firstMarker, secondMarker := isolationNonce(t), isolationNonce(t)
	results := make(chan error, 2)
	for _, run := range []struct{ project, marker string }{{first, firstMarker}, {second, secondMarker}} {
		go func() {
			output, err := isolationQuery(ctx, compose, run.project, "CREATE TABLE isolation_marker (marker text); INSERT INTO isolation_marker VALUES ('"+run.marker+"'); SELECT marker FROM isolation_marker;")
			if err == nil && strings.TrimSpace(output) != run.marker {
				err = fmt.Errorf("database marker = %q, want %q", output, run.marker)
			}
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent projects: %v", err)
		}
	}

	t.Run("no published ports or Docker socket", func(t *testing.T) {
		for _, project := range []string{first, second} {
			output, err := dockerOutput(ctx, "container", "ls", "--all", "--quiet", "--filter", "label="+projectLabel+"="+project)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range dockerLines(output) {
				raw, err := dockerOutput(ctx, "container", "inspect", id)
				if err != nil {
					t.Fatal(err)
				}
				var containers []struct {
					HostConfig struct {
						NetworkMode  string
						PortBindings map[string]json.RawMessage
					}
					Mounts []struct{ Source string }
				}
				if err := json.Unmarshal(raw, &containers); err != nil || len(containers) != 1 {
					t.Fatalf("inspect %s: %v", id, err)
				}
				config := containers[0]
				if len(config.HostConfig.PortBindings) != 0 || config.HostConfig.NetworkMode == "host" {
					t.Fatalf("container %s publishes ports or uses host networking", id)
				}
				for _, mount := range config.Mounts {
					if strings.Contains(mount.Source, "docker.sock") {
						t.Fatalf("container %s mounts the Docker socket", id)
					}
				}
			}
		}
	})

	t.Run("host localhost is not runner localhost", func(t *testing.T) {
		host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "host-only-marker")
		}))
		defer host.Close()
		response, err := host.Client().Get(host.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || string(body) != "host-only-marker" {
			t.Fatalf("host listener was not reachable on host: %q, %v", body, readErr)
		}
		// Verify wget exists so missing tooling cannot masquerade as isolation.
		_, err = isolationRun(ctx, compose, second, "runner", "sh", "-ec", "command -v wget >/dev/null; if wget -q -T 2 -O /tmp/response \"$1\"; then printf 'host localhost unexpectedly reachable\\n' >&2; exit 1; fi", "localhost-probe", host.URL)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Tearing down one project leaves the other's services and state intact.
	if err := teardownProject(dockerOutput, first); err != nil {
		t.Fatal(err)
	}
	output, err := isolationQuery(ctx, compose, second, "SELECT marker FROM isolation_marker;")
	if err != nil {
		t.Fatalf("second project failed after removing first: %v", err)
	}
	if strings.TrimSpace(output) != secondMarker {
		t.Fatalf("second database changed after removing first: %q", output)
	}
	remaining, err := dockerOutput(ctx, "container", "ls", "--all", "--quiet", "--filter", "label="+projectLabel+"="+first)
	if err != nil || len(dockerLines(remaining)) != 0 {
		t.Fatalf("first project left containers: %q, %v", remaining, err)
	}
}

func isolationNonce(t *testing.T) string {
	t.Helper()
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(bytes[:])
}

// isolationRun uses the Postgres image as the runner image: it has psql and
// wget, so the real compose services exercise isolation without a check build.
func isolationRun(ctx context.Context, compose, project, service string, command ...string) (string, error) {
	args := append([]string{"compose", "--file", compose, "--project-name", project, "run", "--rm", "--no-TTY", "--interactive=false", service}, command...)
	run := exec.CommandContext(ctx, "docker", args...)
	run.Env = append(checkEnvironment(os.Environ()), "DIFFX_CHECK_IMAGE="+isolationPostgresImage)
	output, err := run.Output()
	if err != nil {
		var stderr string
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			stderr = string(exit.Stderr)
		}
		return "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, stderr)
	}
	return string(output), nil
}

func isolationQuery(ctx context.Context, compose, project, query string) (string, error) {
	return isolationRun(ctx, compose, project, "runner-db", "sh", "-ec", "PGPASSWORD=diffx exec psql --host=postgres --username=diffx --dbname=postgres --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 --command=\"$1\"", "query", query)
}
