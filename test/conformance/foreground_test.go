package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestForegroundPipedPatchAndPersistentHistory(t *testing.T) {
	harness := newServiceHarness(t)
	patch, err := os.ReadFile("../fixtures/sample.diff")
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	configuration, err := json.Marshal(config.Values{Local: config.LocalValues{Host: "127.0.0.1", State: harness.state, Port: &port, RetentionDays: 7}, Remote: config.RemoteValues{URL: "http://127.0.0.1:1", Token: "remote-token"}})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, configuration, 0600); err != nil {
		t.Fatal(err)
	}
	harness.environment = append(harness.environment, "DIFFX_CONFIG_PATH="+configPath, "DIFFX_REMOTE_URL=http://127.0.0.1:1", "SSH_CONNECTION=test")
	client := &localserver.Client{RuntimeDirectory: harness.runtimeDir}
	// The first file alone is a different capture in the same database.
	otherPatch := patch[:bytes.Index(patch, []byte("\ndiff --git"))+1]
	var original review.RepositoryDiff
	var originalID string
	for _, source := range []string{"pipe", "file", "other"} {
		t.Run(source, func(t *testing.T) {
			var stdin io.Reader = bytes.NewReader(patch)
			var args []string
			if source == "other" {
				stdin = bytes.NewReader(otherPatch)
			}
			if source == "file" {
				path := filepath.Join(t.TempDir(), "saved.patch")
				if err := os.WriteFile(path, patch, 0600); err != nil {
					t.Fatal(err)
				}
				file, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				stdin, args = file, []string{"--no-browser"}
			}
			logPath := filepath.Join(t.TempDir(), "output.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			command := exec.Command(serviceBinary, args...)
			command.Env, command.Dir, command.Stdin = harness.environment, t.TempDir(), stdin
			command.Stdout, command.Stderr = log, log
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill() })
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			var status localserver.Status
			deadline := time.Now().Add(8 * time.Second)
			for {
				status, err = client.Status(t.Context())
				if err == nil && status.State == "running" {
					break
				}
				select {
				case err := <-done:
					output, _ := os.ReadFile(logPath)
					t.Fatalf("patch server exited: %v\n%s", err, output)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("patch server unavailable")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if status.PID != command.Process.Pid {
				t.Fatalf("patch not served in invoking process: %+v", status)
			}
			// Each process serves only its own capture; history stays stored.
			var catalog contextservice.Page
			requestJSON(t, status.BrowserURL+"/api/v2/contexts", &catalog)
			if len(catalog.Contexts) != 1 {
				t.Fatalf("patch duplicated, lost or mixed with other captures: %+v", catalog)
			}
			entry := catalog.Contexts[0]
			if entry.Source != "stdin" || entry.RepositoryID != nil || entry.Branch != nil || !entry.Capabilities.Diff.Scopes.Allows(review.DiffAll) || entry.Capabilities.Diff.Scopes.Allows(review.DiffStaged) || entry.Capabilities.Files.Contents.Enabled() {
				t.Fatalf("incorrect patch identity/capabilities: %+v", entry)
			}
			var diff review.RepositoryDiff
			requestJSON(t, status.BrowserURL+"/api/v2/contexts/"+entry.ID+"/diffs/current?scope=all", &diff)
			files := 12
			if source == "other" {
				files = 1
			}
			if diff.Source != "stdin" || len(diff.Files) != files {
				t.Fatalf("patch contents: %+v", diff)
			}
			switch source {
			case "pipe":
				original, originalID = diff, entry.ID
			case "file":
				if diff.ID != original.ID || diff.VersionID != original.VersionID || entry.ID != originalID {
					t.Fatalf("restart lost patch identity: before=%+v after=%+v", original, diff)
				}
			default:
				if entry.ID == originalID || statusOf(t, http.MethodGet, status.BrowserURL+"/api/v2/contexts/"+originalID) != http.StatusNotFound {
					t.Fatalf("another capture visible to this process: %+v", entry)
				}
			}
			var bootstrap struct {
				Collect *bool `json:"collect"`
			}
			requestJSON(t, status.BrowserURL+"/api/v2/bootstrap", &bootstrap)
			if bootstrap.Collect == nil || *bootstrap.Collect || statusOf(t, http.MethodPost, status.BrowserURL+"/api/v2/collections") != http.StatusNotFound {
				t.Fatalf("piped capture exposed checkout collection: %+v", bootstrap)
			}
			response, err := http.Post(status.BrowserURL+"/api/v2/ingestions", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("patch exposed HTTP ingestion: %d", response.StatusCode)
			}
			output, err := harness.run(patch, "--no-browser")
			if err == nil || !strings.Contains(string(output), "--replace") {
				t.Fatalf("patch replacement not guarded: %s %v", output, err)
			}
			if err := client.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("patch server did not exit gracefully")
			}
			output, err = os.ReadFile(logPath)
			if err != nil || !strings.Contains(string(output), status.BrowserURL+"/contexts/"+entry.ID) || !strings.Contains(string(output), "mode:               pipe") || !strings.Contains(string(output), "ctrl-c to stop") {
				t.Fatalf("incorrect startup output: %s %v", output, err)
			}
		})
	}
}

