package serverapp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/mcpapi"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewservice"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deployment struct {
	server *httptest.Server
	store  *pgstore.Store
	owner  string
	token  string
	close  func()
}

// start composes an unauthenticated queued deployment or an authenticated
// remote deployment on a Postgres URL ("" for a fresh database). Only server
// state has a durable ingestion queue, so both run on Postgres.
func start(t *testing.T, state string, remote bool) deployment {
	t.Helper()
	assets := fstest.MapFS{"index.html": {Data: []byte("stored review UI")}}
	if state == "" {
		state = pgtest.URL(t)
	}
	store, err := pgstore.Open(t.Context(), state, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	identity := identityservice.New(store)
	accounts, err := identity.Accounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var account identityservice.Account
	if len(accounts) == 0 {
		account = testsupport.RemoteAccount(t, store, "team")
	} else {
		for _, existing := range accounts {
			if existing.Username == "team" {
				account = existing
				break
			}
		}
		if account.ID == "" {
			t.Fatal("persisted deployment lost team account")
		}
	}
	var handler http.Handler
	var closeService func() error
	credential := ""
	if remote {
		created, err := identity.CreateToken(t.Context(), account.ID, "boundary tests", identityservice.PurposeAPI, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		credential = created.Secret
		handler, closeService, err = remoteserver.Handler(t.Context(), store, remoteserver.Settings{}, assets)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		service := contextservice.NewWithContext(t.Context(), store, reviewdata.User{ID: account.ID, Name: account.Username})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- ingestionqueue.Run(ctx, store.Queue(), func(ctx context.Context, _ string, input ingestion.Request) (string, error) {
				result, err := service.Ingest(ctx, input)
				return result.Context.ID, err
			})
		}()
		handler = serverapp.Handler(ctx, service, store, assets, "", serverapp.Options{Queue: store.Queue()})
		closeService = func() error { cancel(); return errors.Join(<-done, service.Close()) }
	}
	server := httptest.NewServer(handler)
	var once sync.Once
	close := func() { once.Do(func() { server.Close(); _ = closeService(); _ = store.Close() }) }
	t.Cleanup(close)
	return deployment{server, store, account.ID, credential, close}
}

// principal creates another account in the deployment.
func principal(t *testing.T, d deployment, name string) reviewdata.User {
	t.Helper()
	account := testsupport.RemoteAccount(t, d.store, name)
	return reviewdata.User{ID: account.ID, Name: account.Username}
}

func request(t *testing.T, d deployment, method, path string, body io.Reader) *http.Response {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), method, d.server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if d.token != "" {
		r.Header.Set("Authorization", "Bearer "+d.token)
	}
	response, err := d.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decode[T any](t *testing.T, response *http.Response) T {
	t.Helper()
	defer response.Body.Close()
	var result T
	if response.StatusCode != 200 {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("query status %d: %s", response.StatusCode, raw)
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func collectGit(t *testing.T) ingestion.Request {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, raw)
		}
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/shared")
	git("remote", "add", "origin", "https://example.com/acme/project.git")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := collector.Collect(t.Context(), root, collector.Options{SourceID: "container-a", Hostname: "sandbox", RunID: "job-a", Trigger: "agent-hook", SubmissionID: "turn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	return input
}

// The public protocol, rather than a storage shortcut, carries producer data
// into both deployment compositions. Neither checkout nor Git exists at query time.
func TestPublicIngestionSurvivesProducerRemovalAndServerRestart(t *testing.T) {
	input := collectGit(t)
	t.Setenv("PATH", t.TempDir())
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
			state := pgtest.URL(t)
			d := start(t, state, remote)
			client := testsupport.NewClient(d.server.URL, d.token)
			first, err := client.Submit(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			retry, err := client.Submit(t.Context(), input)
			if err != nil || !reflect.DeepEqual(first, retry) {
				t.Fatalf("retry receipt changed: %v %#v %#v", err, first, retry)
			}
			changed := input
			changed.Metadata.Hostname = "different"
			_, err = client.Submit(t.Context(), changed)
			var problem *ingestion.Problem
			if !errors.As(err, &problem) || problem.Status != 409 {
				t.Fatalf("conflicting retry: %v", err)
			}
			other := input
			other.Metadata.SourceID, other.Metadata.RunID = "container-b", "job-b"
			other.Metadata.Trigger = "manual"
			second, err := client.Submit(t.Context(), other)
			if err != nil {
				t.Fatal(err)
			}
			if first.ContextID == second.ContextID {
				t.Fatal("independent same-branch sources collapsed")
			}
			if _, err := d.store.ObservationSnapshot(t.Context(), d.owner, first.ContextID, review.DiffAll); err != nil {
				t.Fatalf("snapshot not owned by server principal: %v", err)
			}
			foreign := principal(t, d, "other").ID
			if _, err := d.store.ObservationSnapshot(t.Context(), foreign, first.ContextID, review.DiffAll); !errors.Is(err, reviewdata.ErrNotFound) {
				t.Fatalf("cross-account observation exposed: %v", err)
			}
			file := first.Snapshot.Files[0]
			comment := review.ReviewComment{ID: "retained-comment", DiffID: first.Snapshot.ID, VersionID: first.Snapshot.VersionID, Path: file.Path, Scope: review.DiffAll, Fingerprint: file.Fingerprint, Side: "additions", Start: 1, End: 1, Code: "after", Body: "Check this change", Status: "open", CreatedAt: 1}
			if err := d.store.PutComment(t.Context(), first.ContextID, comment); err != nil {
				t.Fatal(err)
			}
			fresh := input
			fresh.SubmissionID = "turn-2"
			fresh.Metadata.CollectedAt++
			reused, err := client.Submit(t.Context(), fresh)
			if err != nil || !reflect.DeepEqual(first, reused) {
				t.Fatalf("fresh unchanged review duplicated: %v %#v %#v", err, first, reused)
			}
			d.close()
			d = start(t, state, remote)
			page := decode[contextservice.Page](t, request(t, d, "GET", "/api/v2/contexts?repository=project&branch=shared", nil))
			if len(page.Contexts) != 2 {
				t.Fatalf("stored source catalog: %#v", page)
			}
			filtered := decode[contextservice.Page](t, request(t, d, "GET", "/api/v2/contexts?sourceId=container-b&runId=job-b&hostname=sandbox", nil))
			if len(filtered.Contexts) != 1 || filtered.Contexts[0].ID != second.ContextID {
				t.Fatalf("source filter: %#v", filtered)
			}
			base := "/api/v2/contexts/" + first.ContextID
			for _, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged, review.DiffUnstaged} {
				snapshot := decode[review.RepositoryDiff](t, request(t, d, "GET", base+"/diffs/current?scope="+string(mode), nil))
				if snapshot.Mode != mode || snapshot.VersionID == "" {
					t.Fatalf("stored scope: %#v", snapshot)
				}
				version := decode[review.RepositoryDiff](t, request(t, d, "GET", base+"/diffs/"+snapshot.ID+"/versions/"+snapshot.VersionID, nil))
				if !reflect.DeepEqual(version, snapshot) {
					t.Fatalf("immutable version changed: %#v %#v", snapshot, version)
				}
				for _, file := range snapshot.Files {
					fileBase := base + "/diffs/" + snapshot.ID + "/files/" + file.ID
					query := "?scope=" + string(mode) + "&versionId=" + snapshot.VersionID + "&fileVersion=" + file.Fingerprint
					patch := decode[review.FilePatch](t, request(t, d, "GET", fileBase+"/patch"+query, nil))
					if !strings.Contains(patch.Patch, "+after") {
						t.Fatalf("stored patch: %#v", patch)
					}
					contents := decode[review.FileContents](t, request(t, d, "GET", fileBase+"/contents"+query, nil))
					if contents.Before != "before\n" || contents.After != "after\n" {
						t.Fatalf("stored contents: %#v", contents)
					}
				}
			}
			mcpClient := mcp.NewClient(&mcp.Implementation{Name: "stored-observation-test", Version: "test"}, nil)
			httpClient := &http.Client{Transport: authorizedTransport{base: d.server.Client().Transport, token: d.token}}
			mcpSession, err := mcpClient.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.server.URL + "/mcp/contexts/" + first.ContextID, HTTPClient: httpClient, DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
			if err != nil {
				t.Fatal(err)
			}
			defer mcpSession.Close()
			result, err := mcpSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_review_comments", Arguments: map[string]bool{}})
			if err != nil || result.IsError {
				t.Fatalf("stored MCP comment query: %#v %v", result, err)
			}
			raw, err := json.Marshal(result.StructuredContent)
			if err != nil || !strings.Contains(string(raw), "retained-comment") || !strings.Contains(string(raw), "Check this change") {
				t.Fatalf("retained MCP comments: %s %v", raw, err)
			}
		})
	}
}

