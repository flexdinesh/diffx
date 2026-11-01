package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"dagger.io/dagger"
)

// This acceptance test uses only the explicitly provisioned repository engine.
// It must remain opt-in: the ordinary Go suite runs inside a check container,
// where neither a host Docker socket nor Dagger nesting is permitted.
func TestDaggerExecutionIsolation(t *testing.T) {
	if os.Getenv("DIFFX_DAGGER_INTEGRATION") != "1" {
		t.Skip("set DIFFX_DAGGER_INTEGRATION=1 after explicit engine setup")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	configureIsolationEnvironment(t, ctx)
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	first, err := dagger.Connect(firstCtx, dagger.WithRunnerHost("docker-container://"+engineName), dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatal(err)
	}
	firstClosed := false
	defer func() {
		if !firstClosed {
			_ = first.Close()
		}
	}()
	second, err := dagger.Connect(ctx, dagger.WithRunnerHost("docker-container://"+engineName), dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	firstMarker, secondMarker := isolationNonce(t), isolationNonce(t)
	firstService := isolationDatabase(first, firstMarker)
	secondService := isolationDatabase(second, secondMarker)
	type outcome struct {
		marker string
		output string
		err    error
	}
	results := make(chan outcome, 2)
	start := func(runCtx context.Context, client *dagger.Client, service *dagger.Service, marker string) {
		// Starting explicitly keeps the service alive across multiple clients'
		// queries until its owning session closes. Both use internal port 7980.
		if _, err := service.Start(runCtx); err != nil {
			results <- outcome{marker: marker, err: err}
			return
		}
		query := "CREATE TABLE isolation_marker (marker text); INSERT INTO isolation_marker VALUES ('" + marker + "'); SELECT marker FROM isolation_marker;"
		output, err := isolationQuery(runCtx, client, service, marker, query)
		results <- outcome{marker, output, err}
	}
	go start(firstCtx, first, firstService, firstMarker)
	go start(ctx, second, secondService, secondMarker)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent database %s: %v", result.marker, result.err)
		}
		if strings.TrimSpace(result.output) != result.marker {
			t.Fatalf("database marker = %q, want %q", result.output, result.marker)
		}
	}

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
		_, err = second.Container().From(postgresImage).
			WithEnvVariable("ISOLATION_EXECUTION", isolationNonce(t)).
			WithExec([]string{"sh", "-ec", "command -v wget >/dev/null; if wget -q -T 2 -O /tmp/response \"$1\"; then printf 'host localhost unexpectedly reachable\\n' >&2; exit 1; fi", "localhost-probe", host.URL}).
			Sync(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Cancel and close only the first session. The other session retains its
	// service and writable database state; the shared engine remains running.
	cancelFirst()
	_ = first.Close()
	firstClosed = true
	output, err := isolationQuery(ctx, second, secondService, isolationNonce(t), "SELECT marker FROM isolation_marker;")
	if err != nil {
		t.Fatalf("second execution failed after cancelling first: %v", err)
	}
	if strings.TrimSpace(output) != secondMarker {
		t.Fatalf("second database changed after cancellation: %q", output)
	}
	present, err := inspectEngine(ctx, engineCommandOutput)
	if err != nil || !present {
		t.Fatalf("shared engine did not survive session cancellation: present=%v, err=%v", present, err)
	}
}

func configureIsolationEnvironment(t *testing.T, ctx context.Context) {
	t.Helper()
	environment, err := engineEnvironment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// t.Setenv preserves the caller's environment after the test and prevents
	// accidentally inheriting a session belonging to a different repository.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "DAGGER_") || strings.HasPrefix(key, "_EXPERIMENTAL_DAGGER_") {
			t.Setenv(key, "")
		}
	}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "DAGGER_") || strings.HasPrefix(key, "_EXPERIMENTAL_DAGGER_") {
			t.Setenv(key, value)
		}
	}
	cli, err := exec.LookPath("dagger")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("_EXPERIMENTAL_DAGGER_CLI_BIN", cli)
}

