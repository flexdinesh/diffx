package remoteserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

const testPassword = "test-password-at-least-twelve-bytes"

func provisionToken(t *testing.T, store *reviewstore.Store, name string, purpose identityservice.Purpose) string {
	t.Helper()
	identity := identityservice.New(store)
	account, err := identity.Provision(t.Context(), name, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	token, err := identity.CreateToken(t.Context(), account.ID, "test", purpose, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return token.Secret
}

func TestRemoteAuthenticationCoversUIAPIAndMCP(t *testing.T) {
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := provisionToken(t, store, "account", identityservice.PurposeAPI)
	handler, closeService, err := Handler(t.Context(), store, Settings{}, fstest.MapFS{"index.html": {Data: []byte("review dashboard")}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeService()
	for _, p := range []string{"/", "/settings", "/api/v2/bootstrap"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 {
			t.Fatalf("public %s: %d", p, w.Code)
		}
	}
	for _, p := range []string{"/api/v2/contexts", "/mcp", "/api/v2/events", "/api/v2/ingestion-jobs/unknown"} {
		for _, auth := range []string{"", "Bearer wrong", "bearer " + token} {
			r := httptest.NewRequest("GET", p, nil)
			r.Header.Set("Authorization", auth)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("unauthenticated %s: %d", p, w.Code)
			}
		}
	}
	r := httptest.NewRequest("GET", "/api/v2/contexts", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"contexts":[]`) {
		t.Fatalf("authorized catalog: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentialed cache/referrer headers missing")
	}
	r = httptest.NewRequest("GET", "/api/v2/contexts", nil)
	r.SetBasicAuth("account", token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("retired Basic auth accepted")
	}
}

func TestMultiUserRESTMCPAndEventIsolation(t *testing.T) {
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	testToken := provisionToken(t, store, "admin", identityservice.PurposeAPI)
	otherToken := provisionToken(t, store, "other", identityservice.PurposeAPI)
	handler, closeServices, err := MultiHandler(t.Context(), store, fstest.MapFS{"index.html": {Data: []byte("reviews")}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeServices()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	request := func(token, method, path string, body io.Reader) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	adminEvents := request(testToken, "GET", "/api/v2/events", nil)
	defer adminEvents.Body.Close()
	otherEvents := request(otherToken, "GET", "/api/v2/events", nil)
	defer otherEvents.Body.Close()
	if adminEvents.StatusCode != 200 || otherEvents.StatusCode != 200 {
		t.Fatal("event subscription failed")
	}
	adminReader, otherReader := bufio.NewReader(adminEvents.Body), bufio.NewReader(otherEvents.Body)
	for _, reader := range []*bufio.Reader{adminReader, otherReader} {
		if line, err := reader.ReadString('\n'); err != nil || line != ": connected\n" {
			t.Fatalf("event connection: %q %v", line, err)
		}
	}
	patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n"
	input, err := collector.CollectPatch(t.Context(), patch, t.TempDir(), collector.Options{SourceID: "shared-source", SubmissionID: "same-submission"})
	if err != nil {
		t.Fatal(err)
	}
	adminReceipt, err := testsupport.NewClient(server.URL, testToken).Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	otherReceipt, err := testsupport.NewClient(server.URL, otherToken).Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if adminReceipt.ContextID == otherReceipt.ContextID {
		t.Fatal("review deduplication crossed user boundary")
	}
	// Queue admission and polling use the same authenticated owner boundary.
	rawInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	accepted := request(testToken, "POST", "/api/v2/ingestion-jobs", bytes.NewReader(rawInput))
	var job ingestion.Job
	err = json.NewDecoder(accepted.Body).Decode(&job)
	accepted.Body.Close()
	if err != nil || accepted.StatusCode != http.StatusAccepted || job.ContextID != adminReceipt.ContextID {
		t.Fatalf("queue admission: %d %+v %v", accepted.StatusCode, job, err)
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{testToken, http.StatusOK}, {otherToken, http.StatusNotFound}} {
		response := request(tc.token, "GET", "/api/v2/ingestion-jobs/"+job.ID, nil)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("job owner boundary: %d, want %d", response.StatusCode, tc.status)
		}
	}
	stale := httptest.NewRequest("GET", "/api/v2/ingestion-jobs/"+job.ID, nil)
	stale.Header.Set("Authorization", "Bearer "+testToken)
	stale.Header.Set("X-Diffx-State", "replaced-database")
	staleResult := httptest.NewRecorder()
	handler.ServeHTTP(staleResult, stale)
	if staleResult.Code != http.StatusConflict {
		t.Fatalf("job database identity: %d", staleResult.Code)
	}
	for _, tc := range []struct {
		reader              *bufio.Reader
		expected, forbidden string
	}{
		{adminReader, adminReceipt.ContextID, otherReceipt.ContextID},
		{otherReader, otherReceipt.ContextID, adminReceipt.ContextID},
	} {
		for {
			line, err := tc.reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data:") {
				if !strings.Contains(line, tc.expected) || strings.Contains(line, tc.forbidden) {
					t.Fatalf("foreign event: %q", line)
				}
				break
			}
		}
	}
	for _, tc := range []struct{ token, own, foreign string }{
		{testToken, adminReceipt.ContextID, otherReceipt.ContextID},
		{otherToken, otherReceipt.ContextID, adminReceipt.ContextID},
	} {
		response := request(tc.token, "GET", "/api/v2/contexts", nil)
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !strings.Contains(string(raw), tc.own) || strings.Contains(string(raw), tc.foreign) {
			t.Fatalf("catalog ownership: %d %s %v", response.StatusCode, raw, err)
		}
		for _, suffix := range []string{"", "/diffs/current", "/review"} {
			response := request(tc.token, "GET", "/api/v2/contexts/"+tc.foreign+suffix, nil)
			response.Body.Close()
			if response.StatusCode != 404 {
				t.Fatalf("foreign REST %s: %d", suffix, response.StatusCode)
			}
		}
		response = request(tc.token, "DELETE", "/api/v2/contexts/"+tc.foreign, nil)
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("foreign delete: %d", response.StatusCode)
		}
		initialize := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
		response = request(tc.token, "POST", "/mcp/contexts/"+tc.foreign, bytes.NewReader(initialize))
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("foreign MCP: %d", response.StatusCode)
		}
		response = request(tc.token, "POST", "/mcp/contexts/"+tc.own, bytes.NewReader(initialize))
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("own MCP: %d", response.StatusCode)
		}
	}
	for _, name := range []string{"admin", "", "other"} {
		r := httptest.NewRequest("GET", "/api/v2/contexts", nil)
		r.SetBasicAuth(name, testToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("Basic accepted")
		}
	}
	for _, value := range []string{"", "Bearer wrong", "bearer " + testToken} {
		r := httptest.NewRequest("GET", "/api/v2/contexts", nil)
		r.Header.Set("Authorization", value)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("invalid auth: %d", w.Code)
		}
	}
}

func TestPasswordBootstrapSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Username: "admin", Password: testPassword}
	identity := identityservice.New(store)
	if err := bootstrap(t.Context(), identity, settings); err != nil {
		t.Fatal(err)
	}
	session, err := identity.Login(t.Context(), "admin", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.ChangePassword(t.Context(), session.Account.ID, testPassword, "changed-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity = identityservice.New(store)
	if err := bootstrap(t.Context(), identity, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Login(t.Context(), "admin", testPassword); err == nil {
		t.Fatal("restart replaced changed password")
	}
	again, err := identity.Login(t.Context(), "admin", "changed-password-123")
	if err != nil || again.Account.ID != session.Account.ID {
		t.Fatalf("restart identity: %+v %v", again, err)
	}
}
func TestEmptyServerRequiresExplicitIdentityPolicy(t *testing.T) {
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := identityservice.New(store)
	for _, settings := range []Settings{{}, {Username: "admin"}, {Password: testPassword}} {
		if err := bootstrap(t.Context(), identity, settings); err == nil {
			t.Fatal("missing policy accepted")
		}
	}
	if err := bootstrap(t.Context(), identity, Settings{SignupEnabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionDurationValidation(t *testing.T) {
	for days, expected := range map[int]time.Duration{0: 7 * 24 * time.Hour, 7: 7 * 24 * time.Hour, 1: 24 * time.Hour, 30: 30 * 24 * time.Hour} {
		actual, err := Retention(days)
		if err != nil || actual != expected {
			t.Fatalf("retention %d: %v %v", days, actual, err)
		}
	}
	for _, days := range []int{-1, 1000000000} {
		if _, err := Retention(days); err == nil {
			t.Fatalf("invalid retention: %d", days)
		}
	}
}

func TestRemoteRunShutdownClosesActiveEventStream(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Settings{Listen: "127.0.0.1:0", State: filepath.Join(t.TempDir(), "reviews.sqlite"), Username: "account", Password: testPassword, InsecureCookies: true}, func(address string) { ready <- address })
	}()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("server did not start: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("server startup timed out")
	}
	r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+address+"/api/v2/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	login, err := client.Post("http://"+address+"/api/v2/auth/login", "application/json", strings.NewReader(`{"username":"account","password":"`+testPassword+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	for _, cookie := range login.Cookies() {
		r.AddCookie(cookie)
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("event stream status %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote shutdown failed to cancel active stream")
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("shutdown stream: %v", err)
	}
}
