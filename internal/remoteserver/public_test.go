package remoteserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

const publicInstallationID = 9

// publicGitHubAPI serves the public installation: octo/app is public with
// any pull request, octo/secret private and octo/huge over the size limit.
func publicGitHubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	reply := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
	}
	reply("/app/installations/9/access_tokens", `{"token":"public-installation","expires_at":"2099-01-01T00:00:00Z"}`)
	reply("/repos/octo/app", `{"id":10,"full_name":"octo/app","private":false,"visibility":"public","size":10}`)
	reply("/repos/octo/secret", `{"id":11,"full_name":"octo/secret","private":true,"visibility":"private","size":10}`)
	reply("/repos/octo/huge", `{"id":12,"full_name":"octo/huge","private":false,"visibility":"public","size":600000}`)
	mux.HandleFunc("/repos/octo/app/pulls/", func(w http.ResponseWriter, r *http.Request) {
		number := strings.TrimPrefix(r.URL.Path, "/repos/octo/app/pulls/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":` + number + `,"state":"open","title":"Add search","body":"","html_url":"https://github.com/octo/app/pull/` + number + `","user":{"id":7,"login":"mona"},
			"base":{"ref":"main","sha":"3333333333333333333333333333333333333333","repo":{"id":10,"full_name":"octo/app"}},
			"head":{"ref":"search","sha":"1111111111111111111111111111111111111111","repo":{"id":77,"full_name":"mona/app"}},
			"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T01:00:00Z"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// publicServer composes a server with the GitHub integration, a public
// installation and the setting on, running its tasks.
func publicServer(t *testing.T, header string, admission identityservice.Settings) (*pgstore.Store, http.Handler, http.Handler) {
	t.Helper()
	store := pgtest.Store(t)
	testsupport.PutSettings(t, store, admission)
	github := githubSettings(t, publicGitHubAPI(t).URL)
	github.FetcherSecret = strings.Repeat("s", 32)
	github.PublicInstallationID = publicInstallationID
	ctx, cancel := context.WithCancel(t.Context())
	handler, internal, closeServices, tasks, err := compose(ctx, store, fstest.MapFS{"index.html": {Data: []byte("app shell")}}, Settings{GitHub: github, PublicClientIPHeader: header})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, len(tasks))
	for _, task := range tasks {
		go func() { done <- task(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		for range tasks {
			<-done
		}
		closeServices()
	})
	return store, handler, internal
}

var publicOn = identityservice.Settings{Signup: true, PasswordSignup: true, PublicPullRequests: true}

func publicRequest(handler http.Handler, method, path, ip string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if ip != "" {
		r.Header.Set("X-Real-IP", ip)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func publicStatus(t *testing.T, w *httptest.ResponseRecorder) githubsync.PublicPullRequest {
	t.Helper()
	var status githubsync.PublicPullRequest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil {
		t.Fatalf("public pull request: %d %s", w.Code, w.Body.String())
	}
	return status
}

func TestAnonymousPublicPreviewIsFetchedOnceAndReadOnly(t *testing.T) {
	store, handler, internal := publicServer(t, "", publicOn)
	if body := identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", "").Body.String(); !strings.Contains(body, `"publicPullRequests":true`) {
		t.Fatalf("bootstrap: %s", body)
	}
	path := "/api/v2/public/pull-requests/octo/app/1"
	if w := publicRequest(handler, "GET", path, ""); w.Code != 404 {
		t.Fatalf("status before a request: %d", w.Code)
	}
	for _, refused := range []struct{ path, code string }{
		{"/api/v2/public/pull-requests/octo/secret/1", githubsync.FailureNotFound},
		{"/api/v2/public/pull-requests/octo/missing/1", githubsync.FailureNotFound},
		{"/api/v2/public/pull-requests/octo/huge/1", githubsync.FailureTooLarge},
	} {
		if status := publicStatus(t, publicRequest(handler, "PUT", refused.path, "")); status.Status != githubsync.PublicFailed || status.Failure == nil || status.Failure.Code != refused.code {
			t.Fatalf("%s: %+v", refused.path, status)
		}
	}
	for _, invalid := range []string{"/api/v2/public/pull-requests/-octo/app/1", "/api/v2/public/pull-requests/octo/app/0", "/api/v2/public/pull-requests/octo/app/one", "/api/v2/public/pull-requests/octo/a%20pp/1"} {
		if w := publicRequest(handler, "PUT", invalid, ""); w.Code != 400 {
			t.Fatalf("invalid %s: %d", invalid, w.Code)
		}
	}
	if status := publicStatus(t, publicRequest(handler, "PUT", path, "")); status.Status != githubsync.PublicQueued || status.PullRequest == nil || status.ContextID != nil {
		t.Fatalf("ensure: %+v", status)
	}
	// A signed-in visitor shares the same preview and needs no CSRF token.
	cookie, _ := memberSession(t, store, "alice")
	if w := identityRequest(handler, "PUT", path, "", cookie, "", ""); w.Code != 200 {
		t.Fatalf("signed-in ensure without CSRF: %d %s", w.Code, w.Body.String())
	}

	fetcher := func(path string, payload any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 32))
		w := httptest.NewRecorder()
		internal.ServeHTTP(w, r)
		return w
	}
	w := fetcher(fetchprotocol.LeasePath, nil)
	var job fetchprotocol.Job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil || !job.Public || job.Token != "" {
		t.Fatalf("public lease: %d %s", w.Code, w.Body.String())
	}
	snapshot := fetchedSnapshot(job)
	if w := fetcher(fetchprotocol.ResultPath, fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &snapshot}); w.Code != 204 {
		t.Fatalf("result: %d %s", w.Code, w.Body.String())
	}
	var ready githubsync.PublicPullRequest
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if ready = publicStatus(t, publicRequest(handler, "GET", path, "")); ready.Status == githubsync.PublicReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("preview never became ready: %+v", ready)
		}
	}
	if ready.ContextID == nil || ready.FetchedAt == nil || ready.FullName != "octo/app" {
		t.Fatalf("ready preview: %+v", ready)
	}
	contextPath := "/api/v2/public/contexts/" + *ready.ContextID
	for _, suffix := range []string{"", "/session", "/diffs/current?scope=all", "/commits"} {
		if w := publicRequest(handler, "GET", contextPath+suffix, ""); w.Code != 200 {
			t.Fatalf("read %s: %d %s", suffix, w.Code, w.Body.String())
		}
	}
	var session struct {
		Capabilities struct {
			Review struct {
				Comments struct{ State string } `json:"comments"`
			} `json:"review"`
		} `json:"capabilities"`
	}
	if w := publicRequest(handler, "GET", contextPath+"/session", ""); json.Unmarshal(w.Body.Bytes(), &session) != nil || session.Capabilities.Review.Comments.State != "disabled" {
		t.Fatalf("public session: %s", w.Body.String())
	}
	for _, refused := range []struct{ method, path string }{
		{"GET", contextPath + "/review/comments"}, {"POST", contextPath + "/review/comments"}, {"GET", contextPath + "/review-marks"},
		{"PUT", contextPath + "/review-marks/f1"}, {"DELETE", contextPath}, {"GET", "/api/v2/public/events"}, {"GET", "/api/v2/public/contexts"},
		{"GET", "/api/v2/public/mcp"}, {"GET", "/api/v2/public/contexts/" + *ready.ContextID + "/diffs/current/extra"},
	} {
		if w := publicRequest(handler, refused.method, refused.path, ""); w.Code != 404 {
			t.Fatalf("%s %s: %d", refused.method, refused.path, w.Code)
		}
	}
	// An account's context is not found through the public routes.
	owner := testsupport.RemoteAccount(t, store, "bob")
	binding, err := store.IngestContext(t.Context(), owner.ID, fetchedSnapshot(job))
	if err != nil {
		t.Fatal(err)
	}
	if w := publicRequest(handler, "GET", "/api/v2/public/contexts/"+binding.ContextID, ""); w.Code != 404 {
		t.Fatalf("account context through public routes: %d", w.Code)
	}
	// The ready preview is free to open again.
	if again := publicStatus(t, publicRequest(handler, "PUT", path, "")); again.Status != githubsync.PublicReady || *again.ContextID != *ready.ContextID {
		t.Fatalf("ensure ready preview: %+v", again)
	}
	if refreshed := publicStatus(t, publicRequest(handler, "POST", path+"/refresh", "")); refreshed.Status != githubsync.PublicReady {
		t.Fatalf("refresh within a minute: %+v", refreshed)
	}
}

