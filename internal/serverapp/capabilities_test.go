package serverapp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/mcpapi"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAdmissionCapabilitiesMatchHealthAndRoutes(t *testing.T) {
	for _, test := range []struct {
		name    string
		queue   bool
		collect bool
		want    serverapp.Capabilities
	}{
		{"foreground capture", false, false, serverapp.Capabilities{}},
		{"foreground checkout", false, true, serverapp.Capabilities{Collection: true}},
		{"remote queued", true, false, serverapp.Capabilities{IngestionEnabled: true, QueuedIngestion: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := reviewstore.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			user, err := store.User("local", "Local")
			if err != nil {
				t.Fatal(err)
			}
			service := contextservice.NewWithContext(t.Context(), store, user)
			defer service.Close()
			options := serverapp.Options{}
			if test.queue {
				options.Queue = store.Queue()
			}
			if test.collect {
				options.Collect = func(context.Context) (ingestion.Request, error) { return ingestion.Request{}, errors.New("unused") }
			}
			if got := options.Capabilities(); got != test.want {
				t.Fatalf("capabilities: %+v, want %+v", got, test.want)
			}
			server := httptest.NewServer(serverapp.Handler(t.Context(), service, store, fstest.MapFS{}, "", options))
			defer server.Close()
			d := deployment{server: server}
			health := decode[ingestion.Health](t, request(t, d, "GET", "/api/v2/health", nil))
			if health.IngestionEnabled != test.want.IngestionEnabled || health.QueuedIngestion != test.want.QueuedIngestion {
				t.Fatalf("health capabilities: %+v, want %+v", health, test.want)
			}
			bootstrap := decode[struct {
				Collect bool `json:"collect"`
			}](t, request(t, d, "GET", "/api/v2/bootstrap", nil))
			if bootstrap.Collect != test.want.Collection {
				t.Fatalf("bootstrap collect: %v, want %v", bootstrap.Collect, test.want.Collection)
			}
			collections := request(t, d, "GET", "/api/v2/collections", nil)
			collections.Body.Close()
			if want := map[bool]int{false: http.StatusNotFound, true: http.StatusMethodNotAllowed}[test.want.Collection]; collections.StatusCode != want {
				t.Fatalf("collections route: status %d, want %d", collections.StatusCode, want)
			}
			for path, enabled := range map[string]bool{"/api/v2/ingestions": false, "/api/v2/ingestion-jobs": test.want.QueuedIngestion} {
				response := request(t, d, "POST", path, bytes.NewBufferString("{}"))
				response.Body.Close()
				want := http.StatusNotFound
				if enabled {
					want = http.StatusBadRequest
				}
				if response.StatusCode != want {
					t.Fatalf("%s: status %d, want %d", path, response.StatusCode, want)
				}
			}
		})
	}
}

func TestLocalAndQueuedRemoteShareSnapshotContract(t *testing.T) {
	input := collectGit(t)
	input.Metadata.Trigger, input.Metadata.RunID = "manual", ""
	store, err := reviewstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.User("local", "Local")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.NewWithContext(t.Context(), store, user)
	defer service.Close()
	submitted, err := service.Ingest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(serverapp.Handler(t.Context(), service, store, fstest.MapFS{}, submitted.Context.ID, serverapp.Options{}))
	defer local.Close()
	remote := start(t, ":memory:", true)
	plugin := input
	plugin.Metadata.Trigger, plugin.Metadata.Agent, plugin.Metadata.RunID = "agent-hook", "codex", "session-1"
	plugin.Metadata.AgentSession = &ingestion.AgentSession{Harness: "codex", ID: "session-1", Name: "Feature work"}
	receipt, err := ingestion.NewClient(remote.server.URL, remote.token).SyncTo(t.Context(), plugin, remote.owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		deployment deployment
		id         string
		input      ingestion.Request
	}{
		{deployment{server: local}, submitted.Context.ID, input},
		{remote, receipt.ContextID, plugin},
	} {
		context := decode[contextservice.Context](t, request(t, test.deployment, "GET", "/api/v2/contexts/"+test.id, nil))
		if context.Observation == nil || !reflect.DeepEqual(*context.Observation, test.input.Metadata) {
			t.Fatalf("checkout/session metadata diverged: %+v, want %+v", context.Observation, test.input.Metadata)
		}
		if !reflect.DeepEqual(context.Capabilities, submitted.Context.Capabilities) || context.Capabilities.Diff.Refresh.Enabled() {
			t.Fatalf("snapshot capabilities depend on mode: %+v", context.Capabilities)
		}
		active := decode[struct {
			Capabilities session.Capabilities `json:"capabilities"`
		}](t, request(t, test.deployment, "GET", "/api/v2/contexts/"+test.id+"/session", nil))
		if !reflect.DeepEqual(active.Capabilities, context.Capabilities) {
			t.Fatal("catalog/session capabilities diverged")
		}
		for _, scope := range input.Scopes {
			snapshot := decode[review.RepositoryDiff](t, request(t, test.deployment, "GET", "/api/v2/contexts/"+test.id+"/diffs/current?scope="+string(scope.Snapshot.Mode), nil))
			if snapshot.Revision != scope.Snapshot.Revision || snapshot.Branch != scope.Snapshot.Branch || !reflect.DeepEqual(snapshot.Files, scope.Snapshot.Files) {
				t.Fatalf("snapshot contract diverged: %+v", snapshot)
			}
		}
	}
}

