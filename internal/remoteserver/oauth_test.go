package remoteserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/caller"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

const (
	oauthIssuer   = "https://diffx.example"
	oauthResource = oauthIssuer + "/mcp"
	cimdClient    = "https://claude.example/oauth/client.json"
	cimdRedirect  = "http://127.0.0.1/callback"
	verifier      = "a-proof-key-for-code-exchange-that-is-long-enough-1234"
)

// documents serves client metadata documents in place of the public network.
type documents map[string]string

func (docs documents) Fetch(_ context.Context, raw string) ([]byte, error) {
	document, ok := docs[raw]
	if !ok {
		return nil, errors.New("unavailable")
	}
	return []byte(document), nil
}

var testDocuments = documents{
	cimdClient:                         `{"client_id":"` + cimdClient + `","client_name":"Claude","redirect_uris":["` + cimdRedirect + `","https://claude.example/callback"],"token_endpoint_auth_method":"none"}`,
	"https://evil.example/client.json": `{"client_id":"https://other.example/client.json","client_name":"Impostor","redirect_uris":["https://evil.example/cb"]}`,
}

type oauthFixture struct {
	store   *pgstore.Store
	conn    *pgx.Conn
	handler http.Handler
	remote  *transport
	cookie  *http.Cookie
	csrf    string
	account identityservice.Account
}

