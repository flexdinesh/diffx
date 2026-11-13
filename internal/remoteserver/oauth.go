package remoteserver

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/caller"
	"github.com/flexdinesh/diffx/internal/oauthservice"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

// OAuth routes. Metadata, token, revocation and registration are called by
// clients from anywhere and never read cookies, so they skip the same-origin
// check and allow CORS. Authorization is a browser navigation; consent is an
// app API that needs the browser session and CSRF token.
const (
	resourceMetadataPath = "/.well-known/oauth-protected-resource"
	serverMetadataPath   = "/.well-known/oauth-authorization-server"
	authorizePath        = "/oauth/authorize"
	consentPath          = "/oauth/consent"
	tokenPath            = "/oauth/token"
	revokePath           = "/oauth/revoke"
	registerPath         = "/oauth/register"
	consentAPI           = "/api/v2/oauth/requests/"
)

// oauthRoute serves the OAuth protocol routes and reports whether it did.
// Without a public URL they do not exist.
func (remote *transport) oauthRoute(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case resourceMetadataPath, resourceMetadataPath + "/mcp", serverMetadataPath, tokenPath, revokePath, registerPath, authorizePath:
	default:
		return false
	}
	if !remote.oauth.Enabled() {
		http.NotFound(w, r)
		return true
	}
	if r.URL.Path == authorizePath {
		if !serverapp.SameOrigin(r) && !serverapp.PageNavigation(r) {
			authhttp.Problem(w, 403, "Cross-origin access denied")
			return true
		}
		remote.authorize(w, r)
		return true
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
	metadata := strings.HasPrefix(r.URL.Path, "/.well-known/")
	method := http.MethodPost
	if metadata {
		method = http.MethodGet
	}
	w.Header().Set("Access-Control-Allow-Methods", method+", OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	if r.Method != method && !(metadata && r.Method == http.MethodHead) {
		authhttp.Problem(w, 405, "Method not allowed")
		return true
	}
	switch r.URL.Path {
	case serverMetadataPath:
		remote.serverMetadata(w)
	case tokenPath:
		remote.token(w, r)
	case revokePath:
		remote.revoke(w, r)
	case registerPath:
		remote.register(w, r)
	default:
		authhttp.WriteJSON(w, 200, struct {
			Resource               string   `json:"resource"`
			AuthorizationServers   []string `json:"authorization_servers"`
			ScopesSupported        []string `json:"scopes_supported"`
			BearerMethodsSupported []string `json:"bearer_methods_supported"`
			ResourceName           string   `json:"resource_name"`
		}{remote.oauth.Resource(), []string{remote.oauth.Issuer()}, oauthservice.Scopes, []string{"header"}, "diffx"})
	}
	return true
}

// serverMetadata is the authorization server metadata (RFC 8414).
func (remote *transport) serverMetadata(w http.ResponseWriter) {
	issuer := remote.oauth.Issuer()
	authhttp.WriteJSON(w, 200, struct {
		Issuer                                 string   `json:"issuer"`
		AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
		TokenEndpoint                          string   `json:"token_endpoint"`
		RegistrationEndpoint                   string   `json:"registration_endpoint"`
		RevocationEndpoint                     string   `json:"revocation_endpoint"`
		ScopesSupported                        []string `json:"scopes_supported"`
		ResponseTypesSupported                 []string `json:"response_types_supported"`
		ResponseModesSupported                 []string `json:"response_modes_supported"`
		GrantTypesSupported                    []string `json:"grant_types_supported"`
		TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
		RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
		CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
		ClientIDMetadataDocumentSupported      bool     `json:"client_id_metadata_document_supported"`
		AuthorizationResponseISSSupported      bool     `json:"authorization_response_iss_parameter_supported"`
	}{
		issuer, issuer + authorizePath, issuer + tokenPath, issuer + registerPath, issuer + revokePath, oauthservice.Scopes,
		[]string{"code"}, []string{"query"}, []string{"authorization_code", "refresh_token"}, []string{"none"}, []string{"none"}, []string{"S256"}, true, true,
	})
}

// authorize validates an authorization request, then sends the browser to
// the consent page. An invalid client or redirect URI shows the consent
// page's error state and never redirects; other errors return to the client.
func (remote *transport) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	// A client ID URL makes the server fetch it, so authorization is bounded
	// like sign-in.
	release, ok := remote.admission.Admit(w, r)
	if !ok {
		return
	}
	defer release()
	query := r.URL.Query()
	_, scopeGiven := query["scope"]
	request, err := remote.oauth.Authorize(r.Context(), oauthservice.AuthorizeParams{
		ResponseType: query.Get("response_type"), ClientID: query.Get("client_id"), RedirectURI: query.Get("redirect_uri"), State: query.Get("state"),
		CodeChallenge: query.Get("code_challenge"), CodeChallengeMethod: query.Get("code_challenge_method"), Scope: query.Get("scope"), ScopeGiven: scopeGiven,
		Resources: query["resource"],
	})
	var failure *oauthservice.Error
	switch {
	case errors.As(err, &failure) && failure.Redirect != "":
		http.Redirect(w, r, failure.Redirect, http.StatusFound)
	case errors.As(err, &failure):
		remote.appShell(w, http.StatusBadRequest)
	case err != nil:
		log.Printf("OAuth authorization: %v", err)
		remote.appShell(w, http.StatusInternalServerError)
	default:
		http.Redirect(w, r, consentPath+"?"+url.Values{"request": {request.ID}}.Encode(), http.StatusSeeOther)
	}
}