func TestCollectionIngestsFreshObservationAndMovesMCPDefault(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if raw, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, raw)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	collections := 0
	var failure error
	collect := func(ctx context.Context) (ingestion.Request, error) {
		mu.Lock()
		defer mu.Unlock()
		if failure != nil {
			return ingestion.Request{}, failure
		}
		collections++
		return collector.Collect(ctx, root, collector.Options{SourceID: "source", SubmissionID: fmt.Sprintf("collection-%d", collections)})
	}
	initial, err := collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store, err := reviewstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.User("local", "Local")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.NewScoped(t.Context(), store, user, contextservice.ScopeFor(initial))
	defer service.Close()
	first, err := service.Ingest(t.Context(), initial)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(serverapp.Handler(t.Context(), service, store, fstest.MapFS{}, first.Context.ID, serverapp.Options{Collect: collect}))
	defer server.Close()
	d := deployment{server: server}
	mcpDiff := func() review.RepositoryDiff {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "collection-test", Version: "test"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: server.Client(), DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_diff", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("default MCP diff: %#v %v", result, err)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot review.RepositoryDiff
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	if mcpDiff().ID != first.Snapshot.ID {
		t.Fatal("bare MCP did not open the initial collection")
	}
	events, unsubscribe := service.Subscribe()
	defer unsubscribe()

	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	collected := decode[contextservice.Context](t, request(t, d, "POST", "/api/v2/collections", nil))
	if collected.ID == first.Context.ID || collected.Observation == nil || collected.Observation.Root != initial.Metadata.Root {
		t.Fatalf("collection did not create a fresh observation: %#v", collected)
	}
	select {
	case id := <-events:
		if id != collected.ID {
			t.Fatalf("event %s, want %s", id, collected.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("collection published no event")
	}
	snapshot := decode[review.RepositoryDiff](t, request(t, d, "GET", "/api/v2/contexts/"+collected.ID+"/diffs/current?scope=all", nil))
	if snapshot.ID == first.Snapshot.ID || mcpDiff().ID != snapshot.ID {
		t.Fatal("bare MCP default did not follow the collection")
	}
	page := decode[contextservice.Page](t, request(t, d, "GET", "/api/v2/contexts", nil))
	if len(page.Contexts) != 2 {
		t.Fatalf("catalog after collection: %#v", page)
	}

	mu.Lock()
	failure = errors.New("not a git checkout")
	mu.Unlock()
	response := request(t, d, "POST", "/api/v2/collections", nil)
	var problem struct {
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity || problem.Status != http.StatusUnprocessableEntity || !strings.Contains(problem.Detail, "not a git checkout") || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("collection failure: %d %#v", response.StatusCode, problem)
	}
	if count, err := service.Count(t.Context()); err != nil || count != 2 {
		t.Fatalf("failed collection changed the catalog: %d %v", count, err)
	}
	select {
	case id := <-events:
		t.Fatalf("failed collection published %s", id)
	default:
	}
	if mcpDiff().ID != snapshot.ID {
		t.Fatal("failed collection moved the MCP default")
	}
}