func newOAuthFixture(t *testing.T, settings Settings) oauthFixture {
	t.Helper()
	url := pgtest.URL(t)
	store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	if settings.clientMetadata == nil {
		settings.clientMetadata = testDocuments
	}
	// The transport alone: these tests need no workers or replication.
	handler, _, closeServices, _, err := compose(t.Context(), store, fstest.MapFS{"index.html": {Data: []byte("app shell")}}, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	remote, ok := handler.(*transport)
	if !ok {
		t.Fatal("compose returned no transport")
	}
	account := testsupport.RemoteAccount(t, store, "alice")
	session := testsupport.RemoteSession(t, store, account)
	return oauthFixture{store: store, conn: conn, handler: remote, remote: remote, account: account, csrf: session.CSRFToken,
		cookie: &http.Cookie{Name: "diffx_session", Value: session.Secret}}
}

func (f oauthFixture) do(t *testing.T, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func challenge(v string) string {
	hash := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func authorizeQuery(clientID, redirect string, overrides map[string]string) string {
	values := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"xyz"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "resource": {oauthResource}}
	for name, value := range overrides {
		if value == "" {
			values.Del(name)
		} else {
			values.Set(name, value)
		}
	}
	return "/oauth/authorize?" + values.Encode()
}

// pending starts an authorization and returns its consent request ID.
func (f oauthFixture) pending(t *testing.T, clientID, redirect string, overrides map[string]string) string {
	t.Helper()
	w := f.do(t, httptest.NewRequest("GET", authorizeQuery(clientID, redirect, overrides), nil))
	location, err := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusSeeOther || err != nil || location.Path != "/oauth/consent" {
		t.Fatalf("authorize: %d %q", w.Code, w.Header().Get("Location"))
	}
	return location.Query().Get("request")
}

// consent answers a pending request as the fixture's account and returns
// the redirect's query.
func (f oauthFixture) consent(t *testing.T, id string, allow bool, scopes ...string) url.Values {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"allow": allow, "scopes": scopes})
	w := identityRequest(f.handler, "POST", "/api/v2/oauth/requests/"+id, string(raw), f.cookie, f.csrf, "")
	var result struct {
		RedirectTo string `json:"redirectTo"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("consent: %d %s", w.Code, w.Body.String())
	}
	target, err := url.Parse(result.RedirectTo)
	if err != nil {
		t.Fatal(err)
	}
	return target.Query()
}

func (f oauthFixture) code(t *testing.T, clientID, redirect string, scopes ...string) string {
	t.Helper()
	query := f.consent(t, f.pending(t, clientID, redirect, nil), true, scopes...)
	if query.Get("state") != "xyz" || query.Get("iss") != oauthIssuer || query.Get("code") == "" {
		t.Fatalf("authorization response: %v", query)
	}
	return query.Get("code")
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (f oauthFixture) token(t *testing.T, values url.Values) (int, tokenResponse) {
	t.Helper()
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := f.do(t, r)
	var result tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("token response: %d %s", w.Code, w.Body.String())
	}
	return w.Code, result
}

func exchange(code, clientID, redirect string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_verifier": {verifier}, "resource": {oauthResource}}
}

func refresh(token, clientID string) url.Values {
	return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}, "resource": {oauthResource}}
}

func (f oauthFixture) mcp(t *testing.T, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return f.do(t, r)
}

const mcpChallengeValue = `resource_metadata="` + oauthIssuer + `/.well-known/oauth-protected-resource/mcp", scope="reviews:read comments:write"`

func TestOAuthMetadataExistsOnlyWithPublicURL(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer + "/"})
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Origin", "https://claude.ai")
		w := f.do(t, r)
		var metadata struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			ScopesSupported      []string `json:"scopes_supported"`
		}
		if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "*" || json.Unmarshal(w.Body.Bytes(), &metadata) != nil ||
			metadata.Resource != oauthResource || len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != oauthIssuer ||
			strings.Join(metadata.ScopesSupported, " ") != "reviews:read comments:write" {
			t.Fatalf("%s: %d %v %s", path, w.Code, w.Header(), w.Body.String())
		}
	}
	w := f.do(t, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	var server map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &server) != nil {
		t.Fatalf("server metadata: %d %s", w.Code, w.Body.String())
	}
	for name, want := range map[string]string{
		"issuer": `"` + oauthIssuer + `"`, "authorization_endpoint": `"` + oauthIssuer + `/oauth/authorize"`, "token_endpoint": `"` + oauthIssuer + `/oauth/token"`,
		"registration_endpoint": `"` + oauthIssuer + `/oauth/register"`, "revocation_endpoint": `"` + oauthIssuer + `/oauth/revoke"`,
		"code_challenge_methods_supported": `["S256"]`, "token_endpoint_auth_methods_supported": `["none"]`,
		"client_id_metadata_document_supported": "true", "authorization_response_iss_parameter_supported": "true",
		"grant_types_supported": `["authorization_code","refresh_token"]`, "response_types_supported": `["code"]`,
	} {
		if got := string(server[name]); got != want {
			t.Fatalf("%s: %s want %s", name, got, want)
		}
	}
	r := httptest.NewRequest("OPTIONS", "/oauth/token", nil)
	r.Header.Set("Origin", "https://claude.ai")
	if w := f.do(t, r); w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	plain := newOAuthFixture(t, Settings{})
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/oauth/token", "/oauth/register", authorizeQuery(cimdClient, cimdRedirect, nil)} {
		method := "GET"
		if strings.HasPrefix(path, "/oauth/t") || strings.HasPrefix(path, "/oauth/r") {
			method = "POST"
		}
		if w := plain.do(t, httptest.NewRequest(method, path, nil)); w.Code != 404 {
			t.Fatalf("%s without public URL: %d", path, w.Code)
		}
	}
	if w := plain.mcp(t, "/mcp", ""); w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Bearer realm="diffx"` {
		t.Fatalf("plain MCP challenge: %d %v", w.Code, w.Header())
	}
}

func TestOAuthAuthorizationCodeFlow(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	if w := f.mcp(t, "/mcp", ""); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer "+mcpChallengeValue {
		t.Fatalf("MCP challenge: %d %v", w.Code, w.Header())
	}
	if w := f.mcp(t, "/mcp/contexts/x", "diffx_at_unknown"); w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Bearer error="invalid_token", `+mcpChallengeValue {
		t.Fatalf("invalid token challenge: %d %v", w.Code, w.Header())
	}

	// The registered loopback redirect matches any port.
	redirect := "http://127.0.0.1:53682/callback"
	id := f.pending(t, cimdClient, redirect, nil)
	w := identityRequest(f.handler, "GET", "/api/v2/oauth/requests/"+id, "", f.cookie, "", "")
	if w.Code != 200 || w.Body.String() != `{"client":{"name":"Claude","verified":true,"host":"claude.example"},"redirectHost":"127.0.0.1:53682","scopes":["reviews:read","comments:write"]}`+"\n" {
		t.Fatalf("pending request: %d %s", w.Code, w.Body.String())
	}
	// Consent unticks comments:write: the token can only read.
	query := f.consent(t, id, true, caller.ScopeReviewsRead)
	if query.Get("state") != "xyz" || query.Get("iss") != oauthIssuer {
		t.Fatalf("authorization response: %v", query)
	}
	code := query.Get("code")
	status, tokens := f.token(t, exchange(code, cimdClient, redirect))
	if status != 200 || tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 || tokens.Scope != "reviews:read" || !strings.HasPrefix(tokens.AccessToken, "diffx_at_") || !strings.HasPrefix(tokens.RefreshToken, "diffx_rt_") {
		t.Fatalf("exchange: %d %+v", status, tokens)
	}
	for _, path := range []string{"/mcp", "/mcp/contexts/missing"} {
		if w := f.mcp(t, path, tokens.AccessToken); w.Code == 401 || w.Code == 403 {
			t.Fatalf("MCP %s with OAuth token: %d %s", path, w.Code, w.Body.String())
		}
	}
	// OAuth tokens are bound to the MCP resource; REST refuses them.
	for _, path := range []string{"/api/v2/contexts", "/api/v2/health", "/api/v2/account/agents", "/api/v2/bootstrap"} {
		if w := identityRequest(f.handler, "GET", path, "", nil, "", "Bearer "+tokens.AccessToken); w.Code != 401 {
			t.Fatalf("REST %s with OAuth token: %d", path, w.Code)
		}
	}
	w = identityRequest(f.handler, "GET", "/api/v2/account/agents", "", f.cookie, "", "")
	var agents struct {
		Agents []agentJSON `json:"agents"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &agents) != nil || len(agents.Agents) != 1 || agents.Agents[0].Name != "Claude" ||
		!agents.Agents[0].Verified || agents.Agents[0].Host == nil || *agents.Agents[0].Host != "claude.example" ||
		strings.Join(agents.Agents[0].Scopes, " ") != "reviews:read" || agents.Agents[0].LastUsedAt == nil {
		t.Fatalf("agents: %d %s", w.Code, w.Body.String())
	}

	// A replayed code fails and revokes the tokens it issued.
	if status, result := f.token(t, exchange(code, cimdClient, redirect)); status != 400 || result.Error != "invalid_grant" {
		t.Fatalf("code reuse: %d %+v", status, result)
	}
	if w := f.mcp(t, "/mcp", tokens.AccessToken); w.Code != 401 {
		t.Fatalf("token after code reuse: %d", w.Code)
	}
	if status, _ := f.token(t, refresh(tokens.RefreshToken, cimdClient)); status != 400 {
		t.Fatalf("refresh after code reuse: %d", status)
	}

	// Disconnecting ends the grant's tokens.
	_, tokens = f.token(t, exchange(f.code(t, cimdClient, cimdRedirect, caller.ScopeReviewsRead, caller.ScopeCommentsWrite), cimdClient, cimdRedirect))
	if tokens.Scope != "reviews:read comments:write" {
		t.Fatalf("full consent scopes: %+v", tokens)
	}
	if w := identityRequest(f.handler, "DELETE", "/api/v2/account/agents/"+agents.Agents[0].ID, "", f.cookie, "", ""); w.Code != 403 {
		t.Fatalf("disconnect without CSRF: %d", w.Code)
	}
	if w := identityRequest(f.handler, "DELETE", "/api/v2/account/agents/"+agents.Agents[0].ID, "", f.cookie, f.csrf, ""); w.Code != 204 {
		t.Fatalf("disconnect: %d %s", w.Code, w.Body.String())
	}
	if w := f.mcp(t, "/mcp", tokens.AccessToken); w.Code != 401 {
		t.Fatalf("token after disconnect: %d", w.Code)
	}
	if status, _ := f.token(t, refresh(tokens.RefreshToken, cimdClient)); status != 400 {
		t.Fatalf("refresh after disconnect: %d", status)
	}
}