type authorizedTransport struct {
	base  http.RoundTripper
	token string
}

func (transport authorizedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	if transport.token != "" {
		request.Header.Set("Authorization", "Bearer "+transport.token)
	}
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

func emptyRequest() ingestion.Request {
	return ingestion.Request{ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: "one", Metadata: ingestion.Metadata{SourceID: "source", Trigger: "manual", CollectedAt: 1, RepositoryKey: "repo", CheckoutKey: "checkout"}, Scopes: []ingestion.Scope{{Snapshot: review.RepositoryDiff{Source: "local", Mode: review.DiffAll, Revision: "revision", Files: []review.ChangedFile{}}, Patches: map[string]review.FilePatch{}}}}
}

func TestIngestionHTTPRejectsMalformedAndAmbiguousInput(t *testing.T) {
	d := start(t, "", false)
	raw, err := json.Marshal(emptyRequest())
	if err != nil {
		t.Fatal(err)
	}
	unsupported := emptyRequest()
	unsupported.ProtocolVersion++
	unsupportedRaw, err := json.Marshal(unsupported)
	if err != nil {
		t.Fatal(err)
	}
	reserved := emptyRequest()
	reserved.Metadata.SourceID = ingestion.GitHubSourceID
	reservedRaw, err := json.Marshal(reserved)
	if err != nil {
		t.Fatal(err)
	}
	pull := emptyRequest()
	head := strings.Repeat("c", 40)
	pull.Metadata.SourceID, pull.Metadata.Trigger, pull.Metadata.Branch, pull.Metadata.Head = ingestion.GitHubSourceID, ingestion.GitHubTrigger, "feature", &head
	pull.Scopes[0].Snapshot.Branch, pull.Scopes[0].Snapshot.Head = "feature", &head
	pull.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
	pull.Metadata.PullRequest = &ingestion.PullRequest{Provider: "github", RepositoryID: 7, FullName: "octo/app", Number: 12, URL: "https://github.com/octo/app/pull/12",
		Title: "Spoofed", State: "open", Author: ingestion.PullRequestAuthor{Login: "mona", URL: "https://github.com/mona"}}
	if err := ingestion.Validate(pull); err != nil {
		t.Fatalf("fixture must be otherwise valid: %v", err)
	}
	pullRaw, err := json.Marshal(pull)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, contentType, body string
		status                          int
	}{
		{"wrong method", "GET", "application/json", "", 405},
		{"wrong media", "POST", "text/plain", string(raw), 415},
		{"malformed", "POST", "application/json", `{"`, 400},
		{"unknown identity", "POST", "application/json", `{"userId":"other"}`, 400},
		{"unknown nested field", "POST", "application/json", strings.Replace(string(raw), `"sourceId":`, `"userId":"other","sourceId":`, 1), 400},
		{"trailing JSON", "POST", "application/json", string(raw) + ` {}`, 400},
		{"trailing garbage", "POST", "application/json", string(raw) + ` x`, 400},
		{"unsupported protocol", "POST", "application/json", string(unsupportedRaw), 400},
		{"reserved github source", "POST", "application/json", string(reservedRaw), 400},
		{"producer-claimed pull request", "POST", "application/json", string(pullRaw), 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/v2/ingestion-jobs", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			d.server.Config.Handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	// One giant field tests the body bound rather than merely a validation bound.
	oversize := io.MultiReader(strings.NewReader(`{"submissionId":"`), strings.NewReader(strings.Repeat("x", ingestion.MaxRequestBytes)), strings.NewReader(`"}`))
	r := httptest.NewRequest("POST", "/api/v2/ingestion-jobs", oversize)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.server.Config.Handler.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("oversized body status %d", w.Code)
	}
	page := decode[contextservice.Page](t, request(t, d, "GET", "/api/v2/contexts", nil))
	if len(page.Contexts) != 0 {
		t.Fatal("rejected input created observations")
	}
}

func TestSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin, fetch string
		allow         bool
	}{
		{"", "", true}, {"http://reviews.example", "same-origin", true}, {"https://reviews.example", "same-origin", true},
		{"https://other.example", "", false}, {"https://reviews.example.evil", "", false}, {"", "cross-site", false}, {"https://reviews.example", "cross-site", false},
	} {
		r := httptest.NewRequest("POST", "http://reviews.example/api/v2/ingestion-jobs", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetch)
		if got := serverapp.SameOrigin(r); got != tc.allow {
			t.Errorf("origin=%q fetch=%q: %v", tc.origin, tc.fetch, got)
		}
	}
}

