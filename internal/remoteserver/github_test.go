package remoteserver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

// fakeGitHubAPI serves the API and OAuth endpoints the integration calls.
func fakeGitHubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	reply := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
	}
	reply("/app", `{"slug":"diffx-test"}`)
	reply("/login/oauth/access_token", `{"access_token":"user-access","expires_in":28800,"refresh_token":"user-refresh","refresh_token_expires_in":15897600}`)
	reply("/user", `{"id":100,"login":"mona","html_url":"https://github.com/mona"}`)
	reply("/user/installations", `{"installations":[{"id":5}]}`)
	reply("/user/installations/5/repositories", `{"repositories":[{"id":10,"full_name":"octo/app","private":true}]}`)
	reply("/app/installations/5/access_tokens", `{"token":"installation","expires_at":"2099-01-01T00:00:00Z"}`)
	reply("/repos/octo/app/pulls", `[]`)
	reply("/repos/octo/app/pulls/1", `{"number":1,"state":"open","title":"Add search","body":"","html_url":"https://github.com/octo/app/pull/1","user":{"id":7,"login":"mona"},
		"base":{"ref":"main","sha":"3333333333333333333333333333333333333333","repo":{"id":10,"full_name":"octo/app"}},
		"head":{"ref":"search","sha":"1111111111111111111111111111111111111111","repo":{"id":77,"full_name":"mona/app"}},
		"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T01:00:00Z"}`)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func githubSettings(t *testing.T, api string) *GitHubSettings {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	return &GitHubSettings{AppID: 1, ClientID: "client", ClientSecret: "secret", PrivateKeyFile: path, WebhookSecret: "hook-secret",
		TokenKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), PublicURL: "https://diffx.test", apiURL: api, webURL: api}
}

