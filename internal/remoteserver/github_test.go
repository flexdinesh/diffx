package remoteserver

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/identityservice"
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
	store, handler := identityHandler(t, Settings{SignupEnabled: true})
	if _, err := identityservice.New(store).Provision(t.Context(), "user", testPassword); err != nil {
		t.Fatal(err)
	}
	cookie, _ := loginIdentity(t, handler, "user")
	if w := identityRequest(handler, "GET", "/api/v2/github/connection", "", cookie, "", ""); w.Code != 404 {
		t.Fatalf("unconfigured connection: %d", w.Code)
	}
	if code := webhook(handler, "d", signed("{}"), "{}"); code != 404 {
		t.Fatalf("unconfigured webhook: %d", code)
	}
}

func TestGitHubConnectFollowAndWebhookBoundaries(t *testing.T) {
	api := fakeGitHubAPI(t)
	store, handler := identityHandler(t, Settings{SignupEnabled: true, GitHub: githubSettings(t, api.URL)})
	identity := identityservice.New(store)
	if _, err := identity.Provision(t.Context(), "user", testPassword); err != nil {
		t.Fatal(err)
	}
	token := provisionToken(t, store, "automation", identityservice.PurposeAPI)
	cookie, csrf := loginIdentity(t, handler, "user")

	body := `{"action":"opened","number":1,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`
	if code := webhook(handler, "d1", "sha256=00", body); code != 401 {
		t.Fatalf("forged webhook: %d", code)
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
	if w.Code != 303 || w.Header().Get("Location") != "/settings?github=connected" {
		t.Fatalf("callback: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = identityRequest(handler, "GET", "/api/v2/github/connection", "", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"login":"mona"`) || !strings.Contains(w.Body.String(), "apps/diffx-test/installations/new") {
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
	if w := identityRequest(handler, "DELETE", "/api/v2/github/connection", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("disconnect: %d", w.Code)
	}
}

func TestGitHubCallbackRefusesForeignAndMissingSessions(t *testing.T) {
	api := fakeGitHubAPI(t)
	store, handler := identityHandler(t, Settings{SignupEnabled: true, GitHub: githubSettings(t, api.URL)})
	identity := identityservice.New(store)
	for _, name := range []string{"alice", "bob"} {
		if _, err := identity.Provision(t.Context(), name, testPassword); err != nil {
			t.Fatal(err)
		}
	}
	alice, csrf := loginIdentity(t, handler, "alice")
	bob, _ := loginIdentity(t, handler, "bob")
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
	}{{nil, "/settings?github=signin"}, {bob, "/settings?github=failed"}} {
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