func TestIngestionNotificationReferencesCommittedObservation(t *testing.T) {
	d := start(t, "", false)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", d.server.URL+"/api/v2/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := d.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("SSE connected: %q %v", line, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	receipt, err := testsupport.NewClient(d.server.URL, "").Submit(t.Context(), emptyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "event: ingestion\n" {
		t.Fatalf("SSE event: %q %v", line, err)
	}
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, receipt.ContextID) {
		t.Fatalf("SSE context: %q %v", line, err)
	}
	if _, err := d.store.ObservationSnapshot(t.Context(), d.owner, receipt.ContextID, review.DiffAll); err != nil {
		t.Fatalf("notification before commit: %v", err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	// Notifications are advisory; reconnecting clients recover the catalog via GET.
	page := decode[contextservice.Page](t, request(t, d, "GET", "/api/v2/contexts", nil))
	if len(page.Contexts) != 1 || page.Contexts[0].ID != receipt.ContextID {
		t.Fatalf("catalog recovery: %#v", page)
	}
}

func TestCrossOriginSubmissionCannotCommit(t *testing.T) {
	d := start(t, "", false)
	raw, err := json.Marshal(emptyRequest())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://reviews.example/api/v2/ingestion-jobs", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	d.server.Config.Handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin submission: %d", w.Code)
	}
}

func TestGlobalMCPDiscoversStoredReviewsAndRejectsForeignContexts(t *testing.T) {
	input := collectGit(t) // Producer checkout removed before queries.
	t.Setenv("PATH", t.TempDir())
	d := start(t, "", false)
	client := testsupport.NewClient(d.server.URL, "")
	first, err := client.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Metadata.SourceID = "second-source"
	second, err := client.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	foreign := principal(t, d, "foreign")
	foreignService := contextservice.New(d.store, foreign)
	defer foreignService.Close()
	foreignSubmission, err := foreignService.Ingest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	connect := func(path string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "global-test", Version: "test"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.server.URL + path, HTTPClient: d.server.Client(), DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	session := connect("/mcp")
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 6 {
		t.Fatalf("global tools: %#v", tools)
	}
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: %#v content: %#v", name, result, result.Content[0])
		}
		return result
	}
	result := call("list_contexts", map[string]any{}, false)
	var page contextservice.Page
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Contexts) != 2 {
		t.Fatalf("owner catalog: %#v", page)
	}
	for _, item := range page.Contexts {
		if item.ID != first.ContextID && item.ID != second.ContextID {
			t.Fatalf("foreign catalog item: %#v", item)
		}
	}
	result = call("get_diff", map[string]any{"context_id": first.ContextID}, false)
	var snapshot review.RepositoryDiff
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != first.Snapshot.ID || len(snapshot.Files) != 1 {
		t.Fatalf("stored diff: %#v", snapshot)
	}
	result = call("get_file_patch", map[string]any{"context_id": first.ContextID, "file_id": snapshot.Files[0].ID}, false)
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil || !strings.Contains(string(raw), "+after") {
		t.Fatalf("stored patch: %s %v", raw, err)
	}
	call("get_review_comments", map[string]any{}, true)
	call("get_review_comments", map[string]any{"context_id": first.ContextID}, false)
	call("get_diff", map[string]any{"context_id": foreignSubmission.Context.ID}, true)
	call("get_review_comments", map[string]any{"context_id": foreignSubmission.Context.ID}, true)
	call("resolve_review_comment", map[string]any{"context_id": foreignSubmission.Context.ID, "comment_id": "missing"}, true)
	call("get_file_patch", map[string]any{"context_id": foreignSubmission.Context.ID, "file_id": snapshot.Files[0].ID}, true)
	scoped := connect("/mcp/contexts/" + first.ContextID)
	result, err = scoped.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_diff", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("scoped stored diff: %#v %v", result, err)
	}
	result, err = scoped.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_review_comments", Arguments: map[string]any{"context_id": second.ContextID}})
	if err != nil || !result.IsError {
		t.Fatalf("scoped context override: %#v %v", result, err)
	}
}