func TestOAuthCodeBindings(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	for name, mutate := range map[string]func(url.Values){
		"PKCE":     func(v url.Values) { v.Set("code_verifier", strings.Repeat("b", 43)) },
		"redirect": func(v url.Values) { v.Set("redirect_uri", "https://claude.example/callback") },
		"client":   func(v url.Values) { v.Set("client_id", "https://other.example/client.json") },
	} {
		code := f.code(t, cimdClient, cimdRedirect)
		values := exchange(code, cimdClient, cimdRedirect)
		mutate(values)
		if status, result := f.token(t, values); status != 400 || result.Error != "invalid_grant" {
			t.Fatalf("%s mismatch: %d %+v", name, status, result)
		}
	}
	code := f.code(t, cimdClient, cimdRedirect)
	values := exchange(code, cimdClient, cimdRedirect)
	values.Set("resource", oauthIssuer+"/api")
	if status, result := f.token(t, values); status != 400 || result.Error != "invalid_target" {
		t.Fatalf("resource mismatch: %d %+v", status, result)
	}
	values.Del("resource")
	if status, result := f.token(t, values); status != 400 || result.Error != "invalid_target" {
		t.Fatalf("missing resource: %d %+v", status, result)
	}
	// The failed attempts left the code valid; once expired it is not.
	if _, err := f.conn.Exec(t.Context(), `UPDATE diffx.oauth_codes SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if status, result := f.token(t, exchange(code, cimdClient, cimdRedirect)); status != 400 || result.Error != "invalid_grant" {
		t.Fatalf("expired code: %d %+v", status, result)
	}
	if status, result := f.token(t, url.Values{"grant_type": {"password"}, "client_id": {cimdClient}, "resource": {oauthResource}}); status != 400 || result.Error != "unsupported_grant_type" {
		t.Fatalf("grant type: %d %+v", status, result)
	}
}

func TestOAuthAuthorizeErrors(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	// An unknown client, a mismatched document or an unregistered redirect
	// shows the error page and never redirects.
	for _, query := range []string{
		authorizeQuery("https://unknown.example/client.json", cimdRedirect, nil),
		authorizeQuery("https://evil.example/client.json", "https://evil.example/cb", nil),
		authorizeQuery("unregistered", cimdRedirect, nil),
		authorizeQuery(cimdClient, "https://attacker.example/callback", nil),
		authorizeQuery(cimdClient, "http://localhost:4000/callback", nil),
		authorizeQuery(cimdClient, "http://127.0.0.1:4000/other", nil),
	} {
		w := f.do(t, httptest.NewRequest("GET", query, nil))
		if w.Code != 400 || w.Header().Get("Location") != "" || w.Body.String() != "app shell" {
			t.Fatalf("%s: %d %q", query, w.Code, w.Header().Get("Location"))
		}
	}
	// Other errors return to the verified redirect with state and issuer.
	for override, want := range map[string]string{
		"code_challenge_method": "invalid_request", "code_challenge": "invalid_request", "resource": "invalid_target", "response_type": "unsupported_response_type",
	} {
		value := ""
		if override == "resource" {
			value = oauthIssuer + "/other"
		}
		if override == "response_type" {
			value = "token"
		}
		w := f.do(t, httptest.NewRequest("GET", authorizeQuery(cimdClient, cimdRedirect, map[string]string{override: value}), nil))
		location, err := url.Parse(w.Header().Get("Location"))
		if w.Code != 302 || err != nil || location.Host != "127.0.0.1" || location.Query().Get("error") != want || location.Query().Get("state") != "xyz" || location.Query().Get("iss") != oauthIssuer {
			t.Fatalf("%s: %d %q", override, w.Code, w.Header().Get("Location"))
		}
	}
	// Denying reports access_denied; the request is then answered.
	id := f.pending(t, cimdClient, cimdRedirect, nil)
	if query := f.consent(t, id, false); query.Get("error") != "access_denied" || query.Get("state") != "xyz" || query.Get("code") != "" {
		t.Fatalf("deny: %v", query)
	}
	if w := identityRequest(f.handler, "GET", "/api/v2/oauth/requests/"+id, "", f.cookie, "", ""); w.Code != 404 {
		t.Fatalf("answered request: %d", w.Code)
	}
	// Consent needs the session's CSRF token, never an API token.
	id = f.pending(t, cimdClient, cimdRedirect, map[string]string{"scope": "reviews:read"})
	if w := identityRequest(f.handler, "POST", "/api/v2/oauth/requests/"+id, `{"allow":true,"scopes":["reviews:read"]}`, f.cookie, "", ""); w.Code != 403 {
		t.Fatalf("consent without CSRF: %d", w.Code)
	}
	created, err := testsupport.Identity(t, f.store).CreateToken(t.Context(), f.account.ID, "api", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if w := identityRequest(f.handler, "POST", "/api/v2/oauth/requests/"+id, `{"allow":true,"scopes":[]}`, nil, "", "Bearer "+created.Secret); w.Code != 403 {
		t.Fatalf("consent with API token: %d", w.Code)
	}
	if w := identityRequest(f.handler, "GET", "/api/v2/oauth/requests/"+id, "", nil, "", ""); w.Code != 401 {
		t.Fatalf("signed-out request lookup: %d", w.Code)
	}
	// A request that asked only to read cannot gain comment writes at consent.
	w := identityRequest(f.handler, "GET", "/api/v2/oauth/requests/"+id, "", f.cookie, "", "")
	if !strings.Contains(w.Body.String(), `"scopes":["reviews:read"]`) {
		t.Fatalf("read-only request: %s", w.Body.String())
	}
	query := f.consent(t, id, true, caller.ScopeReviewsRead, caller.ScopeCommentsWrite)
	if _, tokens := f.token(t, exchange(query.Get("code"), cimdClient, cimdRedirect)); tokens.Scope != "reviews:read" {
		t.Fatalf("escalated scopes: %+v", tokens)
	}
	// Expired requests are gone.
	id = f.pending(t, cimdClient, cimdRedirect, nil)
	if _, err := f.conn.Exec(t.Context(), `UPDATE diffx.oauth_requests SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if w := identityRequest(f.handler, "POST", "/api/v2/oauth/requests/"+id, `{"allow":true,"scopes":[]}`, f.cookie, f.csrf, ""); w.Code != 404 {
		t.Fatalf("expired request: %d", w.Code)
	}
}

func TestOAuthRefreshRotationAndReuse(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	_, first := f.token(t, exchange(f.code(t, cimdClient, cimdRedirect, caller.ScopeReviewsRead, caller.ScopeCommentsWrite), cimdClient, cimdRedirect))
	status, second := f.token(t, refresh(first.RefreshToken, cimdClient))
	if status != 200 || second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken || second.Scope != "reviews:read comments:write" {
		t.Fatalf("refresh: %d %+v", status, second)
	}
	if status, result := f.token(t, refresh(second.RefreshToken, "https://other.example/client.json")); status != 400 || result.Error != "invalid_grant" {
		t.Fatalf("refresh by another client: %d %+v", status, result)
	}
	if w := f.mcp(t, "/mcp", second.AccessToken); w.Code == 401 {
		t.Fatalf("rotated access token: %d", w.Code)
	}
	// Presenting the rotated token again revokes the whole family.
	if status, result := f.token(t, refresh(first.RefreshToken, cimdClient)); status != 400 || result.Error != "invalid_grant" {
		t.Fatalf("reuse: %d %+v", status, result)
	}
	if status, _ := f.token(t, refresh(second.RefreshToken, cimdClient)); status != 400 {
		t.Fatalf("family survived reuse: %d", status)
	}
	if w := f.mcp(t, "/mcp", second.AccessToken); w.Code != 401 {
		t.Fatalf("access token survived reuse: %d", w.Code)
	}

	// Revocation (RFC 7009) ends a family and answers 200 for unknown tokens.
	_, tokens := f.token(t, exchange(f.code(t, cimdClient, cimdRedirect), cimdClient, cimdRedirect))
	for _, token := range []string{"unknown", tokens.RefreshToken} {
		r := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(url.Values{"token": {token}, "client_id": {cimdClient}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if w := f.do(t, r); w.Code != 200 {
			t.Fatalf("revoke %s: %d %s", token, w.Code, w.Body.String())
		}
	}
	if w := f.mcp(t, "/mcp", tokens.AccessToken); w.Code != 401 {
		t.Fatalf("access token after revoke: %d", w.Code)
	}
}

func TestOAuthDynamicRegistration(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer, ClientIPHeader: "X-Real-IP"})
	register := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Real-IP", "203.0.113.9")
		return f.do(t, r)
	}
	for _, body := range []string{
		`{"redirect_uris":[]}`,
		`{"redirect_uris":["http://cursor.example/callback"]}`,
		`{"redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback"]}`,
		`{"redirect_uris":["https://app.example/cb#fragment"]}`,
		`{"redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		`{"redirect_uris":["https://app.example/cb"],"grant_types":["client_credentials"]}`,
		`not json`,
	} {
		if w := register(body); w.Code != 400 || !strings.Contains(w.Body.String(), `"error":"invalid_`) {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	w := register(`{"client_name":"Cursor\u0000 Agent","redirect_uris":["http://localhost/callback"],"logo_uri":"https://app.example/logo.png","scope":"reviews:read"}`)
	var client struct {
		ClientID                string `json:"client_id"`
		ClientName              string `json:"client_name"`
		TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &client) != nil || client.ClientName != "Cursor  Agent" || client.TokenEndpointAuthMethod != "none" {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	// A registered client's name is its own claim, shown unverified.
	r := httptest.NewRequest("GET", authorizeQuery(client.ClientID, "http://localhost:9999/callback", nil), nil)
	r.Header.Set("X-Real-IP", "203.0.113.10")
	w = f.do(t, r)
	location, _ := url.Parse(w.Header().Get("Location"))
	w = identityRequest(f.handler, "GET", "/api/v2/oauth/requests/"+location.Query().Get("request"), "", f.cookie, "", "")
	if !strings.Contains(w.Body.String(), `"client":{"name":"Cursor  Agent","verified":false,"host":null}`) {
		t.Fatalf("unverified client: %d %s", w.Code, w.Body.String())
	}
	// Registration is limited like sign-in.
	for range 30 {
		w = register(`{"redirect_uris":["https://app.example/cb"]}`)
		if w.Code == 429 {
			return
		}
	}
	t.Fatalf("registration never limited: %d", w.Code)
}

func TestOAuthAccountDisableRevokesGrants(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	_, tokens := f.token(t, exchange(f.code(t, cimdClient, cimdRedirect), cimdClient, cimdRedirect))
	pendingCode := f.code(t, cimdClient, cimdRedirect)
	if err := f.store.SetAccountDisabled(t.Context(), f.account.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := f.mcp(t, "/mcp", tokens.AccessToken); w.Code != 401 {
		t.Fatalf("access after disable: %d", w.Code)
	}
	if status, _ := f.token(t, refresh(tokens.RefreshToken, cimdClient)); status != 400 {
		t.Fatalf("refresh after disable: %d", status)
	}
	if status, _ := f.token(t, exchange(pendingCode, cimdClient, cimdRedirect)); status != 400 {
		t.Fatalf("code after disable: %d", status)
	}
	var grants int
	if err := f.conn.QueryRow(t.Context(), `SELECT count(*) FROM diffx.oauth_grants`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("grants after disable: %d %v", grants, err)
	}
}

func TestOAuthInsufficientScope(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	_, tokens := f.token(t, exchange(f.code(t, cimdClient, cimdRedirect), cimdClient, cimdRedirect))
	if _, err := f.conn.Exec(t.Context(), `UPDATE diffx.oauth_tokens SET scopes = '{comments:write}'`); err != nil {
		t.Fatal(err)
	}
	w := f.mcp(t, "/mcp", tokens.AccessToken)
	if w.Code != 403 || w.Header().Get("WWW-Authenticate") != `Bearer error="insufficient_scope", resource_metadata="`+oauthIssuer+`/.well-known/oauth-protected-resource/mcp", scope="reviews:read"` {
		t.Fatalf("insufficient scope: %d %v", w.Code, w.Header())
	}
}

// Every authenticated request reaches the application with its caller.
func TestCallerInRequestContext(t *testing.T) {
	f := newOAuthFixture(t, Settings{PublicURL: oauthIssuer})
	_, tokens := f.token(t, exchange(f.code(t, cimdClient, cimdRedirect, caller.ScopeReviewsRead), cimdClient, cimdRedirect))
	created, err := testsupport.Identity(t, f.store).CreateToken(t.Context(), f.account.ID, "laptop agent", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	remote := f.remote
	service := contextservice.NewWithContext(t.Context(), f.store, reviewdata.User{ID: f.account.ID, Name: f.account.Email}, contextservice.ExternalEvents())
	var seen []caller.Caller
	remote.applications.mu.Lock()
	remote.applications.applications[f.account.ID] = application{service: service, handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := caller.From(r.Context())
		if !ok {
			t.Error("no caller in context")
		}
		seen = append(seen, value)
	})}
	remote.applications.mu.Unlock()
	identityRequest(remote, "GET", "/api/v2/contexts", "", f.cookie, "", "")
	identityRequest(remote, "GET", "/api/v2/contexts", "", nil, "", "Bearer "+created.Secret)
	identityRequest(remote, "POST", "/mcp", "{}", nil, "", "Bearer "+tokens.AccessToken)
	want := []caller.Caller{
		{Kind: caller.KindSession},
		{Kind: caller.KindAPIToken, Name: "laptop agent"},
		{Kind: caller.KindOAuth, Name: "Claude", Verified: true, ClientID: cimdClient, Scopes: []string{caller.ScopeReviewsRead}},
	}
	if len(seen) != len(want) {
		t.Fatalf("callers: %+v", seen)
	}
	for i := range want {
		if seen[i].Kind != want[i].Kind || seen[i].Name != want[i].Name || seen[i].Verified != want[i].Verified || seen[i].ClientID != want[i].ClientID ||
			strings.Join(seen[i].Scopes, " ") != strings.Join(want[i].Scopes, " ") {
			t.Fatalf("caller %d: %+v want %+v", i, seen[i], want[i])
		}
	}
	if seen[2].Allows(caller.ScopeCommentsWrite) || !seen[1].Allows(caller.ScopeCommentsWrite) {
		t.Fatal("OAuth scopes do not limit the caller")
	}
}

func TestSignInReturnsToConsentOnly(t *testing.T) {
	id := strings.Repeat("A", 43)
	for next, want := range map[string]bool{
		"/settings/github": true, "/oauth/consent?request=" + id: true,
		"/oauth/consent?request=short": false, "/oauth/consent?request=" + id + "&x=1": false, "https://evil.example/oauth/consent?request=" + id: false,
		"//evil.example/oauth/consent?request=" + id: false, "/oauth/authorize?request=" + id: false, "/settings": false,
	} {
		if returnAllowed(next) != want {
			t.Fatalf("%s: want %v", next, want)
		}
	}
}
