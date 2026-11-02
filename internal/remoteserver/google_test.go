package remoteserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
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
		if cookie.Name == authhttp.SessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

// createInvite issues an invite as the console does and returns its secret.
func createInvite(t *testing.T, store *pgstore.Store, email string) string {
	t.Helper()
	_, secret, err := identityservice.New(store).CreateInvite(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestInviteSignupThenGoogleSignInByStableSubject(t *testing.T) {
	fake := newFakeGoogle(t)
	// Sign-up is closed: only invites create accounts.
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()}, identityservice.Settings{PasswordSignup: true})
	identity := identityservice.New(store)
	if w := identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", ""); !strings.Contains(w.Body.String(), `"googleEnabled":true,"signupEnabled":false`) {
		t.Fatalf("bootstrap: %s", w.Body.String())
	}
	secret := createInvite(t, store, "Alice.Smith@Example.com")

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
	if !strings.Contains(w.Body.String(), `"email":"alice.smith@example.com","method":"google"`) {
		t.Fatalf("member identity: %s", w.Body.String())
	}
	if w := identityRequest(handler, "POST", "/api/v2/invites/lookup", `{"token":"`+secret+`"}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("used invite still pending: %d", w.Code)
	}
	if _, _, err := identity.CreateInvite(t.Context(), "alice.smith@example.com"); !errors.Is(err, identityservice.ErrConflict) {
		t.Fatalf("invited an existing account's email: %v", err)
	}

	// The same Google account signs in after changing its email.
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "renamed", "subject-a", "alice@new.example.com", true)
	again := sessionFrom(t, callback(handler, state, "renamed", flow))
	w = identityRequest(handler, "GET", "/api/v2/bootstrap", "", again, "", "")
	if !strings.Contains(w.Body.String(), `"email":"alice@new.example.com"`) {
		t.Fatalf("email change changed account: %s", w.Body.String())
	}
	if summaries, err := identity.AccountSummaries(t.Context()); err != nil || len(summaries) != 1 || summaries[0].Email != "alice@new.example.com" {
		t.Fatalf("email change not recorded: %+v %v", summaries, err)
	}

	// A Google account without an account or invite cannot sign up.
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "stranger", "subject-b", "stranger@example.com", true)
	if w := callback(handler, state, "stranger", flow); w.Header().Get("Location") != "/?signin=no-account" {
		t.Fatalf("uninvited Google account: %s", w.Header().Get("Location"))
	}
	if w := identityRequest(handler, "POST", "/api/v2/auth/login", `{"email":"alice.smith@example.com","password":""}`, nil, "", ""); w.Code != 401 {
		t.Fatalf("Google account password login: %d", w.Code)
	}
}

func TestOpenSignupCreatesVerifiedGoogleAccounts(t *testing.T) {
	fake := newFakeGoogle(t)
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()}, identityservice.Settings{Signup: true})
	authorize, flow := startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state := fake.authorize(t, authorize, "unverified", "subject-new", "new@example.com", false)
	if w := callback(handler, state, "unverified", flow); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("unverified email signed up: %s", w.Header().Get("Location"))
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "verified", "subject-new", "New.Person@Example.com", true)
	session := sessionFrom(t, callback(handler, state, "verified", flow))
	w := identityRequest(handler, "GET", "/api/v2/bootstrap", "", session, "", "")
	if !strings.Contains(w.Body.String(), `"email":"new.person@example.com","method":"google"`) {
		t.Fatalf("signed-up identity: %s", w.Body.String())
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "again", "subject-new", "new.person@example.com", true)
	sessionFrom(t, callback(handler, state, "again", flow))
	if summaries, err := identityservice.New(store).AccountSummaries(t.Context()); err != nil || len(summaries) != 1 || summaries[0].Admin {
		t.Fatalf("sign-up accounts: %+v %v", summaries, err)
	}
}

// An email identifies one account, so Google never signs up with a password
// account's email, nor links to it.
func TestGoogleSignupRefusesTakenEmail(t *testing.T) {
	fake := newFakeGoogle(t)
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()}, identityservice.Settings{Signup: true, PasswordSignup: true})
	testsupport.RemotePasswordAccount(t, store, "taken@example.com", testPassword)
	authorize, flow := startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state := fake.authorize(t, authorize, "taken", "subject-taken", "Taken@Example.com", true)
	if w := callback(handler, state, "taken", flow); w.Header().Get("Location") != "/?signin=email-taken" {
		t.Fatalf("taken email: %s", w.Header().Get("Location"))
	}
	if summaries, err := identityservice.New(store).AccountSummaries(t.Context()); err != nil || len(summaries) != 1 || summaries[0].Method != identityservice.MethodPassword {
		t.Fatalf("accounts: %+v %v", summaries, err)
	}
}

func TestReplacedRevokedInvitesAndDisabledGoogleAccounts(t *testing.T) {
	fake := newFakeGoogle(t)
	store, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()}, identityservice.Settings{PasswordSignup: true})
	identity := identityservice.New(store)
	first := createInvite(t, store, "bob@example.com")
	_, second, err := identity.CreateInvite(t.Context(), "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if w := identityRequest(handler, "POST", "/api/v2/invites/google", `{"token":"`+first+`"}`, nil, "", ""); w.Code != 404 {
		t.Fatalf("replaced invite started sign-in: %d", w.Code)
	}
	invites, err := identity.Invites(t.Context())
	if err != nil || len(invites) != 2 {
		t.Fatalf("invites: %+v %v", invites, err)
	}
	authorize, flow := startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+second+`"}`)
	for _, invite := range invites {
		if invite.Status(time.Now()) == identityservice.InvitePending {
			if err := identity.RevokeInvite(t.Context(), invite.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	state := fake.authorize(t, authorize, "late", "subject-bob", "bob@example.com", true)
	if w := callback(handler, state, "late", flow); w.Header().Get("Location") != "/?signin=invite-invalid" {
		t.Fatalf("revoked invite accepted: %s", w.Header().Get("Location"))
	}

	bob := createInvite(t, store, "bob@example.com")
	authorize, flow = startGoogle(t, handler, "/api/v2/invites/google", `{"token":"`+bob+`"}`)
	state = fake.authorize(t, authorize, "bob", "subject-bob", "bob@example.com", true)
	sessionFrom(t, callback(handler, state, "bob", flow))
	summaries, err := identity.AccountSummaries(t.Context())
	if err != nil || len(summaries) != 1 {
		t.Fatalf("accounts: %+v %v", summaries, err)
	}
	if err := identity.DisableAccount(t.Context(), "admin", summaries[0].ID); err != nil {
		t.Fatal(err)
	}
	authorize, flow = startGoogle(t, handler, "/api/v2/auth/google", `{}`)
	state = fake.authorize(t, authorize, "disabled", "subject-bob", "bob@example.com", true)
	if w := callback(handler, state, "disabled", flow); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("disabled account signed in: %s", w.Header().Get("Location"))
	}
}

func TestGoogleRoutesAbsentWithoutConfiguration(t *testing.T) {
	_, handler := identityHandler(t, Settings{})
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
}

// nextFrom returns the return-page cookie a Google start set.
func nextFrom(t *testing.T, handler http.Handler, body string) (string, *http.Cookie, *http.Cookie) {
	t.Helper()
	w := identityRequest(handler, "POST", "/api/v2/auth/google", body, nil, "", "")
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var started struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	var flow, next *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		switch cookie.Name {
		case flowCookie:
			flow = cookie
		case nextCookie:
			next = cookie
		}
	}
	if flow == nil || next == nil || next.Path != googleCallback || !next.HttpOnly {
		t.Fatalf("start cookies: flow %+v next %+v", flow, next)
	}
	return started.AuthorizeURL, flow, next
}

func callbackWith(handler http.Handler, state, code string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", googleCallback+"?"+url.Values{"state": {state}, "code": {code}}.Encode(), nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	for _, cookie := range cookies {
		r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestGoogleSignInReturnsOnlyToListedPages(t *testing.T) {
	fake := newFakeGoogle(t)
	_, handler := identityHandler(t, Settings{PublicURL: "https://diffx.example.com", Google: fake.settings()}, identityservice.Settings{Signup: true})
	for _, next := range []string{"/", "https://evil.example.com", "//evil.example.com", "/settings/github/../tokens"} {
		if w := identityRequest(handler, "POST", "/api/v2/auth/google", `{"next":"`+next+`"}`, nil, "", ""); w.Code != 400 {
			t.Fatalf("next %q: %d", next, w.Code)
		}
	}
	authorize, flow, next := nextFrom(t, handler, `{"next":"/settings/github"}`)
	if next.Value != "/settings/github" || next.MaxAge <= 0 {
		t.Fatalf("next cookie: %+v", next)
	}
	state := fake.authorize(t, authorize, "unverified", "subject-next", "next@example.com", false)
	if w := callbackWith(handler, state, "unverified", flow, next); w.Header().Get("Location") != "/?signin=failed" {
		t.Fatalf("failed sign-in returned to the page: %s", w.Header().Get("Location"))
	}
	authorize, flow, next = nextFrom(t, handler, `{"next":"/settings/github"}`)
	state = fake.authorize(t, authorize, "verified", "subject-next", "next@example.com", true)
	w := callbackWith(handler, state, "verified", flow, next)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/settings/github" {
		t.Fatalf("sign-in redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		cleared = cleared || cookie.Name == nextCookie && cookie.MaxAge < 0
	}
	if !cleared {
		t.Fatal("callback kept the return page")
	}
	// A start without a page clears an earlier one, and a forged page is ignored.
	authorize, flow, next = nextFrom(t, handler, `{}`)
	if next.MaxAge >= 0 {
		t.Fatalf("start without next kept a page: %+v", next)
	}
	state = fake.authorize(t, authorize, "forged", "subject-next", "next@example.com", true)
	sessionFrom(t, callbackWith(handler, state, "forged", flow, &http.Cookie{Name: nextCookie, Value: "https://evil.example.com"}))
}