func TestMCPReadsCapturedCommitsWithoutCheckout(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, raw)
		}
	}
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "--initial-branch=main")
	write("before\n")
	git("add", ".")
	git("commit", "-m", "initial")
	git("switch", "--create", "feature")
	write("first\n")
	git("commit", "-am", "First change")
	write("second\n")
	git("commit", "-am", "Second change")
	input, err := collector.Collect(t.Context(), root, collector.Options{SourceID: "container-a", Hostname: "sandbox", Trigger: "manual", SubmissionID: "turn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	d := start(t, "", false)
	submitted, err := testsupport.NewClient(d.server.URL, "").Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "commit-test", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.server.URL + "/mcp", HTTPClient: d.server.Client(), DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any, target any) bool {
		t.Helper()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			return false
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
		return true
	}
	id := submitted.ContextID
	var commits reviewservice.CommitList
	if !call("list_commits", map[string]any{"context_id": id}, &commits) || len(commits.Commits) != 2 || commits.Commits[0].Subject != "First change" {
		t.Fatalf("list_commits: %+v", commits)
	}
	first := commits.Commits[0].OID
	var snapshot review.RepositoryDiff
	if !call("get_diff", map[string]any{"context_id": id, "commit": first}, &snapshot) || snapshot.Mode != review.DiffCommit || len(snapshot.Files) != 1 {
		t.Fatalf("commit diff: %+v", snapshot)
	}
	var patch review.FilePatch
	if !call("get_file_patch", map[string]any{"context_id": id, "commit": first, "file_id": snapshot.Files[0].ID}, &patch) || !strings.Contains(patch.Patch, "+first") || strings.Contains(patch.Patch, "+second") {
		t.Fatalf("commit patch: %+v", patch)
	}
	if call("get_diff", map[string]any{"context_id": id, "commit": first, "scope": "all"}, &snapshot) {
		t.Fatal("commit and scope combined")
	}
	if call("get_diff", map[string]any{"context_id": id, "commit": strings.Repeat("0", 40)}, &snapshot) {
		t.Fatal("uncaptured commit read")
	}
}

