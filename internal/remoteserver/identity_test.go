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

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// identityHandler serves settings with the default app settings, signup and
// password signup on, unless the test stores others first.
func identityHandler(t *testing.T, settings Settings, admission ...identityservice.Settings) (*pgstore.Store, http.Handler) {
	t.Helper()
	store := pgtest.Store(t)
	for _, value := range admission {
		testsupport.PutSettings(t, store, value)
	}
	handler, closeServices, err := Handler(t.Context(), store, settings, fstest.MapFS{"index.html": {Data: []byte("app shell")}, "assets/app.js": {Data: []byte("app()")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
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

// memberSession signs in a Google member, as Google sign-in does.
func memberSession(t *testing.T, store *pgstore.Store, name string) (*http.Cookie, string) {
	t.Helper()
	session := testsupport.RemoteSession(t, store, testsupport.RemoteAccount(t, store, name))
	return &http.Cookie{Name: authhttp.SessionCookie, Value: session.Secret}, session.CSRFToken
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
func TestSignupActivatesOwnerWithoutRestart(t *testing.T) {
	_, handler := identityHandler(t, Settings{InsecureCookies: true})
	if w := identityRequest(handler, "POST", "/api/v2/auth/signup", `{"username":"valid-handle","password":"`+testPassword+`"}`, nil, "", ""); w.Code != 400 || !strings.Contains(w.Body.String(), "valid email address") {
		t.Fatalf("signup without email: %d %s", w.Code, w.Body.String())
	}
	w := identityRequest(handler, "POST", "/api/v2/auth/signup", `{"email":"ab","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 400 {
		t.Fatalf("invalid email: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/auth/signup", `{"email":"Newcomer@Example.com","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 200 {
		t.Fatalf("signup: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatal("development cookie policy")
	}
	var result bootstrapJSON
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User == nil || result.User.Username != "newcomer@example.com" || result.User.Method != identityservice.MethodPassword || result.CSRFToken == nil {
		t.Fatalf("signup identity: %+v", result.User)
	}
	if w = identityRequest(handler, "POST", "/api/v2/auth/signup", `{"email":"newcomer@example.com","password":"`+testPassword+`"}`, nil, "", ""); w.Code != 409 {
		t.Fatalf("duplicate signup: %d", w.Code)
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
	input, err := collector.CollectPatch(t.Context(), patch, t.TempDir(), collector.Options{SourceID: "signup", SubmissionID: "first"})
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
func TestPasswordLoginAdmitsPasswordAccounts(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	testsupport.RemoteAdmin(t, store, "alice", testPassword)
	testsupport.RemoteAccount(t, store, "member")
	for _, body := range []string{`{"username":"member","password":""}`, `{"username":"member","password":"` + testPassword + `"}`, `{"username":"nobody","password":"` + testPassword + `"}`, `{"username":"alice","password":"wrong-password-value"}`} {
		w := identityRequest(handler, "POST", "/api/v2/auth/login", body, nil, "", "")
		if w.Code != 401 || !strings.Contains(w.Body.String(), "Authentication required") {
			t.Fatalf("login %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	// Admins use the web app like every other account.
	cookie, _ := loginIdentity(t, handler, "alice")
	w := identityRequest(handler, "GET", "/api/v2/bootstrap", "", cookie, "", "")
	if strings.Contains(w.Body.String(), "admin") || !strings.Contains(w.Body.String(), `"googleEnabled":false,"signupEnabled":true,"passwordSignupEnabled":true`) || !strings.Contains(w.Body.String(), `"email":null,"method":"password"`) {
		t.Fatalf("bootstrap: %s", w.Body.String())
	}
	for _, path := range []string{"/api/v2/setup", "/api/v2/manage/users", "/api/v2/manage/invites"} {
		if w := identityRequest(handler, "GET", path, "", cookie, "", ""); w.Code != 404 {
			t.Fatalf("admin route %s on the web app: %d", path, w.Code)
		}
	}
}
func TestClosedSettingsKeepExistingPasswordAccounts(t *testing.T) {
	store := pgtest.Store(t)
	open := testsupport.Identity(t, store)
	session, err := open.SignUp(t.Context(), "member@example.com", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	token, err := open.CreateToken(t.Context(), session.Account.ID, "api", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	_, reset, err := open.ResetPassword(t.Context(), session.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	invite, inviteSecret, err := open.CreateInvite(t.Context(), "new@example.com")
	if err != nil {
		t.Fatal(err)
	}
	testsupport.PutSettings(t, store, identityservice.Settings{})
	handler, closeServices, err := Handler(t.Context(), store, Settings{}, fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	// New accounts cannot join; Google is not configured either.
	for _, test := range []struct{ path, body string }{
		{"/api/v2/auth/signup", `{"email":"another@example.com","password":"` + testPassword + `"}`},
		{"/api/v2/invites/password", `{"token":"` + inviteSecret + `","email":"new@example.com","password":"` + testPassword + `"}`},
		{"/api/v2/auth/google", `{}`},
	} {
		if w := identityRequest(handler, "POST", test.path, test.body, nil, "", ""); w.Code != 404 {
			t.Fatalf("%s with signup off: %d %s", test.path, w.Code, w.Body.String())
		}
	}
	// The existing password account keeps every way in.
	cookie := &http.Cookie{Name: authhttp.SessionCookie, Value: session.Secret}
	if w := identityRequest(handler, "GET", "/api/v2/contexts", "", cookie, "", ""); w.Code != 200 {
		t.Fatalf("password session with password signup off: %d", w.Code)
	}
	if w := identityRequest(handler, "GET", "/api/v2/contexts", "", nil, "", "Bearer "+token.Secret); w.Code != 200 {
		t.Fatalf("password account token with password signup off: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/password-reset/lookup", `{"token":"`+reset+`"}`, nil, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"member@example.com"`) {
		t.Fatalf("reset lookup with password signup off: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "POST", "/api/v2/password-reset", `{"token":"`+reset+`","password":"a new password value"}`, nil, "", ""); w.Code != 204 {
		t.Fatalf("reset with password signup off: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"member@example.com","password":"a new password value"}`, nil, "", ""); w.Code != 200 {
		t.Fatalf("password login with password signup off: %d %s", w.Code, w.Body.String())
	}
	w := identityRequest(handler, "POST", "/api/v2/invites/lookup", `{"token":"`+inviteSecret+`"}`, nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"email":"`+invite.Email+`"`) || strings.Contains(w.Body.String(), "admin") {
		t.Fatalf("invite lookup: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", "")
	if !strings.Contains(w.Body.String(), `"signupEnabled":false,"passwordSignupEnabled":false`) {
		t.Fatalf("bootstrap: %s", w.Body.String())
	}
}

func TestPasswordInviteAndResetLinks(t *testing.T) {
	store, handler := identityHandler(t, Settings{InsecureCookies: true})
	identity := testsupport.Identity(t, store)
	_, inviteSecret, err := identity.CreateInvite(t.Context(), "lead@example.com")
	if err != nil {
		t.Fatal(err)
	}
	w := identityRequest(handler, "POST", "/api/v2/invites/lookup", `{"token":"`+inviteSecret+`"}`, nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"email":"lead@example.com"`) {
		t.Fatalf("invite lookup: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/invites/password", `{"token":"`+inviteSecret+`","email":"lead@example.com","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("password invite: %d %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	w = identityRequest(handler, "POST", "/api/v2/invites/password", `{"token":"`+inviteSecret+`","email":"lead@example.com","password":"`+testPassword+`"}`, nil, "", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "Invite link is invalid") {
		t.Fatalf("invite reused: %d %s", w.Code, w.Body.String())
	}
	account, reset, err := identity.ResetPasswordByUsername(t.Context(), "lead@example.com")
	if err != nil || account.Admin {
		t.Fatalf("reset: %+v %v", account, err)
	}
	w = identityRequest(handler, "POST", "/api/v2/password-reset/lookup", `{"token":"`+reset+`"}`, nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"lead@example.com"`) {
		t.Fatalf("reset lookup: %d %s", w.Code, w.Body.String())
	}
	w = identityRequest(handler, "POST", "/api/v2/password-reset", `{"token":"`+reset+`","password":"a new password value"}`, nil, "", "")
	if w.Code != 204 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	if w = identityRequest(handler, "POST", "/api/v2/password-reset", `{"token":"`+reset+`","password":"another password value"}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("reset reused: %d", w.Code)
	}
	if w = identityRequest(handler, "GET", "/api/v2/contexts", "", cookie, "", ""); w.Code != 401 {
		t.Fatalf("reset kept the session: %d", w.Code)
	}
	if w = identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"lead@example.com","password":"a new password value"}`, nil, "", ""); w.Code != 200 {
		t.Fatalf("login with reset password: %d %s", w.Code, w.Body.String())
	}
}

// Links opened from email or other sites are cross-site top-level navigations;
// they load pages, never APIs, embedded frames or unsafe methods.
func TestCrossSiteNavigationOpensPagesOnly(t *testing.T) {
	_, handler := identityHandler(t, Settings{})
	for _, test := range []struct {
		method, path, dest string
		want               int
	}{
		{"GET", "/", "document", 200}, {"GET", "/reset-password", "document", 200},
		{"GET", "/api/v2/bootstrap", "document", 403}, {"GET", "/mcp", "document", 403}, {"GET", "/control/x", "document", 403},
		{"GET", "/", "iframe", 403}, {"POST", "/", "document", 403},
	} {
		r := httptest.NewRequest(test.method, test.path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", test.dest)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Errorf("%s %s dest=%s: %d, want %d", test.method, test.path, test.dest, w.Code, test.want)
		}
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
	// Without a trusted client IP header the server cannot tell clients apart,
	// so it never limits attempts.
	for i := 0; i < 25; i++ {
		if w = identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"alice","password":"wrong"}`, nil, "", ""); w.Code != 401 {
			t.Fatalf("unlimited attempt %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
}

// Replicas share one database, so attempts count against every replica.
func TestAuthRateLimitIsSharedAndNeedsClientIP(t *testing.T) {
	url := pgtest.URL(t)
	handlers := make([]http.Handler, 2)
	var store *pgstore.Store
	for i := range handlers {
		replica, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = replica.Close() })
		handler, closeServices, err := Handler(t.Context(), replica, Settings{ClientIPHeader: "X-Real-IP"}, fstest.MapFS{"index.html": {Data: []byte("app shell")}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeServices() })
		handlers[i], store = handler, replica
	}
	testsupport.RemoteAdmin(t, store, "alice", testPassword)
	login := func(handler http.Handler, clientIP ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v2/auth/login", strings.NewReader(`{"username":"alice","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		for _, value := range clientIP {
			r.Header.Add("X-Real-IP", value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, clientIP := range [][]string{nil, {""}, {"not-an-ip"}, {"192.0.2.1, 192.0.2.2"}, {"192.0.2.1", "192.0.2.2"}, {"192.0.2.1:443"}, {"[2001:db8::1]"}, {"fe80::1%eth0"}} {
		if w := login(handlers[0], clientIP...); w.Code != 400 || !strings.Contains(w.Body.String(), `"status":400`) {
			t.Fatalf("client IP %q: %d %s", clientIP, w.Code, w.Body.String())
		}
	}
	for i := range authhttp.AttemptLimit {
		if w := login(handlers[i%2], "192.0.2.1"); w.Code != 401 {
			t.Fatalf("attempt %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	// The IPv4-mapped form is the same client.
	w := login(handlers[1], "::ffff:192.0.2.1")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || !strings.Contains(w.Body.String(), "Too many authentication attempts") {
		t.Fatalf("attempts beyond limit: %d %s", w.Code, w.Body.String())
	}
	if w := login(handlers[0], "192.0.2.2"); w.Code != 401 {
		t.Fatalf("other client limited: %d %s", w.Code, w.Body.String())
	}
	// Reading the bootstrap verifies no secret and is not limited.
	r := httptest.NewRequest("GET", "/api/v2/bootstrap", nil)
	w = httptest.NewRecorder()
	handlers[0].ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body.String())
	}
}
func TestRevocationAndExpiryCloseActiveStreams(t *testing.T) {
	for _, action := range []string{"logout", "reset", "revoke", "expire"} {
		t.Run(action, func(t *testing.T) {
			store, handler := identityHandler(t, Settings{InsecureCookies: true})
			testsupport.RemoteAdmin(t, store, "alice", testPassword)
			identity := testsupport.Identity(t, store)
			session, err := identity.Login(t.Context(), identityservice.AudienceMembers, "alice", testPassword)
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
				r.AddCookie(&http.Cookie{Name: authhttp.SessionCookie, Value: session.Secret})
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
				var secret string
				if _, secret, err = identity.ResetPasswordByUsername(t.Context(), "alice"); err == nil {
					err = identity.CompletePasswordReset(t.Context(), identityservice.AudienceMembers, secret, "reset-password-123")
				}
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
