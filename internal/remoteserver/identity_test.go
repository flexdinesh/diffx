package remoteserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func identityHandler(t *testing.T, settings Settings) (*reviewstore.Store, http.Handler) {
	t.Helper()
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler, closeServices, err := Handler(t.Context(), store, settings, fstest.MapFS{"index.html": {Data: []byte("app shell")}, "assets/app.js": {Data: []byte("app()")}})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices(); store.Close() })
	return store, handler
}
func identityRequest(handler http.Handler, method, path, body string, cookie *http.Cookie, csrf, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if bearer != "" {
		r.Header.Set("Authorization", bearer)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func loginIdentity(t *testing.T, handler http.Handler, name string) (*http.Cookie, string) {
	t.Helper()
	w := identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"`+name+`","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var result bootstrapJSON
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || result.CSRFToken == nil {
		t.Fatal("missing session")
	}
	return cookies[0], *result.CSRFToken
}

// memberSession signs in a passwordless member, as Google sign-in does.
func memberSession(t *testing.T, store *reviewstore.Store, name string) (*http.Cookie, string) {
	t.Helper()
	session := testsupport.RemoteSession(t, store, testsupport.RemoteAccount(t, store, name))
	return &http.Cookie{Name: sessionCookie, Value: session.Secret}, session.CSRFToken
}
func TestRemoteSessionCSRFAndTokenLifecycle(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	testsupport.RemoteAdmin(t, store, "alice", testPassword)
	cookie, csrf := loginIdentity(t, handler, "alice")
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("session cookie: %+v", cookie)
	}
	for _, path := range []string{"/api/v2/account/tokens", "/api/v2/contexts/missing"} {
		method := "POST"
		if strings.Contains(path, "contexts") {
			method = "DELETE"
		}
		w := identityRequest(handler, method, path, `{}`, cookie, "", "")
		if w.Code != 403 {
			t.Fatalf("missing csrf %s: %d", path, w.Code)
		}
	}
	w := identityRequest(handler, "POST", "/api/v2/account/tokens", `{"name":"collector","purpose":"collector","expiresInDays":30}`, cookie, csrf, "")
	if w.Code != 201 {
		t.Fatalf("create token: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Token  tokenJSON `json:"token"`
		Secret string    `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || created.Token.CreatedAt <= 0 || created.Token.LastUsedAt != nil {
		t.Fatalf("token metadata: %+v", created)
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{{"GET", "/api/v2/health", 200}, {"GET", "/api/v2/contexts", 200}, {"GET", "/api/v2/contexts/missing", 404}, {"GET", "/api/v2/contexts/missing/diffs/current", 403}, {"DELETE", "/api/v2/contexts/missing", 403}, {"GET", "/api/v2/events", 403}, {"POST", "/mcp", 403}, {"GET", "/api/v2/account/tokens", 403}, {"POST", "/api/v2/auth/logout", 403}} {
		w = identityRequest(handler, test.method, test.path, `{}`, nil, "", "Bearer "+created.Secret)
		if w.Code != test.want {
			t.Fatalf("collector %s %s: %d want%d", test.method, test.path, w.Code, test.want)
		}
	}
	w = identityRequest(handler, "GET", "/api/v2/account/tokens", "", cookie, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), created.Secret) {
		t.Fatalf("token listing leaks secret: %s", w.Body.String())
	}
	// A valid browser session never rescues an invalid explicit bearer credential.
	w = identityRequest(handler, "GET", "/api/v2/contexts", "", cookie, "", "Bearer invalid")
	if w.Code != 401 {
		t.Fatal("invalid bearer fell back to cookie")
	}
	other := provisionToken(t, store, "other", identityservice.PurposeAPI)
	w = identityRequest(handler, "GET", "/api/v2/account/tokens", "", nil, "", "Bearer "+other)
	if w.Code != 403 {
		t.Fatal("api token managed account")
	}
	otherCookie, otherCSRF := memberSession(t, store, "member")
	w = identityRequest(handler, "DELETE", "/api/v2/account/tokens/"+created.Token.ID, "", otherCookie, otherCSRF, "")
	if w.Code != 404 {
		t.Fatal("foreign token revoked")
	}
	w = identityRequest(handler, "DELETE", "/api/v2/account/tokens/"+created.Token.ID, "", cookie, csrf, "")
	if w.Code != 204 {
		t.Fatal("token revocation failed")
	}
	w = identityRequest(handler, "GET", "/api/v2/health", "", nil, "", "Bearer "+created.Secret)
	if w.Code != 401 {
		t.Fatal("revoked token accepted")
	}
	w = identityRequest(handler, "PUT", "/api/v2/account/password", `{"currentPassword":"`+testPassword+`","newPassword":"new-password-1234"}`, cookie, csrf, "")
	if w.Code != 204 {
		t.Fatalf("password change: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "GET", "/api/v2/contexts", "", cookie, "", "")
	if w.Code != 401 {
		t.Fatal("changed password did not revoke session")
	}
	for _, path := range []string{"/", "/settings", "/assets/app.js", "/api/v2/bootstrap"} {
		w = identityRequest(handler, "GET", path, "", cookie, "", "")
		if w.Code != 200 {
			t.Fatalf("expired cookie blocks public login shell %s: %d", path, w.Code)
		}
		if path == "/api/v2/bootstrap" && !strings.Contains(w.Body.String(), `"user":null`) {
			t.Fatal("stale session bootstrap exposes user")
		}
	}
}
func TestSetupActivatesAdminWithoutRestart(t *testing.T) {
	store, handler := identityHandler(t, Settings{InsecureCookies: true})
	setup, err := identityservice.New(store).PrepareSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	w := identityRequest(handler, "GET", "/api/v2/setup", "", nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"required":true`) {
		t.Fatalf("setup required: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/setup", `{"token":"`+strings.Repeat("x", 43)+`","username":"admin","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 404 {
		t.Fatalf("forged setup: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/setup", `{"token":"`+setup+`","username":"admin","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatal("development cookie policy")
	}
	var result bootstrapJSON
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User == nil || !result.User.Admin || result.CSRFToken == nil {
		t.Fatal("setup identity absent")
	}
	w = identityRequest(handler, "GET", "/api/v2/setup", "", nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"required":false`) {
		t.Fatalf("setup still required: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/setup", `{"token":"`+setup+`","username":"second","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 404 {
		t.Fatalf("setup reused: %d", w.Code)
	}
	w = identityRequest(handler, "POST", "/api/v2/account/tokens", `{"name":"collector","purpose":"collector","expiresInDays":1}`, cookies[0], *result.CSRFToken, "")
	if w.Code != 201 {
		t.Fatalf("new account token: %d", w.Code)
	}
	var token struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n"
	input, err := collector.CollectPatch(t.Context(), patch, t.TempDir(), collector.Options{SourceID: "setup", SubmissionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	client := ingestion.NewClient(server.URL, token.Secret)
	health, err := client.Health(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.SyncTo(t.Context(), input, health.StateID, nil)
	if err != nil {
		t.Fatal(err)
	}
	w = identityRequest(handler, "GET", "/api/v2/contexts", "", cookies[0], "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), receipt.ContextID) {
		t.Fatalf("new owner handler did not activate: %d %s", w.Code, w.Body.String())
	}
}
func TestPasswordLoginIsAdminOnly(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	testsupport.RemoteAdmin(t, store, "alice", testPassword)
	testsupport.RemoteAccount(t, store, "member")
	for _, body := range []string{`{"username":"member","password":""}`, `{"username":"member","password":"` + testPassword + `"}`, `{"username":"nobody","password":"` + testPassword + `"}`, `{"username":"alice","password":"wrong-password-value"}`} {
		w := identityRequest(handler, "POST", "/api/v2/auth/login", body, nil, "", "")
		if w.Code != 401 || !strings.Contains(w.Body.String(), "Authentication required") {
			t.Fatalf("login %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	cookie, csrf := loginIdentity(t, handler, "alice")
	if w := identityRequest(handler, "POST", "/api/v2/auth/signup", `{"username":"new","password":"`+testPassword+`"}`, cookie, csrf, ""); w.Code != 404 {
		t.Fatalf("public signup route: %d", w.Code)
	}
	w := identityRequest(handler, "GET", "/api/v2/bootstrap", "", cookie, "", "")
	if strings.Contains(w.Body.String(), "signupEnabled") || !strings.Contains(w.Body.String(), `"googleEnabled":false`) || !strings.Contains(w.Body.String(), `"admin":true`) {
		t.Fatalf("bootstrap: %s", w.Body.String())
	}
}
func TestAuthFormsGuardAndRateLimit(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	testsupport.RemoteAdmin(t, store, "alice", testPassword)
	var w *httptest.ResponseRecorder
	for _, test := range []struct {
		header, value string
		want          int
	}{{"Origin", "https://evil.example", 403}, {"Sec-Fetch-Site", "cross-site", 403}, {"Content-Type", "text/plain", 415}} {
		r := httptest.NewRequest("POST", "/api/v2/auth/login", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(test.header, test.value)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("auth guard %s: %d", test.header, w.Code)
		}
	}
	for i := 0; i < 25; i++ {
		w = identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"alice","password":"wrong"}`, nil, "", "")
	}
	if w.Code != 429 {
		t.Fatal("authentication attempts not bounded")
	}
}
func TestRevocationAndExpiryCloseActiveStreams(t *testing.T) {
	for _, action := range []string{"logout", "reset", "revoke", "expire"} {
		t.Run(action, func(t *testing.T) {
			store, handler := identityHandler(t, Settings{InsecureCookies: true})
			testsupport.RemoteAdmin(t, store, "alice", testPassword)
			identity := identityservice.New(store)
			session, err := identity.Login(t.Context(), "alice", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			r, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/api/v2/events", nil)
			if err != nil {
				t.Fatal(err)
			}
			var token identityservice.CreatedToken
			if action == "revoke" || action == "expire" {
				expiry := time.Now().Add(time.Hour)
				if action == "expire" {
					expiry = time.Now().Add(700 * time.Millisecond)
				}
				token, err = identity.CreateToken(t.Context(), session.Account.ID, "stream", identityservice.PurposeAPI, expiry)
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+token.Secret)
			} else {
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session.Secret})
			}
			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("stream unavailable")
			}
			switch action {
			case "logout":
				err = identity.Logout(t.Context(), session.Secret)
			case "reset":
				err = identity.ResetPassword(t.Context(), "alice", "reset-password-123")
			case "revoke":
				err = identity.RevokeToken(t.Context(), session.Account.ID, token.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := io.Copy(&bytes.Buffer{}, response.Body); done <- err }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("invalid credential kept stream open")
			}
		})
	}
}