func TestForegroundRejectsPathAndPipedStdinBeforeStartup(t *testing.T) {
	harness := newServiceHarness(t)
	root := createWorktree(t, "conflicting-input")
	for _, args := range [][]string{{root}, {"--path", root}, {"pipe"}} {
		output, err := harness.run([]byte("not a patch"), args...)
		if err == nil || !strings.Contains(string(output), "cannot be combined") {
			t.Fatalf("accepted conflicting input %v: %s %v", args, output, err)
		}
	}
	if _, err := localserver.ReadDescriptor(harness.runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("invalid input started a service: %v", err)
	}
}

func TestForegroundSnapshotReplacementAndPersistentHistory(t *testing.T) {
	harness := newServiceHarness(t)
	first := createWorktree(t, "first-local")
	second := filepath.Join(t.TempDir(), "second-local")
	collectorGit(t, first, "worktree", "add", "-b", "linked-branch", second)
	if err := os.WriteFile(filepath.Join(second, "value.txt"), []byte("linked edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client := &localserver.Client{RuntimeDirectory: harness.runtimeDir}
	start := func(path string, extra ...string) (*exec.Cmd, chan error) {
		t.Helper()
		args := []string{"--state", harness.state, "--port", "0", "--no-browser"}
		if path != "" {
			args = append(args, path)
		}
		args = append(args, extra...)
		command := exec.Command(serviceBinary, args...)
		command.Dir = first
		command.Env = append(harness.environment, "DIFFX_REMOTE_URL=http://127.0.0.1:1")
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		t.Cleanup(func() { _ = command.Process.Kill() })
		return command, done
	}
	await := func(previous string) localserver.Status {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			status, err := client.Status(t.Context())
			if err == nil && status.State == "running" && status.InstanceID != previous {
				// Readiness precedes collection. Join the initial live-page request
				// before asserting snapshots or making a later explicit Sync.
				response, err := http.Post(status.BrowserURL+"/api/v2/collections?initial=true", "application/json", nil)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("initial collection: %d", response.StatusCode)
				}
				return status
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("foreground instance unavailable")
		return localserver.Status{}
	}
	original, done := start("")
	status := await("")
	if status.PID != original.Process.Pid {
		t.Fatalf("wrong foreground owner: %+v", status)
	}
	var initial contextservice.Page
	requestJSON(t, status.BrowserURL+"/api/v2/contexts", &initial)
	byRoot := func(page contextservice.Page, root string) contextservice.Context {
		t.Helper()
		for _, item := range page.Contexts {
			if item.Observation != nil && item.Observation.Root == root {
				return item
			}
		}
		t.Fatalf("no context for %s: %+v", root, page)
		return contextservice.Context{}
	}
	// Local collection includes every registered worktree of the repository.
	if len(initial.Contexts) != 2 {
		t.Fatalf("local collection missed sibling worktree: %+v", initial)
	}
	entry, linkedEntry := byRoot(initial, first), byRoot(initial, second)
	metadata := entry.Observation
	if metadata == nil || metadata.Root != first || metadata.RepositoryKey == "" || metadata.CheckoutKey == "" || metadata.Branch == "" || metadata.LinkedWorktree == nil || *metadata.LinkedWorktree || entry.Capabilities.Diff.Refresh.Enabled() {
		t.Fatalf("local checkout metadata/capabilities: %+v", entry)
	}
	linked := linkedEntry.Observation
	if linked == nil || linked.TriggerRoot != first || linked.Branch != "linked-branch" || linked.LinkedWorktree == nil || !*linked.LinkedWorktree || linked.RepositoryKey != metadata.RepositoryKey || linked.CheckoutKey == metadata.CheckoutKey {
		t.Fatalf("linked checkout metadata lost: %+v", linked)
	}
	var snapshot review.RepositoryDiff
	requestJSON(t, status.BrowserURL+"/api/v2/contexts/"+entry.ID+"/diffs/current?scope=all", &snapshot)
	var health struct {
		IngestionEnabled bool `json:"ingestionEnabled"`
		QueuedIngestion  bool `json:"queuedIngestion"`
	}
	requestJSON(t, status.BrowserURL+"/api/v2/health", &health)
	if health.IngestionEnabled || health.QueuedIngestion {
		t.Fatalf("foreground advertised external ingestion: %+v", health)
	}
	response, err := http.Post(status.BrowserURL+"/api/v2/ingestions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("foreground exposed HTTP ingestion: %d", response.StatusCode)
	}
	if output, err := harness.run(nil, second, "--no-browser"); err == nil || !strings.Contains(string(output), "--replace") {
		t.Fatalf("noninteractive replacement not guarded: %s %v", output, err)
	}
	if err := os.WriteFile(filepath.Join(first, "value.txt"), []byte("later edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	collectorGit(t, first, "switch", "-c", "later-branch")
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var catalog contextservice.Page
		requestJSON(t, status.BrowserURL+"/api/v2/contexts", &catalog)
		if len(catalog.Contexts) != 2 || byRoot(catalog, first).ID != entry.ID {
			t.Fatalf("foreground recollected checkout: %+v", catalog)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var retained review.RepositoryDiff
	requestJSON(t, status.BrowserURL+"/api/v2/contexts/"+entry.ID+"/diffs/current?scope=all", &retained)
	if retained.Revision != snapshot.Revision || retained.Branch != snapshot.Branch {
		t.Fatalf("snapshot followed edits or branch switch: %+v", retained)
	}
	var bootstrap struct {
		Collect *bool `json:"collect"`
	}
	requestJSON(t, status.BrowserURL+"/api/v2/bootstrap", &bootstrap)
	if bootstrap.Collect == nil || !*bootstrap.Collect {
		t.Fatalf("checkout process cannot collect: %+v", bootstrap)
	}
	// Only an explicit request collects the selected checkouts again.
	response, err = http.Post(status.BrowserURL+"/api/v2/collections", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Contexts []contextservice.Context        `json:"contexts"`
		Failures []localserver.CollectionFailure `json:"failures"`
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(result.Contexts) != 2 || len(result.Failures) != 0 {
		t.Fatalf("explicit collection: %d %+v %v", response.StatusCode, result, err)
	}
	refreshed := byRoot(contextservice.Page{Contexts: result.Contexts}, first)
	if refreshed.ID == entry.ID || refreshed.Observation.Branch != "later-branch" || refreshed.Observation.CheckoutKey != metadata.CheckoutKey {
		t.Fatalf("explicit collection: %+v", refreshed)
	}
	// Unchanged checkouts reuse their review.
	if byRoot(contextservice.Page{Contexts: result.Contexts}, second).ID != linkedEntry.ID {
		t.Fatalf("unchanged worktree created a new review: %+v", result.Contexts)
	}
	var collected contextCatalog
	requestJSON(t, status.BrowserURL+"/api/v2/contexts", &collected)
	if len(collected.Contexts) != 3 {
		t.Fatalf("explicit collection catalog: %+v", collected)
	}
	requestJSON(t, status.BrowserURL+"/api/v2/contexts/"+entry.ID+"/diffs/current?scope=all", &retained)
	if retained.Revision != snapshot.Revision || retained.Branch != snapshot.Branch {
		t.Fatalf("collection replaced stored snapshot: %+v", retained)
	}
	other := createWorktree(t, "other-local")
	subdir := filepath.Join(other, "subdir")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	_, replacementDone := start(subdir, "--replace", "--host", "0.0.0.0")
	replacement := await(status.InstanceID)
	if replacement.Settings.Host != "0.0.0.0" {
		t.Fatal("host flag ignored")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("old foreground did not exit gracefully: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("old process survived replacement")
	}
	// The replacement serves only its own discovered checkouts.
	var catalog contextservice.Page
	requestJSON(t, replacement.BrowserURL+"/api/v2/contexts", &catalog)
	if len(catalog.Contexts) != 1 {
		t.Fatalf("replacement showed another checkout: %+v", catalog)
	}
	nested := catalog.Contexts[0].Observation
	if nested == nil || nested.Root != other || nested.TriggerRoot != subdir {
		t.Fatalf("nested path did not resolve to its checkout: %+v", nested)
	}
	for _, id := range []string{entry.ID, refreshed.ID, linkedEntry.ID} {
		if code := statusOf(t, http.MethodGet, replacement.BrowserURL+"/api/v2/contexts/"+id); code != http.StatusNotFound {
			t.Fatalf("other checkout context %s visible: %d", id, code)
		}
	}
	// Reopening the first checkout shows its repository's persistent history again.
	_, reopenedDone := start("", "--replace")
	reopened := await(replacement.InstanceID)
	select {
	case err := <-replacementDone:
		if err != nil {
			t.Fatalf("replacement did not exit gracefully: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement survived reopening")
	}
	var history contextCatalog
	requestJSON(t, reopened.BrowserURL+"/api/v2/contexts", &history)
	found := map[string]bool{}
	for _, item := range history.Contexts {
		if item.Root == nil || (*item.Root != first && *item.Root != second) {
			t.Fatalf("reopened checkout showed another checkout: %+v", history)
		}
		found[item.ID] = true
	}
	if !found[entry.ID] || !found[refreshed.ID] || !found[linkedEntry.ID] {
		t.Fatalf("reopened checkout lost history: %+v", history)
	}
	// The server may use its five-second shutdown grace before forcing a
	// connection closed; leave time for descriptor and ownership cleanup too.
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reopenedDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("reopened process did not terminate")
	}
}

func statusOf(t *testing.T, method, endpoint string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}
