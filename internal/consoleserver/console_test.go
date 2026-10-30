package consoleserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

const testPassword = "console-password-000001"

var testSettings = Settings{PublicURL: "https://console.example.com", AppURL: "https://diffx.example.com"}

func consoleHandler(t *testing.T, settings Settings) (*pgstore.Store, http.Handler) {
	t.Helper()
	store := pgtest.Store(t)
	return store, Handler(store, settings, fstest.MapFS{"index.html": {Data: []byte("console shell")}, "assets/console.js": {Data: []byte("console()")}})
}

func request(handler http.Handler, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return value
}

// signedIn returns the session cookie and CSRF token from a sign-in response.
func signedIn(t *testing.T, w *httptest.ResponseRecorder) (*http.Cookie, string) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("sign-in: %d %s", w.Code, w.Body.String())
	}
	result := decode[bootstrapJSON](t, w)
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == authhttp.SessionCookie && cookie.Value != "" && result.Admin != nil && result.CSRFToken != nil {
			return cookie, *result.CSRFToken
		}
	}
	t.Fatalf("no session: %s", w.Body.String())
	return nil, ""
}

func login(t *testing.T, handler http.Handler, username string) (*http.Cookie, string) {
	t.Helper()
	return signedIn(t, request(handler, "POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"`+testPassword+`"}`, nil, ""))
}

// passwordMember signs up a password member, as the app does.
func passwordMember(t *testing.T, store *pgstore.Store, username string) identityservice.Account {
	t.Helper()
	session, err := identityservice.New(store, identityservice.WithMembers(identityservice.MemberPolicy{Signup: true, Password: true})).SignUp(t.Context(), username, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return session.Account
}

func accountID(t *testing.T, handler http.Handler, cookie *http.Cookie, username string) string {
	t.Helper()
	w := request(handler, "GET", "/api/v1/accounts", "", cookie, "")
	if w.Code != 200 {
		t.Fatalf("accounts: %d %s", w.Code, w.Body.String())
	}
	for _, account := range decode[struct{ Accounts []accountJSON }](t, w).Accounts {
		if account.Username == username {
			return account.ID
		}
	}
	t.Fatalf("no account %s", username)
	return ""
}

func TestSetupCreatesTheFirstAdminOnce(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	if result := decode[bootstrapJSON](t, request(handler, "GET", "/api/v1/bootstrap", "", nil, "")); !result.SetupRequired || result.Admin != nil || result.GoogleEnabled {
		t.Fatalf("fresh bootstrap: %+v", result)
	}
	// Members may exist before any admin; setup still creates the first admin.
	passwordMember(t, store, "member")
	secret, err := identityservice.New(store).PrepareSetup(t.Context())
	if err != nil || secret == "" {
		t.Fatalf("prepare: %v", err)
	}
	if w := request(handler, "POST", "/api/v1/setup", `{"token":"wrong","username":"root","password":"`+testPassword+`"}`, nil, ""); w.Code != 404 {
		t.Fatalf("wrong secret: %d", w.Code)
	}
	cookie, _ := signedIn(t, request(handler, "POST", "/api/v1/setup", `{"token":"`+secret+`","username":"root","password":"`+testPassword+`"}`, nil, ""))
	result := decode[bootstrapJSON](t, request(handler, "GET", "/api/v1/bootstrap", "", cookie, ""))
	if result.SetupRequired || result.Admin == nil || result.Admin.Username != "root" || result.Admin.Method != identityservice.MethodPassword {
		t.Fatalf("admin bootstrap: %+v", result)
	}
	if w := request(handler, "POST", "/api/v1/setup", `{"token":"`+secret+`","username":"second","password":"`+testPassword+`"}`, nil, ""); w.Code != 404 {
		t.Fatalf("reused secret: %d", w.Code)
	}
	if again, err := identityservice.New(store).PrepareSetup(t.Context()); err != nil || again != "" {
		t.Fatalf("setup after admin: %q %v", again, err)
	}
}

func TestConsoleAdmitsOnlyAdmins(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	passwordMember(t, store, "member")
	if w := request(handler, "POST", "/api/v1/auth/login", `{"username":"member","password":"`+testPassword+`"}`, nil, ""); w.Code != 401 {
		t.Fatalf("member login: %d", w.Code)
	}
	// A member's app session never authenticates here.
	member := testsupport.RemoteSession(t, store, testsupport.RemoteAccount(t, store, "google-member"))
	memberCookie := &http.Cookie{Name: authhttp.SessionCookie, Value: member.Secret}
	w := request(handler, "GET", "/api/v1/bootstrap", "", memberCookie, "")
	if result := decode[bootstrapJSON](t, w); result.Admin != nil || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("member bootstrap: %+v %s", result, w.Header().Get("Set-Cookie"))
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", memberCookie, ""); w.Code != 401 {
		t.Fatalf("member accounts: %d", w.Code)
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous accounts: %d", w.Code)
	}

	// A promoted member signs in; once demoted, its console session ends.
	root, rootCSRF := login(t, handler, "root")
	memberID := accountID(t, handler, root, "member")
	if w := request(handler, "PUT", "/api/v1/accounts/"+memberID+"/admin", `{"admin":true}`, root, rootCSRF); w.Code != 204 {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	promoted, _ := login(t, handler, "member")
	if w := request(handler, "PUT", "/api/v1/accounts/"+memberID+"/admin", `{"admin":false}`, root, rootCSRF); w.Code != 204 {
		t.Fatalf("demote: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", promoted, ""); w.Code != 401 {
		t.Fatalf("demoted session: %d", w.Code)
	}
}

func TestAdminActionsOnAccounts(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	passwordMember(t, store, "member")
	root, csrf := login(t, handler, "root")
	rootID, memberID := accountID(t, handler, root, "root"), accountID(t, handler, root, "member")
	for _, path := range []string{"/disable", "/enable", "/admin", ""} {
		method, body := "POST", ""
		switch path {
		case "/admin":
			method, body = "PUT", `{"admin":false}`
		case "":
			method = "DELETE"
		}
		if w := request(handler, method, "/api/v1/accounts/"+rootID+path, body, root, csrf); w.Code != 403 {
			t.Fatalf("self %s: %d", path, w.Code)
		}
	}
	if w := request(handler, "POST", "/api/v1/accounts/"+memberID+"/disable", "", root, ""); w.Code != 403 {
		t.Fatalf("missing CSRF: %d", w.Code)
	}
	if w := request(handler, "PUT", "/api/v1/accounts/"+memberID+"/admin", `{}`, root, csrf); w.Code != 400 {
		t.Fatalf("missing role: %d", w.Code)
	}
	if w := request(handler, "GET", "/api/v1/accounts/"+memberID+"/disable", "", root, csrf); w.Code != 405 {
		t.Fatalf("wrong method: %d", w.Code)
	}
	if w := request(handler, "POST", "/api/v1/accounts/"+memberID+"/disable", "", root, csrf); w.Code != 204 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	accounts := decode[struct{ Accounts []accountJSON }](t, request(handler, "GET", "/api/v1/accounts", "", root, ""))
	for _, account := range accounts.Accounts {
		if account.ID == memberID && (!account.Disabled || account.Admin || account.Method != identityservice.MethodPassword) ||
			account.ID == rootID && (account.Disabled || !account.Admin) {
			t.Fatalf("accounts: %+v", accounts)
		}
	}
	if w := request(handler, "DELETE", "/api/v1/accounts/"+memberID, "", root, csrf); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := request(handler, "DELETE", "/api/v1/accounts/"+memberID, "", root, csrf); w.Code != 404 {
		t.Fatalf("delete again: %d", w.Code)
	}
}

func TestInvitesLinkToTheApp(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	testsupport.RemoteAccount(t, store, "taken")
	root, csrf := login(t, handler, "root")
	if w := request(handler, "POST", "/api/v1/invites", `{"email":"new@example.com"}`, root, csrf); w.Code != 400 {
		t.Fatalf("missing role: %d", w.Code)
	}
	if w := request(handler, "POST", "/api/v1/invites", `{"email":"taken@example.com","admin":false}`, root, csrf); w.Code != 409 {
		t.Fatalf("taken email: %d", w.Code)
	}
	w := request(handler, "POST", "/api/v1/invites", `{"email":"new@example.com","admin":true}`, root, csrf)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decode[struct {
		Invite inviteJSON
		URL    string
	}](t, w)
	if !strings.HasPrefix(created.URL, "https://diffx.example.com/invite#t=") || !created.Invite.Admin || created.Invite.Status != "pending" {
		t.Fatalf("invite: %+v", created)
	}
	listed := decode[struct{ Invites []inviteJSON }](t, request(handler, "GET", "/api/v1/invites", "", root, ""))
	if len(listed.Invites) != 1 || listed.Invites[0].ID != created.Invite.ID || strings.Contains(w.Body.String(), "token_hash") {
		t.Fatalf("list: %+v", listed)
	}
	if w := request(handler, "DELETE", "/api/v1/invites/"+created.Invite.ID, "", root, csrf); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := request(handler, "DELETE", "/api/v1/invites/"+created.Invite.ID, "", root, csrf); w.Code != 404 {
		t.Fatalf("revoke again: %d", w.Code)
	}
}

func TestResetLinksOpenWhereTheAccountSignsIn(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	passwordMember(t, store, "member")
	passwordMember(t, store, "deputy")
	testsupport.RemoteAccount(t, store, "google")
	root, csrf := login(t, handler, "root")
	deputyID := accountID(t, handler, root, "deputy")
	if w := request(handler, "PUT", "/api/v1/accounts/"+deputyID+"/admin", `{"admin":true}`, root, csrf); w.Code != 204 {
		t.Fatalf("promote: %d", w.Code)
	}
	reset := func(username string) (int, string) {
		w := request(handler, "POST", "/api/v1/accounts/"+accountID(t, handler, root, username)+"/password-reset", "", root, csrf)
		if w.Code != 201 {
			return w.Code, ""
		}
		link := decode[struct {
			URL       string
			ExpiresAt int64
		}](t, w)
		if remaining := time.Until(time.UnixMilli(link.ExpiresAt)); remaining < 23*time.Hour || remaining > identityservice.PasswordResetLifetime {
			t.Fatalf("expiry: %v", remaining)
		}
		return w.Code, link.URL
	}
	if code, link := reset("member"); code != 201 || !strings.HasPrefix(link, "https://diffx.example.com/reset-password#t=") {
		t.Fatalf("member link: %d %s", code, link)
	}
	if code, _ := reset("google"); code != 404 {
		t.Fatalf("google link: %d", code)
	}
	_, link := reset("deputy")
	if !strings.HasPrefix(link, "https://console.example.com/reset-password#t=") {
		t.Fatalf("admin link: %s", link)
	}
	deputy, _ := login(t, handler, "deputy")
	secret := strings.TrimPrefix(link, "https://console.example.com/reset-password#t=")
	lookup := request(handler, "POST", "/api/v1/password-reset/lookup", `{"token":"`+secret+`"}`, nil, "")
	if lookup.Code != 200 || decode[struct{ Username string }](t, lookup).Username != "deputy" {
		t.Fatalf("lookup: %d %s", lookup.Code, lookup.Body.String())
	}
	if w := request(handler, "POST", "/api/v1/password-reset", `{"token":"`+secret+`","password":"console-password-000002"}`, nil, ""); w.Code != 204 {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", deputy, ""); w.Code != 401 {
		t.Fatalf("session after reset: %d", w.Code)
	}
	if w := request(handler, "POST", "/api/v1/password-reset", `{"token":"`+secret+`","password":"console-password-000003"}`, nil, ""); w.Code != 404 {
		t.Fatalf("reused link: %d", w.Code)
	}
	signedIn(t, request(handler, "POST", "/api/v1/auth/login", `{"username":"deputy","password":"console-password-000002"}`, nil, ""))
}

func TestPasswordChangeAndLogout(t *testing.T) {
	store, handler := consoleHandler(t, testSettings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	root, csrf := login(t, handler, "root")
	if w := request(handler, "PUT", "/api/v1/account/password", `{"currentPassword":"wrong-password-0001","newPassword":"console-password-000002"}`, root, csrf); w.Code != 403 {
		t.Fatalf("wrong current: %d", w.Code)
	}
	if w := request(handler, "PUT", "/api/v1/account/password", `{"currentPassword":"`+testPassword+`","newPassword":"console-password-000002"}`, root, csrf); w.Code != 204 {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", root, ""); w.Code != 401 {
		t.Fatalf("session after change: %d", w.Code)
	}
	again, csrf := signedIn(t, request(handler, "POST", "/api/v1/auth/login", `{"username":"root","password":"console-password-000002"}`, nil, ""))
	if w := request(handler, "POST", "/api/v1/auth/logout", "", again, csrf); w.Code != 204 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := request(handler, "GET", "/api/v1/accounts", "", again, ""); w.Code != 401 {
		t.Fatalf("session after logout: %d", w.Code)
	}
}

func TestTransportBoundaries(t *testing.T) {
	_, handler := consoleHandler(t, testSettings)
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"root","password":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	for path, want := range map[string]string{"/": "console shell", "/accounts": "console shell", "/assets/console.js": "console()"} {
		w := request(handler, "GET", path, "", nil, "")
		if w.Code != 200 || w.Body.String() != want || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s: %d %q", path, w.Code, w.Body.String())
		}
	}
	for path, want := range map[string]int{"/assets/missing.js": 404, "/api/v1/missing": 401, "/api/v1/auth/google": 404, "/api/v1/auth/google/callback": 404, "/readyz": 200} {
		method := "GET"
		if path == "/api/v1/auth/google" {
			method = "POST"
		}
		if w := request(handler, method, path, "{}", nil, ""); w.Code != want {
			t.Fatalf("%s: %d, want %d", path, w.Code, want)
		}
	}
}

// fakeGoogle issues ID tokens for codes; claims per code are set by the test.
type fakeGoogle struct {
	mu     sync.Mutex
	server *httptest.Server
	claims map[string]map[string]any
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	fake := &fakeGoogle{claims: make(map[string]map[string]any)}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client_secret") != "google-secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		claims, ok := fake.claims[r.PostForm.Get("code")]
		fake.mu.Unlock()
		payload, err := json.Marshal(claims)
		if !ok || err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// signIn runs the console's Google flow for subject and returns the callback response.
func (fake *fakeGoogle) signIn(t *testing.T, handler http.Handler, code, subject, email string) *httptest.ResponseRecorder {
	t.Helper()
	w := request(handler, "POST", "/api/v1/auth/google", "{}", nil, "")
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	authorize, err := url.Parse(decode[struct{ AuthorizeURL string }](t, w).AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authorize.Query()
	if query.Get("redirect_uri") != "https://console.example.com"+googleCallback {
		t.Fatalf("redirect: %s", query.Get("redirect_uri"))
	}
	fake.mu.Lock()
	fake.claims[code] = map[string]any{"iss": "https://accounts.google.com", "aud": "google-client", "exp": time.Now().Add(time.Hour).Unix(),
		"nonce": query.Get("nonce"), "sub": subject, "email": email, "email_verified": true}
	fake.mu.Unlock()
	var flow *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == flowCookie {
			flow = cookie
		}
	}
	if flow == nil || !flow.HttpOnly || flow.Path != googleCallback {
		t.Fatalf("flow cookie: %+v", flow)
	}
	r := httptest.NewRequest("GET", googleCallback+"?"+url.Values{"state": {query.Get("state")}, "code": {code}}.Encode(), nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(&http.Cookie{Name: flowCookie, Value: flow.Value})
	callback := httptest.NewRecorder()
	handler.ServeHTTP(callback, r)
	return callback
}

func TestGoogleSignsInExistingAdminsOnly(t *testing.T) {
	fake := newFakeGoogle(t)
	settings := testSettings
	settings.Google = &GoogleSettings{ClientID: "google-client", ClientSecret: "google-secret", authorizeURL: fake.server.URL + "/auth", tokenURL: fake.server.URL + "/token"}
	store, handler := consoleHandler(t, settings)
	testsupport.RemoteAdmin(t, store, "root", testPassword)
	member := testsupport.RemoteAccount(t, store, "member")
	if result := decode[bootstrapJSON](t, request(handler, "GET", "/api/v1/bootstrap", "", nil, "")); !result.GoogleEnabled {
		t.Fatal("Google not offered")
	}
	for code, test := range map[string]struct{ subject, email, location string }{
		"stranger": {"subject-unknown", "stranger@example.com", "/?signin=no-account"},
		"member":   {"subject-" + member.ID, "member@example.com", "/?signin=failed"},
	} {
		if w := fake.signIn(t, handler, code, test.subject, test.email); w.Code != http.StatusSeeOther || w.Header().Get("Location") != test.location {
			t.Fatalf("%s: %d %s", code, w.Code, w.Header().Get("Location"))
		}
	}
	if accounts, err := identityservice.New(store).AccountSummaries(t.Context()); err != nil || len(accounts) != 2 {
		t.Fatalf("console created an account: %+v %v", accounts, err)
	}
	root, csrf := login(t, handler, "root")
	if w := request(handler, "PUT", "/api/v1/accounts/"+member.ID+"/admin", `{"admin":true}`, root, csrf); w.Code != 204 {
		t.Fatalf("promote: %d", w.Code)
	}
	w := fake.signIn(t, handler, "admin", "subject-"+member.ID, "member@example.com")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("admin: %d %s", w.Code, w.Header().Get("Location"))
	}
	var session *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == authhttp.SessionCookie {
			session = cookie
		}
	}
	result := decode[bootstrapJSON](t, request(handler, "GET", "/api/v1/bootstrap", "", session, ""))
	if result.Admin == nil || result.Admin.Method != identityservice.MethodGoogle || result.Admin.Email == nil || *result.Admin.Email != "member@example.com" {
		t.Fatalf("Google admin: %+v", result)
	}
}