func TestDaggerSourceUpload(t *testing.T) {
	if os.Getenv("DIFFX_DAGGER_INTEGRATION") != "1" {
		t.Skip("set DIFFX_DAGGER_INTEGRATION=1 after explicit engine setup")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	configureIsolationEnvironment(t, ctx)
	client, err := dagger.Connect(ctx, dagger.WithRunnerHost("docker-container://"+engineName), dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	root := t.TempDir()
	for _, directory := range []string{"internal/fixtures", "docs", "docs-website", ".git"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Enumerating these paths as HostDirectoryOpts.Include previously put the
	// entire list in HTTP headers and exceeded the filesync request limit.
	const count = 1200
	for index := range count {
		path := fmt.Sprintf("internal/fixtures/source-upload-regression-file-%04d.go", index)
		if err := os.WriteFile(filepath.Join(root, path), []byte("package fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"docs/excluded.md", "docs-website/excluded.md", ".git/config"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("must not upload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source, err := newPipeline(client, root).source("go")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := source.Directory("internal/fixtures").Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != count {
		t.Fatalf("uploaded %d files, want %d", len(entries), count)
	}
	contents, err := source.File("internal/fixtures/source-upload-regression-file-1199.go").Contents(ctx)
	if err != nil || contents != "package fixture\n" {
		t.Fatalf("uploaded contents = %q, err=%v", contents, err)
	}
	entries, err = source.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(entries, []string{"internal/"}) && !slices.Equal(entries, []string{"internal"}) {
		t.Fatalf("excluded source directories leaked into upload: %v", entries)
	}
}

func TestDaggerFailureArtifacts(t *testing.T) {
	if os.Getenv("DIFFX_DAGGER_INTEGRATION") != "1" {
		t.Skip("set DIFFX_DAGGER_INTEGRATION=1 after explicit engine setup")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	configureIsolationEnvironment(t, ctx)
	root, err := snapshotGit(ctx, ".", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, cleanup, err := captureSnapshot(ctx, strings.TrimSpace(string(root)), false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	client, err := dagger.Connect(ctx, dagger.WithRunnerHost("docker-container://"+engineName), dagger.WithLogOutput(os.Stderr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	artifacts := t.TempDir()
	group := checkGroup{
		Name:     "release",
		Commands: [][]string{{"sh", "-c", "printf 'failure-evidence\\n'; exit 17"}},
	}
	err = newPipeline(client, snapshot.Root).execute(ctx, group, isolationNonce(t), artifacts)
	if err == nil || !strings.Contains(err.Error(), "checks exited 17") {
		t.Fatalf("failure exit status was not preserved: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(artifacts, "release", "00.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "failure-evidence\n") {
		t.Fatalf("failure log was not retained: %q", log)
	}
	present, err := inspectEngine(ctx, engineCommandOutput)
	if err != nil || !present {
		t.Fatalf("shared engine did not survive failed check: present=%v, err=%v", present, err)
	}
}

func isolationNonce(t *testing.T) string {
	t.Helper()
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(bytes[:])
}

func isolationDatabase(client *dagger.Client, identity string) *dagger.Service {
	return client.Container().From(postgresImage).
		WithEnvVariable("POSTGRES_USER", "diffx").
		WithEnvVariable("POSTGRES_PASSWORD", "diffx").
		WithEnvVariable("POSTGRES_DB", "postgres").
		WithEnvVariable("ISOLATION_EXECUTION", identity).
		WithoutExposedPort(5432).
		WithExposedPort(7980).
		AsService(dagger.ContainerAsServiceOpts{Args: []string{"postgres", "-p", "7980"}, UseEntrypoint: true})
}

func isolationQuery(ctx context.Context, client *dagger.Client, service *dagger.Service, identity, query string) (string, error) {
	return client.Container().From(postgresImage).
		WithServiceBinding("database", service).
		WithEnvVariable("PGPASSWORD", "diffx").
		WithEnvVariable("ISOLATION_EXECUTION", identity).
		WithExec([]string{"psql", "--host=database", "--port=7980", "--username=diffx", "--dbname=postgres", "--no-psqlrc", "--quiet", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", fmt.Sprintf("--command=%s", query)}).
		Stdout(ctx)
}