func TestPublicRoutesAbsentUnlessConfiguredAndOn(t *testing.T) {
	_, off, _ := publicServer(t, "", identityservice.Settings{Signup: true, PasswordSignup: true})
	_, unconfigured := identityHandler(t, Settings{}, publicOn)
	store := pgtest.Store(t)
	testsupport.PutSettings(t, store, publicOn)
	withoutInstallation, closeServices, err := Handler(t.Context(), store, Settings{GitHub: githubSettings(t, publicGitHubAPI(t).URL)}, fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	for name, handler := range map[string]http.Handler{"setting off": off, "no GitHub": unconfigured, "no public installation": withoutInstallation} {
		if body := identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", "").Body.String(); !strings.Contains(body, `"publicPullRequests":false`) {
			t.Fatalf("%s bootstrap: %s", name, body)
		}
		for _, request := range []struct{ method, path string }{
			{"GET", "/api/v2/public/pull-requests/octo/app/1"}, {"PUT", "/api/v2/public/pull-requests/octo/app/1"},
			{"POST", "/api/v2/public/pull-requests/octo/app/1/refresh"}, {"GET", "/api/v2/public/contexts/any"},
		} {
			if w := publicRequest(handler, request.method, request.path, ""); w.Code != 404 {
				t.Fatalf("%s %s %s: %d", name, request.method, request.path, w.Code)
			}
		}
	}
}