func TestForeignRESTMutationsLeaveReviewStateUntouched(t *testing.T) {
	input := collectGit(t)
	d := start(t, "", false)
	owner := principal(t, d, "foreign")
	service := contextservice.New(d.store, owner)
	defer service.Close()
	foreign, err := service.Ingest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, file := foreign.Snapshot, foreign.Snapshot.Files[0]
	original := review.ReviewComment{ID: "foreign-comment", DiffID: snapshot.ID, VersionID: snapshot.VersionID, Path: file.Path, Scope: review.DiffAll, Fingerprint: file.Fingerprint, Side: "additions", Start: 1, End: 1, Body: "Keep this concern", Status: "open", CreatedAt: 1}
	if err := d.store.PutComment(t.Context(), foreign.Context.ID, original); err != nil {
		t.Fatal(err)
	}
	mark := review.ReviewMark{DiffID: snapshot.ID, VersionID: snapshot.VersionID, FileID: file.ID, FileVersion: file.Fingerprint, Scope: review.DiffAll}
	if err := d.store.PutMark(t.Context(), foreign.Context.ID, mark); err != nil {
		t.Fatal(err)
	}
	create := map[string]any{"diffId": snapshot.ID, "versionId": snapshot.VersionID, "fileId": file.ID, "fileVersion": file.Fingerprint, "scope": "all", "side": "additions", "start": 1, "end": 1, "body": "New concern"}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/comments", create},
		{"PATCH", "/comments/foreign-comment", map[string]string{"body": "Modified", "status": "resolved"}},
		{"DELETE", "/comments/foreign-comment", nil},
		{"DELETE", "/comments?status=all", nil},
		{"POST", "/comments/import", map[string]any{"comments": []review.ReviewComment{original}}},
		{"POST", "/review/comments/foreign-comment/resolve", map[string]any{}},
		{"POST", "/comments/foreign-comment/carry", nil},
		{"PUT", "/review-marks/" + file.ID + "?scope=all", map[string]string{"versionId": snapshot.VersionID, "fileVersion": file.Fingerprint}},
		{"DELETE", "/review-marks/" + file.ID + "?scope=all", nil},
		{"DELETE", "/review-marks?scope=all", nil},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			var body io.Reader
			if tc.body != nil {
				raw, err := json.Marshal(tc.body)
				if err != nil {
					t.Fatal(err)
				}
				body = bytes.NewReader(raw)
			}
			response := request(t, d, tc.method, "/api/v2/contexts/"+foreign.Context.ID+tc.path, body)
			response.Body.Close()
			if response.StatusCode != 404 {
				t.Fatalf("foreign mutation status: %d", response.StatusCode)
			}
		})
	}
	comments, err := d.store.Comments(t.Context(), foreign.Context.ID)
	if err != nil || !reflect.DeepEqual(comments, []review.ReviewComment{original}) {
		t.Fatalf("foreign comments changed: %#v %v", comments, err)
	}
	marks, err := d.store.Marks(t.Context(), foreign.Context.ID, review.DiffAll)
	if err != nil || !reflect.DeepEqual(marks, []review.ReviewMark{mark}) {
		t.Fatalf("foreign marks changed: %#v %v", marks, err)
	}
}