// appShell serves the web app, whose consent page renders the error state.
func (remote *transport) appShell(w http.ResponseWriter, status int) {
	raw, err := fs.ReadFile(remote.assets, "index.html")
	if err != nil {
		authhttp.Problem(w, status, "Invalid authorization request")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// consentReturn reports a consent page address sign-in may return to.
func consentReturn(next string) bool {
	target, err := url.Parse(next)
	if err != nil || target.Scheme != "" || target.Host != "" || target.Path != consentPath || target.Fragment != "" {
		return false
	}
	query := target.Query()
	return len(query) == 1 && len(query["request"]) == 1 && validRequestID(query.Get("request"))
}

func validRequestID(id string) bool {
	if len(id) != 43 {
		return false
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// oauthError writes a token or registration endpoint error (RFC 6749 section 5.2).
func oauthError(w http.ResponseWriter, err error) {
	var failure *oauthservice.Error
	if !errors.As(err, &failure) {
		log.Printf("OAuth: %v", err)
		authhttp.WriteJSON(w, 500, struct {
			Error string `json:"error"`
		}{"server_error"})
		return
	}
	status := 400
	if failure.Code == "invalid_client" {
		status = 401
	}
	authhttp.WriteJSON(w, status, struct {
		Error       string `json:"error"`
		Description string `json:"error_description,omitempty"`
	}{failure.Code, failure.Description})
}

// form parses a form-encoded token or revocation request body.
func form(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		oauthError(w, &oauthservice.Error{Code: "invalid_request", Description: "expected application/x-www-form-urlencoded"})
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := r.ParseForm(); err != nil {
		oauthError(w, &oauthservice.Error{Code: "invalid_request", Description: "malformed form body"})
		return nil, false
	}
	return r.PostForm, true
}

func (remote *transport) token(w http.ResponseWriter, r *http.Request) {
	values, ok := form(w, r)
	if !ok {
		return
	}
	w.Header().Set("Pragma", "no-cache")
	tokens, err := remote.oauth.Token(r.Context(), oauthservice.TokenParams{
		GrantType: values.Get("grant_type"), ClientID: values.Get("client_id"), Code: values.Get("code"), RedirectURI: values.Get("redirect_uri"),
		CodeVerifier: values.Get("code_verifier"), RefreshToken: values.Get("refresh_token"), Resources: values["resource"],
	})
	if err != nil {
		oauthError(w, err)
		return
	}
	authhttp.WriteJSON(w, 200, struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}{tokens.AccessToken, "Bearer", int64(tokens.ExpiresIn.Seconds()), tokens.RefreshToken, strings.Join(tokens.Scopes, " ")})
}

// revoke answers 200 for unknown tokens too (RFC 7009 section 2.2).
func (remote *transport) revoke(w http.ResponseWriter, r *http.Request) {
	values, ok := form(w, r)
	if !ok {
		return
	}
	if err := remote.oauth.Revoke(r.Context(), values.Get("token"), values.Get("client_id")); err != nil {
		oauthError(w, err)
		return
	}
	w.WriteHeader(200)
}

// register is dynamic client registration (RFC 7591): unauthenticated and
// bounded like sign-in. Registered clients are unverified.
func (remote *transport) register(w http.ResponseWriter, r *http.Request) {
	release, ok := remote.admission.Admit(w, r)
	if !ok {
		return
	}
	defer release()
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		oauthError(w, &oauthservice.Error{Code: "invalid_client_metadata", Description: "expected application/json"})
		return
	}
	// Clients send many optional metadata fields; unknown ones are ignored.
	var input struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		oauthError(w, &oauthservice.Error{Code: "invalid_client_metadata", Description: "malformed registration request"})
		return
	}
	client, err := remote.oauth.Register(r.Context(), oauthservice.Registration{ClientName: input.ClientName, RedirectURIs: input.RedirectURIs,
		TokenEndpointAuthMethod: input.TokenEndpointAuthMethod, GrantTypes: input.GrantTypes, ResponseTypes: input.ResponseTypes})
	if err != nil {
		oauthError(w, err)
		return
	}
	authhttp.WriteJSON(w, 201, struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}{client.ID, client.Name, client.RedirectURIs, []string{"authorization_code", "refresh_token"}, []string{"code"}, "none"})
}