func TestPublicFetchesAreLimitedPerClientOrServerWide(t *testing.T) {
	ensure := func(handler http.Handler, number int, ip string) *httptest.ResponseRecorder {
		return publicRequest(handler, "PUT", "/api/v2/public/pull-requests/octo/app/"+strconv.Itoa(number), ip)
	}
	retryAfter := func(w *httptest.ResponseRecorder) {
		t.Helper()
		seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
		if w.Code != 429 || err != nil || seconds < 1 || seconds > 3600 {
			t.Fatalf("limited fetch: %d Retry-After %q", w.Code, w.Header().Get("Retry-After"))
		}
	}
	_, perClient, _ := publicServer(t, "X-Real-IP", publicOn)
	for number := 1; number <= 5; number++ {
		if w := ensure(perClient, number, "192.0.2.1"); w.Code != 200 {
			t.Fatalf("fetch %d: %d %s", number, w.Code, w.Body.String())
		}
	}
	retryAfter(ensure(perClient, 6, "192.0.2.1"))
	// Joining a pending fetch costs nothing; other clients keep their budget.
	if w := ensure(perClient, 1, "192.0.2.1"); w.Code != 200 {
		t.Fatalf("join over the limit: %d", w.Code)
	}
	if w := ensure(perClient, 7, "192.0.2.2"); w.Code != 200 {
		t.Fatalf("other client: %d", w.Code)
	}
	if w := ensure(perClient, 8, ""); w.Code != 400 {
		t.Fatalf("missing client IP header: %d", w.Code)
	}

	_, global, _ := publicServer(t, "", publicOn)
	for number := 1; number <= 30; number++ {
		if w := ensure(global, number, "192.0.2."+strconv.Itoa(number)); w.Code != 200 {
			t.Fatalf("global fetch %d: %d %s", number, w.Code, w.Body.String())
		}
	}
	retryAfter(ensure(global, 31, "198.51.100.1"))
}

func TestPullRequestPagesServeTheApp(t *testing.T) {
	_, handler := identityHandler(t, Settings{})
	for _, path := range []string{"/octo/app/pull/1", "/vercel/next.js/pull/12", "/vercel/next.js/pull/12/files", "/mcp-tools/app/pull/3"} {
		w := identityRequest(handler, "GET", path, "", nil, "", "")
		if w.Code != 200 || w.Body.String() != "app shell" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