// Review state belongs to the checkout stream: later snapshots re-anchor its
// comments, carry unchanged marks, and keep both after an older snapshot is deleted.
func TestStreamReviewSpansSnapshotsInBothCompositions(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if raw, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, raw)
		}
	}
	write := func(files map[string]string) {
		t.Helper()
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	collect := func(submission string) ingestion.Request {
		t.Helper()
		input, err := collector.Collect(t.Context(), root, collector.Options{SourceID: "container-a", SubmissionID: submission})
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/shared")
	git("remote", "add", "origin", "https://example.com/acme/stream.git")
	write(map[string]string{"file.txt": "alpha\nbeta\n", "other.txt": "keep\n", "same.txt": "base\n"})
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	write(map[string]string{"file.txt": "alpha\nadded\nbeta\n", "other.txt": "keep\nold concern\n", "same.txt": "base\nsame change\n"})
	first := collect("turn-1")
	write(map[string]string{"file.txt": "inserted\nalpha\nadded\nbeta\n", "other.txt": "keep\nnew code\n"})
	second := collect("turn-2")
	second.Metadata.CollectedAt = first.Metadata.CollectedAt + 1
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
			d := start(t, "", remote)
			client := testsupport.NewClient(d.server.URL, d.token)
			older, err := client.Submit(t.Context(), first)
			if err != nil {
				t.Fatal(err)
			}
			later, err := client.Submit(t.Context(), second)
			if err != nil || later.ContextID == older.ContextID {
				t.Fatalf("second snapshot: %#v %v", later, err)
			}
			call := func(method, path string, body any, status int, result any) {
				t.Helper()
				var reader io.Reader
				if body != nil {
					raw, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					reader = bytes.NewReader(raw)
				}
				response := request(t, d, method, path, reader)
				defer response.Body.Close()
				if response.StatusCode != status {
					raw, _ := io.ReadAll(response.Body)
					t.Fatalf("%s %s status %d: %s", method, path, response.StatusCode, raw)
				}
				if result != nil {
					if err := json.NewDecoder(response.Body).Decode(result); err != nil {
						t.Fatal(err)
					}
				}
			}
			file := func(snapshot review.RepositoryDiff, path string) review.ChangedFile {
				t.Helper()
				for _, candidate := range snapshot.Files {
					if candidate.Path == path {
						return candidate
					}
				}
				t.Fatalf("%s missing from %#v", path, snapshot.Files)
				return review.ChangedFile{}
			}
			olderBase, laterBase := "/api/v2/contexts/"+older.ContextID, "/api/v2/contexts/"+later.ContextID
			ids := map[string]string{}
			for _, path := range []string{"file.txt", "other.txt"} {
				changed := file(older.Snapshot, path)
				var created review.ReviewComment
				call("POST", olderBase+"/comments", map[string]any{"diffId": older.Snapshot.ID, "versionId": older.Snapshot.VersionID, "fileId": changed.ID, "fileVersion": changed.Fingerprint, "scope": "all", "side": "additions", "start": 2, "end": 2, "body": "Check " + path}, 201, &created)
				if created.Anchor == nil || created.Placement == nil || created.Placement.Applicability != review.Anchored {
					t.Fatalf("created comment: %#v", created)
				}
				ids[path] = created.ID
			}
			for _, path := range []string{"file.txt", "same.txt"} {
				marked := file(older.Snapshot, path)
				call("PUT", olderBase+"/review-marks/"+marked.ID+"?scope=all", map[string]string{"versionId": older.Snapshot.VersionID, "fileVersion": marked.Fingerprint}, 200, nil)
			}
			var marks struct{ Marks []review.ReviewMark }
			call("GET", laterBase+"/review-marks?scope=all", nil, 200, &marks)
			same := file(later.Snapshot, "same.txt")
			if want := (review.ReviewMark{DiffID: later.Snapshot.ID, VersionID: later.Snapshot.VersionID, FileID: same.ID, FileVersion: same.Fingerprint, Scope: review.DiffAll}); len(marks.Marks) != 1 || marks.Marks[0] != want {
				t.Fatalf("marks carried to later snapshot: %#v", marks.Marks)
			}
			call("DELETE", olderBase, nil, 204, nil)
			var listed struct{ Comments []review.ReviewComment }
			call("GET", laterBase+"/comments", nil, 200, &listed)
			placements := map[string]review.CommentPlacement{}
			for _, comment := range listed.Comments {
				placements[comment.ID] = *comment.Placement
			}
			if placements[ids["file.txt"]] != (review.CommentPlacement{Applicability: review.Moved, Side: "additions", Start: 3, End: 3}) ||
				placements[ids["other.txt"]] != (review.CommentPlacement{Applicability: review.Outdated, Side: "additions", Start: 2, End: 2}) {
				t.Fatalf("stream comments after deleting their snapshot: %#v", listed.Comments)
			}
			call("POST", laterBase+"/comments/"+ids["file.txt"]+"/carry", nil, 409, nil)
			var carried review.ReviewComment
			call("POST", laterBase+"/comments/"+ids["other.txt"]+"/carry", nil, 200, &carried)
			if carried.Target != "file" || carried.DiffID != later.Snapshot.ID || carried.CarriedFrom == nil || carried.CarriedFrom.Start != 2 || carried.Code != "+ old concern" || carried.Placement.Applicability != review.Anchored {
				t.Fatalf("carried comment: %#v", carried)
			}
			var agent struct {
				Comments []struct {
					ID            string
					Applicability review.Applicability
					Actionable    bool
				}
			}
			call("GET", laterBase+"/review/comments", nil, 200, &agent)
			if len(agent.Comments) != 2 || !agent.Comments[0].Actionable || !agent.Comments[1].Actionable {
				t.Fatalf("agent comments: %#v", agent.Comments)
			}
			call("DELETE", laterBase, nil, 204, nil)
			if comments, err := d.store.Comments(t.Context(), later.ContextID); err != nil || len(comments) != 0 {
				t.Fatalf("stream end kept comments: %#v %v", comments, err)
			}
		})
	}
}