// mcpRoute reports an MCP endpoint, the only routes OAuth tokens reach.
func mcpRoute(r *http.Request) bool {
	return r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/")
}

// mcpChallenge refuses an MCP request, pointing clients at the protected
// resource metadata (RFC 9728) so they can start authorization. Without
// OAuth it is the plain bearer challenge.
func (remote *transport) mcpChallenge(w http.ResponseWriter, status int, failure string) {
	if !remote.oauth.Enabled() {
		authhttp.Unauthorized(w)
		return
	}
	scope := strings.Join(oauthservice.Scopes, " ")
	if failure == "insufficient_scope" {
		scope = caller.ScopeReviewsRead
	}
	challenge := "Bearer "
	if failure != "" {
		challenge += `error="` + failure + `", `
	}
	challenge += `resource_metadata="` + remote.oauth.Issuer() + resourceMetadataPath + `/mcp", scope="` + scope + `"`
	w.Header().Set("WWW-Authenticate", challenge)
	detail := "Authentication required"
	if status == http.StatusForbidden {
		detail = "Insufficient scope"
	}
	authhttp.Problem(w, status, detail)
}

type consentClientJSON struct {
	Name     string  `json:"name"`
	Verified bool    `json:"verified"`
	Host     *string `json:"host"`
}

func projectClient(client oauthservice.Client) consentClientJSON {
	result := consentClientJSON{Name: client.Name, Verified: client.Verified}
	if host := client.Host(); host != "" {
		result.Host = &host
	}
	return result
}

// consent serves a pending request to the consent page and records the
// signed-in account's decision. The caller required a session and CSRF.
func (remote *transport) consent(w http.ResponseWriter, r *http.Request, p principal) {
	id := strings.TrimPrefix(r.URL.Path, consentAPI)
	if !remote.oauth.Enabled() || !validRequestID(id) {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		request, err := remote.oauth.PendingRequest(r.Context(), id)
		if err != nil {
			consentError(w, err)
			return
		}
		redirect, _ := url.Parse(request.RedirectURI)
		authhttp.WriteJSON(w, 200, struct {
			Client       consentClientJSON `json:"client"`
			RedirectHost string            `json:"redirectHost"`
			Scopes       []string          `json:"scopes"`
		}{projectClient(request.Client), redirect.Host, request.Scopes})
	case http.MethodPost:
		var input struct {
			Allow  bool     `json:"allow"`
			Scopes []string `json:"scopes"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		target, err := remote.oauth.Consent(r.Context(), id, p.account.ID, input.Allow, input.Scopes)
		if err != nil {
			consentError(w, err)
			return
		}
		authhttp.WriteJSON(w, 200, struct {
			RedirectTo string `json:"redirectTo"`
		}{target})
	default:
		authhttp.Problem(w, 405, "Method not allowed")
	}
}

func consentError(w http.ResponseWriter, err error) {
	if errors.Is(err, oauthservice.ErrNotFound) {
		authhttp.Problem(w, 404, "This request is invalid, expired or already answered")
		return
	}
	log.Printf("OAuth consent: %v", err)
	authhttp.Problem(w, 500, "Server could not process request")
}

type agentJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Verified   bool     `json:"verified"`
	Host       *string  `json:"host"`
	Scopes     []string `json:"scopes"`
	CreatedAt  int64    `json:"createdAt"`
	LastUsedAt *int64   `json:"lastUsedAt"`
}

// agents lists and disconnects the account's OAuth grants. They exist only
// while OAuth was enabled, but stay manageable without it.
func (remote *transport) agents(w http.ResponseWriter, r *http.Request, p principal) {
	if r.URL.Path == "/api/v2/account/agents" {
		if r.Method != http.MethodGet {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		grants, err := remote.oauth.Grants(r.Context(), p.account.ID)
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		values := make([]agentJSON, 0, len(grants))
		for _, grant := range grants {
			client := projectClient(grant.Client)
			value := agentJSON{ID: grant.ID, Name: client.Name, Verified: client.Verified, Host: client.Host, Scopes: grant.Scopes, CreatedAt: grant.CreatedAt.UnixMilli()}
			if grant.LastUsedAt != nil {
				last := grant.LastUsedAt.UnixMilli()
				value.LastUsedAt = &last
			}
			values = append(values, value)
		}
		authhttp.WriteJSON(w, 200, struct {
			Agents []agentJSON `json:"agents"`
		}{values})
		return
	}
	if r.Method != http.MethodDelete {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v2/account/agents/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	err := remote.oauth.RevokeGrant(r.Context(), p.account.ID, id)
	if errors.Is(err, oauthservice.ErrNotFound) {
		authhttp.Problem(w, 404, "Not found")
		return
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	w.WriteHeader(204)
}
