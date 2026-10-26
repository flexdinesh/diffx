package remoteserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport"
)

// fakeGoogle issues ID tokens for codes; claims per code are set by the test.
// It echoes the nonce from the authorization a code was issued for.
type fakeGoogle struct {
	mu     sync.Mutex
	server *httptest.Server
	claims map[string]map[string]any
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	fake := &fakeGoogle{claims: make(map[string]map[string]any)}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client_secret") != "google-secret" || r.PostForm.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		claims, ok := fake.claims[r.PostForm.Get("code")]
		fake.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeGoogle) settings() *GoogleSettings {
	return &GoogleSettings{ClientID: "google-client", ClientSecret: "google-secret", authorizeURL: fake.server.URL + "/auth", tokenURL: fake.server.URL + "/token"}
}

// authorize records what Google would return for the started flow: a code
// whose ID token carries this subject and email and the flow's nonce.
func (fake *fakeGoogle) authorize(t *testing.T, authorizeURL, code, subject, email string, verified bool) string {
	t.Helper()
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("redirect_uri") != "https://diffx.example.com"+googleCallback || query.Get("client_id") != "google-client" {
		t.Fatalf("authorize URL: %s", authorizeURL)
	}
	fake.mu.Lock()
	fake.claims[code] = map[string]any{"iss": "https://accounts.google.com", "aud": "google-client", "exp": time.Now().Add(time.Hour).Unix(),
		"nonce": query.Get("nonce"), "sub": subject, "email": email, "email_verified": verified}
	fake.mu.Unlock()
	return query.Get("state")
}

// startGoogle begins sign-in and returns the authorization URL and flow cookie.
func startGoogle(t *testing.T, handler http.Handler, path, body string) (string, *http.Cookie) {
	t.Helper()
	w := identityRequest(handler, "POST", path, body, nil, "", "")
	if w.Code != 200 {
		t.Fatalf("start %s: %d %s", path, w.Code, w.Body.String())
	}
	var started struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == flowCookie {
			if !cookie.HttpOnly || cookie.Path != googleCallback || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("flow cookie: %+v", cookie)
			}
			return started.AuthorizeURL, cookie
		}
	}
	t.Fatal("flow cookie missing")
	return "", nil
}