func signed(body string) string {
	mac := hmac.New(sha256.New, []byte("hook-secret"))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhook(handler http.Handler, delivery, signature, body string) int {
	r := httptest.NewRequest("POST", "/api/v2/github/webhooks", strings.NewReader(body))
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-GitHub-Delivery", delivery)
	r.Header.Set("X-Hub-Signature-256", signature)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w.Code
}

func TestGitHubRoutesAbsentWithoutConfiguration(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	cookie, _ := memberSession(t, store, "user")
	if w := identityRequest(handler, "GET", "/api/v2/github/connection", "", cookie, "", ""); w.Code != 404 {
		t.Fatalf("unconfigured connection: %d", w.Code)
	}
	if code := webhook(handler, "d", signed("{}"), "{}"); code != 404 {
		t.Fatalf("unconfigured webhook: %d", code)
	}
	if w := identityRequest(handler, "GET", "/api/v2/github/notifications", "", cookie, "", ""); w.Code != 404 {
		t.Fatalf("unconfigured notification preferences: %d", w.Code)
	}
	if w := identityRequest(handler, "GET", "/api/v2/github/history/overview?edges=1&edges=2", "", cookie, "", ""); w.Code != 404 {
		t.Fatalf("unconfigured history overview: %d", w.Code)
	}
}

func TestGitHubNotificationPreferences(t *testing.T) {
	api := fakeGitHubAPI(t)
	store, handler := identityHandler(t, Settings{GitHub: githubSettings(t, api.URL)})
	token := provisionToken(t, store, "automation", identityservice.PurposeAPI)
	cookie, csrf := memberSession(t, store, "user")
	const path = "/api/v2/github/notifications"
	defaults := `{"opened":{"byMe":true,"byOthers":true},"newCommits":{"byMe":false,"byOthers":false},"merged":{"byMe":false,"byOthers":false},` +
		`"closed":{"byMe":false,"byOthers":false},"skipDrafts":false,"onlyAuthors":[],"neverAuthors":[],"baseBranches":[],"mutedTitleTerms":[]}`
	w := identityRequest(handler, "GET", path, "", cookie, "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != defaults {
		t.Fatalf("defaults: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "GET", path, "", nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token read preferences: %d", w.Code)
	}
	put := `{"opened":{"byMe":false,"byOthers":true},"newCommits":{"byMe":true,"byOthers":false},"merged":{"byMe":true,"byOthers":true},` +
		`"closed":{"byMe":false,"byOthers":false},"skipDrafts":true,"onlyAuthors":[" Mona ","mona","","hubot"],"neverAuthors":["dependabot[bot]"],` +
		`"baseBranches":["main","release/*"],"mutedTitleTerms":["WIP","wip "]}`
	if w := identityRequest(handler, "PUT", path, put, cookie, "", ""); w.Code != 403 {
		t.Fatalf("put without CSRF: %d", w.Code)
	}
	if w := identityRequest(handler, "PUT", path, put, nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token wrote preferences: %d", w.Code)
	}
	stored := `{"opened":{"byMe":false,"byOthers":true},"newCommits":{"byMe":true,"byOthers":false},"merged":{"byMe":true,"byOthers":true},` +
		`"closed":{"byMe":false,"byOthers":false},"skipDrafts":true,"onlyAuthors":["Mona","hubot"],"neverAuthors":["dependabot[bot]"],` +
		`"baseBranches":["main","release/*"],"mutedTitleTerms":["WIP"]}`
	w = identityRequest(handler, "PUT", path, put, cookie, csrf, "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != stored {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "GET", path, "", cookie, "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != stored {
		t.Fatalf("stored: %d %s", w.Code, w.Body.String())
	}
	many := make([]string, 21)
	for index := range many {
		many[index] = fmt.Sprintf("%q", fmt.Sprintf("author-%d", index))
	}
	for name, body := range map[string]string{
		"too many entries": `{"onlyAuthors":[` + strings.Join(many, ",") + `]}`,
		"entry too long":   `{"baseBranches":["` + strings.Repeat("b", 101) + `"]}`,
		"unknown field":    `{"everything":true}`,
	} {
		if w := identityRequest(handler, "PUT", path, body, cookie, csrf, ""); w.Code != 400 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := identityRequest(handler, "POST", path, put, cookie, csrf, ""); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	w = identityRequest(handler, "GET", path, "", cookie, "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != stored {
		t.Fatalf("refused puts changed preferences: %d %s", w.Code, w.Body.String())
	}
}

func TestGitHubConnectFollowAndWebhookBoundaries(t *testing.T) {
	api := fakeGitHubAPI(t)
	store, handler := identityHandler(t, Settings{GitHub: githubSettings(t, api.URL)})
	token := provisionToken(t, store, "automation", identityservice.PurposeAPI)
	cookie, csrf := memberSession(t, store, "user")

	body := `{"action":"opened","number":1,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`
	var logs syncBuffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if code := webhook(handler, "d1", "sha256=00", body); code != 401 {
		t.Fatalf("forged webhook: %d", code)
	}
	if got := logs.String(); !strings.Contains(got, `rejected signature for event "pull_request" delivery "d1"`) || strings.Contains(got, "sha256") {
		t.Fatalf("rejected delivery log %q", got)
	}
	if code := webhook(handler, "d1", signed(body), body); code != 202 {
		t.Fatalf("signed webhook without diffx credentials: %d", code)
	}

	if w := identityRequest(handler, "GET", "/api/v2/github/connection", "", nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token reached GitHub credentials: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/github/connection", "", cookie, "", ""); w.Code != 403 {
		t.Fatalf("connect without CSRF: %d", w.Code)
	}
	w := identityRequest(handler, "POST", "/api/v2/github/connection", "", cookie, csrf, "")
	var started struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &started) != nil {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	authorize, err := url.Parse(started.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	callback := "/api/v2/github/callback?" + url.Values{"code": {"code"}, "state": {authorize.Query().Get("state")}}.Encode()
	// GitHub's redirect is a cross-site top-level navigation carrying the Lax session cookie.
	r := httptest.NewRequest("GET", callback, nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/settings/github?github=connected" {
		t.Fatalf("callback: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = identityRequest(handler, "GET", "/api/v2/github/connection", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"login":"mona"`) || !strings.Contains(w.Body.String(), "apps/diffx-test/installations/select_target") {
		t.Fatalf("connection: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "PUT", "/api/v2/github/follows/99", "", cookie, csrf, ""); w.Code != 403 {
		t.Fatalf("followed inaccessible repository: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "PUT", "/api/v2/github/follows/10", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("follow: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "GET", "/api/v2/github/repositories", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"fullName":"octo/app","private":true,"followed":true,"lost":false`) {
		t.Fatalf("repositories: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "POST", "/api/v2/github/resync", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("resync: %d %s", w.Code, w.Body.String())
	}
	now := time.Now().UnixMilli()
	week := fmt.Sprintf("/api/v2/github/history?since=%d&until=%d", now-7*24*3600*1000, now)
	if w := identityRequest(handler, "GET", week, "", nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token read history: %d", w.Code)
	}
	w = identityRequest(handler, "GET", week, "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"connected":true,"login":"mona","following":true`) {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
	var history goclient.GitHubHistory
	strictJSON(t, w.Body.Bytes(), &history, "connected", "following", "retainedSince", "since", "until", "opened", "merged", "repositories", "states", "pullRequests", "nextCursor")
	if history.PullRequests == nil || history.Repositories == nil || !history.NextCursor.IsNull() {
		t.Fatalf("history page: %s", w.Body.String())
	}
	if w := identityRequest(handler, "GET", week+"&limit=500&repo=octo/app&activity=merged&state=draft", "", cookie, "", ""); w.Code != 200 {
		t.Fatalf("filtered history: %d %s", w.Code, w.Body.String())
	}
	year := fmt.Sprintf("?since=%d&until=%d", now-366*24*3600*1000, now)
	for _, query := range []string{"", "?since=1", "?since=a&until=2", fmt.Sprintf("?since=%d&until=%d", now, now-1), fmt.Sprintf("?since=%d&until=%d", now-367*24*3600*1000, now),
		year + "&limit=a", year + "&limit=", year + "&limit=0", year + "&limit=501", year + "&state=bogus", year + "&activity=closed", year + "&cursor=!!"} {
		if w := identityRequest(handler, "GET", "/api/v2/github/history"+query, "", cookie, "", ""); w.Code != 400 {
			t.Fatalf("history %q: %d %s", query, w.Code, w.Body.String())
		}
	}
	if w := identityRequest(handler, "GET", "/api/v2/github/history"+year, "", cookie, "", ""); w.Code != 200 {
		t.Fatalf("history over a year: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "POST", week, "", cookie, csrf, ""); w.Code != 405 {
		t.Fatalf("history POST: %d", w.Code)
	}
	day := int64(24 * 3600 * 1000)
	overviewPath := fmt.Sprintf("/api/v2/github/history/overview?edges=%d&edges=%d&edges=%d", now-2*day, now-day, now)
	if w := identityRequest(handler, "GET", overviewPath, "", nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token read history overview: %d", w.Code)
	}
	w = identityRequest(handler, "GET", overviewPath, "", cookie, "", "")
	if w.Code != 200 {
		t.Fatalf("history overview: %d %s", w.Code, w.Body.String())
	}
	var overview goclient.GitHubHistoryOverview
	strictJSON(t, w.Body.Bytes(), &overview, "connected", "following", "retainedSince", "since", "until", "opened", "merged", "openNow", "medianToMerge", "bins", "repositories", "stillOpen", "truncated")
	if !overview.Connected || !overview.Following || len(overview.Bins) != 2 || overview.Bins[1].Since != now-day || overview.Repositories == nil || overview.StillOpen == nil || !overview.MedianToMerge.IsNull() {
		t.Fatalf("history overview: %s", w.Body.String())
	}
	for _, query := range []string{"", "?edges=1", "?edges=a&edges=2", fmt.Sprintf("?edges=%d&edges=%d", now, now-day), fmt.Sprintf("?edges=%d&edges=%d", now-367*day, now)} {
		if w := identityRequest(handler, "GET", "/api/v2/github/history/overview"+query, "", cookie, "", ""); w.Code != 400 {
			t.Fatalf("history overview %q: %d %s", query, w.Code, w.Body.String())
		}
	}
	if w := identityRequest(handler, "POST", overviewPath, "", cookie, csrf, ""); w.Code != 405 {
		t.Fatalf("history overview POST: %d", w.Code)
	}
	if w := identityRequest(handler, "DELETE", "/api/v2/github/connection", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("disconnect: %d", w.Code)
	}
}

// strictJSON decodes data into the contract type, refusing unknown fields,
// and requires the listed fields, null ones included.
func strictJSON(t *testing.T, data []byte, target any, required ...string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			t.Fatalf("%s lacks %q", data, name)
		}
	}
}

func TestGitHubCallbackRefusesForeignAndMissingSessions(t *testing.T) {
	api := fakeGitHubAPI(t)
	store, handler := identityHandler(t, Settings{GitHub: githubSettings(t, api.URL)})
	alice, csrf := memberSession(t, store, "alice")
	bob, _ := memberSession(t, store, "bob")
	w := identityRequest(handler, "POST", "/api/v2/github/connection", "", alice, csrf, "")
	var started struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(started.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	callback := "/api/v2/github/callback?" + url.Values{"code": {"code"}, "state": {authorize.Query().Get("state")}}.Encode()
	for _, tc := range []struct {
		cookie   *http.Cookie
		location string
	}{{nil, "/settings/github?github=signin"}, {bob, "/settings/github?github=failed"}} {
		r := httptest.NewRequest("GET", callback, nil)
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 303 || w.Header().Get("Location") != tc.location {
			t.Fatalf("callback: %d %s, want %s", w.Code, w.Header().Get("Location"), tc.location)
		}
	}
	w = identityRequest(handler, "GET", "/api/v2/github/connection", "", bob, "", "")
	if strings.Contains(w.Body.String(), `"connected":true`) {
		t.Fatalf("bob completed alice's authorization: %s", w.Body.String())
	}
}

func TestFetchProtocolOnlyOnInternalListenerWithSecret(t *testing.T) {
	store := pgtest.Store(t)
	settings := githubSettings(t, fakeGitHubAPI(t).URL)
	settings.FetcherSecret = strings.Repeat("s", 32)
	public, internal, closeServices, _, err := compose(t.Context(), store, fstest.MapFS{"index.html": {Data: []byte("app shell")}}, Settings{GitHub: settings})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	lease := func(handler http.Handler, method, authorization string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, fetchprotocol.LeasePath, nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := lease(public, "POST", "Bearer "+settings.FetcherSecret); w.Code == 200 || w.Code == 204 {
		t.Fatalf("public listener served the fetch protocol: %d", w.Code)
	}
	for _, authorization := range []string{"", "Bearer wrong", settings.FetcherSecret} {
		if w := lease(internal, "POST", authorization); w.Code != 401 {
			t.Fatalf("lease with %q: %d", authorization, w.Code)
		}
	}
	if w := lease(internal, "GET", "Bearer "+settings.FetcherSecret); w.Code != 405 {
		t.Fatalf("GET lease: %d", w.Code)
	}
	if w := lease(internal, "POST", "Bearer "+settings.FetcherSecret); w.Code != 204 {
		t.Fatalf("idle lease: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", fetchprotocol.ResultPath, strings.NewReader(`{"id":"10-1","leaseToken":"stale","error":"x"}`))
	r.Header.Set("Authorization", "Bearer "+settings.FetcherSecret)
	w := httptest.NewRecorder()
	internal.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatalf("result without a lease: %d", w.Code)
	}
}

func TestStartupReconcileRunsOnOneReplicaAtATime(t *testing.T) {
	url := pgtest.URL(t)
	open := func() *pgstore.Store {
		store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	first, second := open(), open()
	running, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		reconcileOnce(t.Context(), func(context.Context) error {
			close(running)
			<-release
			return nil
		}, first.GitHub())
	}()
	<-running
	calls := 0
	count := func(context.Context) error { calls++; return nil }
	reconcileOnce(t.Context(), count, second.GitHub())
	if calls != 0 {
		t.Fatal("second replica reconciled concurrently")
	}
	close(release)
	<-done
	reconcileOnce(t.Context(), count, second.GitHub())
	if calls != 1 {
		t.Fatalf("reconciled %d times after the first replica finished", calls)
	}
}