// callback follows Google's cross-site redirect back to diffx.
func callback(handler http.Handler, state, code string, flow *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", googleCallback+"?"+url.Values{"state": {state}, "code": {code}}.Encode(), nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if flow != nil {
		r.AddCookie(&http.Cookie{Name: flowCookie, Value: flow.Value})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func sessionFrom(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("sign-in redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func createInvite(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, email string) string {
	t.Helper()
	w := identityRequest(handler, "POST", "/api/v2/admin/invites", `{"email":"`+email+`"}`, cookie, csrf, "")
	if w.Code != 201 {
		t.Fatalf("create invite: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Invite inviteJSON `json:"invite"`
		URL    string     `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	link, secret, found := strings.Cut(created.URL, "/invite#t=")
	if !found || link != "https://diffx.example.com" || created.Invite.Status != "pending" || created.Invite.ExpiresAt-created.Invite.CreatedAt != (48*time.Hour).Milliseconds() {
		t.Fatalf("invite: %+v", created)
	}
	return secret
}

func TestInviteSignupThenGoogleSignInByStableSubject(t *testing.T) {
	fake := newFakeGoogle(t)
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()})
	testsupport.RemoteAdmin(t, store, "admin", testPassword)
	admin, csrf := loginIdentity(t, handler, "admin")
	if w := identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", ""); !strings.Contains(w.Body.String(), `"googleEnabled":true`) {
		t.Fatalf("bootstrap: %s", w.Body.String())
	}
	secret := createInvite(t, handler, admin, csrf, "Alice.Smith@Example.com")

	w := identityRequest(handler, "POST", "/api/v2/invites/lookup", `{"token":"`+secret+`"}`, nil, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"email":"alice.smith@example.com"`) {
		t.Fatalf("lookup: %d %s", w.Code, w.Body.String())
	}
	authorize, flow := startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+secret+`"}`)
	state := fake.authorize(t, authorize, "unverified", "subject-a", "alice.smith@example.com", false)
	if w := callback(handler, state, "unverified", flow); w.Header().Get("Location") != "/?signin=invite-mismatch" {
		t.Fatalf("unverified email accepted: %s", w.Header().Get("Location"))
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+secret+`"}`)
	state = fake.authorize(t, authorize, "other-email", "subject-a", "mallory@example.com", true)
	if w := callback(handler, state, "other-email", flow); w.Header().Get("Location") != "/?signin=invite-mismatch" {
		t.Fatalf("other email accepted: %s", w.Header().Get("Location"))
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+secret+`"}`)
	state = fake.authorize(t, authorize, "accept", "subject-a", "Alice.Smith@example.com", true)
	if w := callback(handler, state, "accept", nil); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("callback without flow cookie: %s", w.Header().Get("Location"))
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+secret+`"}`)
	state = fake.authorize(t, authorize, "accept", "subject-a", "Alice.Smith@example.com", true)
	member := sessionFrom(t, callback(handler, state, "accept", flow))
	if w := callback(handler, state, "accept", flow); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("state reused: %s", w.Header().Get("Location"))
	}
	w = identityRequest(handler, "GET", "/api/v2/bootstrap", "", member, "", "")
	if !strings.Contains(w.Body.String(), `"username":"alice.smith","email":"alice.smith@example.com","admin":false`) {
		t.Fatalf("member identity: %s", w.Body.String())
	}
	if w := identityRequest(handler, "POST", "/api/v2/invites/lookup", `{"token":"`+secret+`"}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("used invite still pending: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/invites", `{"email":"alice.smith@example.com"}`, admin, csrf, ""); w.Code != 409 {
		t.Fatalf("invited an existing account's email: %d", w.Code)
	}

	// The same Google account signs in after changing its email.
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "renamed", "subject-a", "alice@new.example.com", true)
	again := sessionFrom(t, callback(handler, state, "renamed", flow))
	w = identityRequest(handler, "GET", "/api/v2/bootstrap", "", again, "", "")
	if !strings.Contains(w.Body.String(), `"username":"alice.smith","email":"alice@new.example.com"`) {
		t.Fatalf("email change changed account: %s", w.Body.String())
	}
	w = identityRequest(handler, "GET", "/api/v2/admin/users", "", admin, "", "")
	if !strings.Contains(w.Body.String(), `"email":"alice@new.example.com"`) {
		t.Fatalf("email change not recorded: %s", w.Body.String())
	}

	// A Google account without an account or invite cannot sign up.
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "stranger", "subject-b", "stranger@example.com", true)
	if w := callback(handler, state, "stranger", flow); w.Header().Get("Location") != "/?signin=no-account" {
		t.Fatalf("uninvited Google account: %s", w.Header().Get("Location"))
	}
	if w := identityRequest(handler, "POST", "/api/v2/auth/login", `{"username":"alice.smith","password":""}`, nil, "", ""); w.Code != 401 {
		t.Fatalf("member password login: %d", w.Code)
	}
}

func TestInviteAdministrationAndDisabledGoogleAccount(t *testing.T) {
	fake := newFakeGoogle(t)
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()})
	testsupport.RemoteAdmin(t, store, "admin", testPassword)
	admin, csrf := loginIdentity(t, handler, "admin")
	first := createInvite(t, handler, admin, csrf, "bob@example.com")
	second := createInvite(t, handler, admin, csrf, "bob@example.com")
	if w := identityRequest(handler, "POST", "/api/v2/invites/google", `{"token":"`+first+`"}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("replaced invite started sign-in: %d", w.Code)
	}
	w := identityRequest(handler, "GET", "/api/v2/admin/invites", "", admin, "", "")
	var listed struct {
		Invites []inviteJSON `json:"invites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Invites) != 2 || listed.Invites[0].Status == listed.Invites[1].Status {
		t.Fatalf("invites: %s %v", w.Body.String(), err)
	}
	if listed.Invites[0].Status != "pending" {
		listed.Invites[0], listed.Invites[1] = listed.Invites[1], listed.Invites[0]
	}
	if listed.Invites[1].Status != "revoked" {
		t.Fatalf("replaced invite: %+v", listed.Invites)
	}
	if strings.Contains(w.Body.String(), second) {
		t.Fatal("invite listing leaks the secret")
	}
	memberCookie, memberCSRF := memberSession(t, store, "member")
	for _, request := range []struct {
		method, path, body string
	}{{"GET", "/api/v2/admin/invites", ""}, {"POST", "/api/v2/admin/invites", `{"email":"x@example.com"}`}, {"DELETE", "/api/v2/admin/invites/" + listed.Invites[0].ID, ""}} {
		if w := identityRequest(handler, request.method, request.path, request.body, memberCookie, memberCSRF, ""); w.Code != 403 {
			t.Fatalf("member %s %s: %d", request.method, request.path, w.Code)
		}
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/invites", `{"email":"not-an-email"}`, admin, csrf, ""); w.Code != 400 {
		t.Fatalf("invalid email: %d", w.Code)
	}
	authorize, flow := startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+second+`"}`)
	if w := identityRequest(handler, "DELETE", "/api/v2/admin/invites/"+listed.Invites[0].ID, "", admin, csrf, ""); w.Code != 204 {
		t.Fatalf("revoke: %d", w.Code)
	}
	state := fake.authorize(t, authorize, "late", "subject-bob", "bob@example.com", true)
	if w := callback(handler, state, "late", flow); w.Header().Get("Location") != "/?signin=invite-invalid" {
		t.Fatalf("revoked invite accepted: %s", w.Header().Get("Location"))
	}

	bob := createInvite(t, handler, admin, csrf, "bob@example.com")
	authorize, flow = startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+bob+`"}`)
	state = fake.authorize(t, authorize, "bob", "subject-bob", "bob@example.com", true)
	sessionFrom(t, callback(handler, state, "bob", flow))
	w = identityRequest(handler, "GET", "/api/v2/admin/users", "", admin, "", "")
	var users struct {
		Users []adminUserJSON `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	var bobID string
	for _, user := range users.Users {
		if user.Username == "bob" {
			bobID = user.ID
		}
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/users/"+bobID+"/disable", "", admin, csrf, ""); w.Code != 204 {
		t.Fatalf("disable: %d", w.Code)
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "disabled", "subject-bob", "bob@example.com", true)
	if w := callback(handler, state, "disabled", flow); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("disabled account signed in: %s", w.Header().Get("Location"))
	}
}

func TestGoogleRoutesAbsentWithoutConfiguration(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	testsupport.RemoteAdmin(t, store, "admin", testPassword)
	admin, csrf := loginIdentity(t, handler, "admin")
	for _, path := range []string{"/api/v2/auth/google", "/api/v2/invites/google", "/api/v2/invites/lookup"} {
		if w := identityRequest(handler, "POST", path, `{"token":"`+strings.Repeat("x", 43)+`"}`, nil, "", ""); w.Code != 404 && w.Code != 400 {
			t.Fatalf("unconfigured %s: %d", path, w.Code)
		}
	}
	if w := identityRequest(handler, "POST", "/api/v2/auth/google", `{}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("unconfigured Google start: %d", w.Code)
	}
	if w := callback(handler, "state", "code", nil); w.Code != 404 {
		t.Fatalf("unconfigured callback: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/invites", `{"email":"a@example.com"}`, admin, csrf, ""); w.Code != 409 {
		t.Fatalf("invite without Google: %d", w.Code)
	}
	if w := identityRequest(handler, "GET", "/api/v2/admin/invites", "", admin, "", ""); w.Code != 200 {
		t.Fatalf("invite listing without Google: %d", w.Code)
	}
}
