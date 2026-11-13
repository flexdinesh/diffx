// Package oauthservice owns the OAuth 2.1 rules that let MCP clients act for
// an account: clients, authorization requests, consent, codes, grants and
// tokens. Clients are public: PKCE (S256) proves possession instead of a
// secret. Transports own the wire format; storage is behind Store.
package oauthservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/caller"
)

const (
	// RequestLifetime bounds how long a validated request awaits consent.
	RequestLifetime = 10 * time.Minute
	CodeLifetime    = 60 * time.Second
	AccessLifetime  = time.Hour
	// RefreshLifetime slides: each refresh issues a token valid this long.
	RefreshLifetime = 30 * 24 * time.Hour

	// Token prefixes tell OAuth tokens from API tokens at a glance.
	accessPrefix  = "diffx_at_"
	refreshPrefix = "diffx_rt_"
)

// Scopes lists every scope this server grants, in display order. Reading is
// always granted; writing comments is the user's choice at consent.
var Scopes = []string{caller.ScopeReviewsRead, caller.ScopeCommentsWrite}

var (
	ErrNotFound = errors.New("oauth record not found")
	// ErrInvalidGrant reports a code or refresh token that is unknown,
	// expired, used, revoked or bound to another client, redirect or resource.
	ErrInvalidGrant = errors.New("invalid authorization grant")
	// ErrInvalidToken reports an access token this resource does not accept.
	ErrInvalidToken = errors.New("invalid access token")
)

// Error is an OAuth protocol error (RFC 6749 section 4.1.2.1 and 5.2).
// Redirect is set when the authorization error may be reported to the client
// by redirecting there; otherwise the user sees it and is never redirected.
type Error struct {
	Code        string
	Description string
	Redirect    string
}

func (err *Error) Error() string { return err.Code + ": " + err.Description }

func protocolError(code, description string) *Error {
	return &Error{Code: code, Description: description}
}

// Client is an application that asks for access. Verified marks a client
// identified by the URL of a metadata document it serves; its name is the
// document's claim, shown beside the verified host.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	Verified     bool
}

// Host is the domain that verified a client, or "" for an unverified one.
func (client Client) Host() string {
	if !client.Verified {
		return ""
	}
	return hostOf(client.ID)
}

// Request is a validated authorization request awaiting consent.
type Request struct {
	ID            string
	Client        Client
	RedirectURI   string
	State         string
	CodeChallenge string
	// Scopes are the requested scopes; consent may grant fewer.
	Scopes    []string
	Resource  string
	ExpiresAt time.Time
}

// Code is a consented authorization, bound to everything its redemption must match.
type Code struct {
	AccountID     string
	Client        Client
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
	Resource      string
	ExpiresAt     time.Time
}

// Grant is an account's consent to one client.
type Grant struct {
	ID         string
	Client     Client
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Access is an authenticated access token.
type Access struct {
	AccountID    string
	AccountEmail string
	Client       Client
	Scopes       []string
	Resource     string
	ExpiresAt    time.Time
}

// Caller is the access token's caller for transports that attribute writes.
func (access Access) Caller() caller.Caller {
	return caller.Caller{Kind: caller.KindOAuth, Name: access.Client.Name, Verified: access.Client.Verified, ClientID: access.Client.ID, Scopes: slices.Clone(access.Scopes)}
}

// IssuedTokens are the hashes and expiries of one access and refresh token
// pair in a family.
type IssuedTokens struct {
	FamilyID         string
	AccessHash       string
	AccessExpiresAt  time.Time
	RefreshHash      string
	RefreshExpiresAt time.Time
}

// Redemption is a token request for a code: every binding it must match.
type Redemption struct {
	CodeHash      string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Resource      string
}

// Refresh is a token request for a refresh token.
type Refresh struct {
	TokenHash string
	ClientID  string
	Resource  string
}

// Store keeps OAuth state atomically. Disabling an account revokes its grants
// and codes; deleting it removes them.
type Store interface {
	CreateOAuthClient(ctx context.Context, client Client, created time.Time) error
	// OAuthClient resolves a registered client; unknown ones are ErrNotFound.
	OAuthClient(ctx context.Context, clientID string) (Client, error)
	PutOAuthRequest(ctx context.Context, request Request, now time.Time) error
	// OAuthRequest resolves an unexpired request; others are ErrNotFound.
	OAuthRequest(ctx context.Context, id string, now time.Time) (Request, error)
	// ConsumeOAuthRequest removes an unexpired request and, with code, stores
	// the code for an enabled account in the same transaction. A missing,
	// expired or used request, or a disabled account, is ErrNotFound.
	ConsumeOAuthRequest(ctx context.Context, id string, code *Code, codeHash string, now time.Time) (Request, error)
	// RedeemOAuthCode consumes an unexpired code whose bindings all match,
	// records the account's grant to its client and stores the first tokens
	// of a new family, returning the granted scopes. Any other code is
	// ErrInvalidGrant; a code redeemed before also revokes the family it
	// started.
	RedeemOAuthCode(ctx context.Context, redemption Redemption, tokens IssuedTokens, now time.Time) ([]string, error)
	// RefreshOAuthToken rotates an unexpired refresh token of the client and
	// resource into tokens of the same family, returning its scopes. Any
	// other token is ErrInvalidGrant; a rotated one also revokes its family.
	RefreshOAuthToken(ctx context.Context, refresh Refresh, tokens IssuedTokens, now time.Time) ([]string, error)
	// ResolveOAuthToken resolves an unexpired access token of an enabled
	// account and records its grant's use; others are ErrInvalidToken.
	ResolveOAuthToken(ctx context.Context, tokenHash string, now time.Time) (Access, error)
	// RevokeOAuthToken revokes the family of the client's access or refresh
	// token; unknown tokens are ignored.
	RevokeOAuthToken(ctx context.Context, tokenHash, clientID string) error
	OAuthGrants(ctx context.Context, accountID string) ([]Grant, error)
	// DeleteOAuthGrant revokes the grant and every token issued under it;
	// another account's or an unknown grant is ErrNotFound.
	DeleteOAuthGrant(ctx context.Context, accountID, grantID string) error
}

// MetadataFetcher reads a client metadata document from its https URL,
// refusing non-public addresses and oversized documents.
type MetadataFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Service is enabled with an issuer, the server's public origin; without one
// it only lists and revokes grants.
type Service struct {
	store    Store
	metadata MetadataFetcher
	issuer   string
	now      func() time.Time
	clients  clientCache
}

func New(store Store, metadata MetadataFetcher, issuer string) *Service {
	return &Service{store: store, metadata: metadata, issuer: strings.TrimSuffix(issuer, "/"), now: time.Now, clients: clientCache{entries: map[string]cachedClient{}}}
}

// Enabled reports whether this server authorizes OAuth clients.
func (service *Service) Enabled() bool { return service.issuer != "" }

// Issuer identifies this authorization server (RFC 8414).
func (service *Service) Issuer() string { return service.issuer }

// Resource is the canonical MCP resource URI (RFC 8707) tokens are bound to.
func (service *Service) Resource() string { return service.issuer + "/mcp" }

func opaqueSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func secretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

// AuthorizeParams are an authorization request's query parameters.
// ScopeGiven distinguishes an empty scope from an absent one.
type AuthorizeParams struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	ScopeGiven          bool
	Resources           []string
}

// Authorize validates an authorization request and keeps it for consent.
// The client and redirect URI are checked first: when either is invalid the
// *Error has no Redirect, so the user is never sent to an unverified address.
func (service *Service) Authorize(ctx context.Context, params AuthorizeParams) (Request, error) {
	if !service.Enabled() {
		return Request{}, ErrNotFound
	}
	if params.ClientID == "" {
		return Request{}, protocolError("invalid_request", "client_id is required")
	}
	client, err := service.client(ctx, params.ClientID)
	if err != nil {
		return Request{}, err
	}
	if params.RedirectURI == "" || !redirectAllowed(client.RedirectURIs, params.RedirectURI) {
		return Request{}, protocolError("invalid_request", "redirect_uri is not registered for this client")
	}
	fail := func(code, description string) (Request, error) {
		err := protocolError(code, description)
		err.Redirect = service.redirect(params.RedirectURI, url.Values{"error": {code}, "error_description": {description}}, params.State)
		return Request{}, err
	}
	switch {
	case params.ResponseType != "code":
		return fail("unsupported_response_type", "response_type must be code")
	case params.CodeChallengeMethod != "S256" || !validChallenge(params.CodeChallenge):
		return fail("invalid_request", "PKCE with code_challenge_method S256 is required")
	case len(params.Resources) != 1 || params.Resources[0] != service.Resource():
		return fail("invalid_target", "resource must be "+service.Resource())
	case len(params.State) > 1024:
		return fail("invalid_request", "state is too long")
	}
	id, err := opaqueSecret()
	if err != nil {
		return Request{}, err
	}
	now := service.now()
	request := Request{ID: id, Client: client, RedirectURI: params.RedirectURI, State: params.State, CodeChallenge: params.CodeChallenge,
		Scopes: requestedScopes(params.Scope, params.ScopeGiven), Resource: service.Resource(), ExpiresAt: now.Add(RequestLifetime)}
	if err := service.store.PutOAuthRequest(ctx, request, now); err != nil {
		return Request{}, err
	}
	return request, nil
}

// requestedScopes keeps the known scopes requested, in display order, and
// always reading. No scope parameter requests every scope; unknown ones are
// ignored rather than refused, so clients asking for more still connect.
func requestedScopes(scope string, given bool) []string {
	requested := strings.Fields(scope)
	result := []string{caller.ScopeReviewsRead}
	for _, known := range Scopes[1:] {
		if !given || slices.Contains(requested, known) {
			result = append(result, known)
		}
	}
	return result
}

// validChallenge accepts an S256 challenge: 43 base64url characters.
func validChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil
}

// validVerifier accepts RFC 7636's 43-128 unreserved characters.
func validVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, r := range verifier {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	return true
}

func challengeOf(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// redirect adds the response parameters, the client's state and this
// issuer (RFC 9207) to a registered redirect URI.
func (service *Service) redirect(redirectURI string, values url.Values, state string) string {
	target, err := url.Parse(redirectURI)
	if err != nil {
		return ""
	}
	query := target.Query()
	for name, value := range values {
		query[name] = value
	}
	if state != "" {
		query.Set("state", state)
	}
	query.Set("iss", service.issuer)
	target.RawQuery = query.Encode()
	return target.String()
}

// PendingRequest resolves a request awaiting consent.
func (service *Service) PendingRequest(ctx context.Context, id string) (Request, error) {
	if !service.Enabled() || len(id) != 43 {
		return Request{}, ErrNotFound
	}
	return service.store.OAuthRequest(ctx, id, service.now())
}

// Consent records the signed-in account's decision and returns where to send
// the browser. Allowing grants the chosen scopes the request asked for, and
// always reading; denying reports access_denied to the client.
func (service *Service) Consent(ctx context.Context, id, accountID string, allow bool, scopes []string) (string, error) {
	if !service.Enabled() || len(id) != 43 {
		return "", ErrNotFound
	}
	now := service.now()
	if !allow {
		request, err := service.store.ConsumeOAuthRequest(ctx, id, nil, "", now)
		if err != nil {
			return "", err
		}
		return service.redirect(request.RedirectURI, url.Values{"error": {"access_denied"}, "error_description": {"The user denied access"}}, request.State), nil
	}
	pending, err := service.store.OAuthRequest(ctx, id, now)
	if err != nil {
		return "", err
	}
	granted := []string{caller.ScopeReviewsRead}
	for _, scope := range pending.Scopes {
		if scope != caller.ScopeReviewsRead && slices.Contains(scopes, scope) {
			granted = append(granted, scope)
		}
	}
	secret, err := opaqueSecret()
	if err != nil {
		return "", err
	}
	code := Code{AccountID: accountID, Client: pending.Client, RedirectURI: pending.RedirectURI, CodeChallenge: pending.CodeChallenge,
		Scopes: granted, Resource: pending.Resource, ExpiresAt: now.Add(CodeLifetime)}
	request, err := service.store.ConsumeOAuthRequest(ctx, id, &code, secretHash(secret), now)
	if err != nil {
		return "", err
	}
	return service.redirect(request.RedirectURI, url.Values{"code": {secret}}, request.State), nil
}

// TokenParams are a token request's form parameters.
type TokenParams struct {
	GrantType    string
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Resources    []string
}

// Tokens is a token response.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Scopes       []string
}

// Token exchanges a code or refresh token. Failures are *Error.
func (service *Service) Token(ctx context.Context, params TokenParams) (Tokens, error) {
	if !service.Enabled() {
		return Tokens{}, protocolError("invalid_request", "OAuth is not enabled")
	}
	if params.ClientID == "" {
		return Tokens{}, protocolError("invalid_client", "client_id is required")
	}
	if len(params.Resources) != 1 || params.Resources[0] != service.Resource() {
		return Tokens{}, protocolError("invalid_target", "resource must be "+service.Resource())
	}
	now := service.now()
	issued, tokens, err := service.newTokens(now)
	if err != nil {
		return Tokens{}, err
	}
	var scopes []string
	switch params.GrantType {
	case "authorization_code":
		if params.Code == "" || params.RedirectURI == "" || !validVerifier(params.CodeVerifier) {
			return Tokens{}, protocolError("invalid_request", "code, redirect_uri and a valid code_verifier are required")
		}
		scopes, err = service.store.RedeemOAuthCode(ctx, Redemption{CodeHash: secretHash(params.Code), ClientID: params.ClientID, RedirectURI: params.RedirectURI,
			CodeChallenge: challengeOf(params.CodeVerifier), Resource: service.Resource()}, issued, now)
	case "refresh_token":
		if !strings.HasPrefix(params.RefreshToken, refreshPrefix) {
			return Tokens{}, protocolError("invalid_grant", "refresh token is invalid")
		}
		// A scope parameter is ignored: a refresh keeps its family's scopes.
		scopes, err = service.store.RefreshOAuthToken(ctx, Refresh{TokenHash: secretHash(params.RefreshToken), ClientID: params.ClientID, Resource: service.Resource()}, issued, now)
	default:
		return Tokens{}, protocolError("unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
	if errors.Is(err, ErrInvalidGrant) {
		return Tokens{}, protocolError("invalid_grant", "the code or refresh token is invalid, expired or revoked")
	}
	if err != nil {
		return Tokens{}, err
	}
	tokens.Scopes = scopes
	return tokens, nil
}

func (service *Service) newTokens(now time.Time) (IssuedTokens, Tokens, error) {
	var secrets [3]string
	for i := range secrets {
		secret, err := opaqueSecret()
		if err != nil {
			return IssuedTokens{}, Tokens{}, err
		}
		secrets[i] = secret
	}
	access, refresh := accessPrefix+secrets[0], refreshPrefix+secrets[1]
	return IssuedTokens{FamilyID: secrets[2], AccessHash: secretHash(access), AccessExpiresAt: now.Add(AccessLifetime), RefreshHash: secretHash(refresh), RefreshExpiresAt: now.Add(RefreshLifetime)},
		Tokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: AccessLifetime}, nil
}

// Revoke revokes the family of a client's access or refresh token (RFC
// 7009). Unknown tokens succeed, so revocation reveals nothing.
func (service *Service) Revoke(ctx context.Context, token, clientID string) error {
	if !service.Enabled() {
		return protocolError("invalid_request", "OAuth is not enabled")
	}
	if clientID == "" {
		return protocolError("invalid_client", "client_id is required")
	}
	if token == "" {
		return protocolError("invalid_request", "token is required")
	}
	if !strings.HasPrefix(token, accessPrefix) && !strings.HasPrefix(token, refreshPrefix) {
		return nil
	}
	return service.store.RevokeOAuthToken(ctx, secretHash(token), clientID)
}

// IsAccessToken reports whether a bearer credential is an OAuth access token
// rather than an API token.
func IsAccessToken(secret string) bool { return strings.HasPrefix(secret, accessPrefix) }

// Authenticate resolves an access token bound to this server's MCP resource.
func (service *Service) Authenticate(ctx context.Context, secret string) (Access, error) {
	if !service.Enabled() || !IsAccessToken(secret) || len(secret) != len(accessPrefix)+43 {
		return Access{}, ErrInvalidToken
	}
	access, err := service.store.ResolveOAuthToken(ctx, secretHash(secret), service.now())
	if err != nil {
		return Access{}, err
	}
	if access.Resource != service.Resource() {
		return Access{}, ErrInvalidToken
	}
	return access, nil
}

func (service *Service) Grants(ctx context.Context, accountID string) ([]Grant, error) {
	return service.store.OAuthGrants(ctx, accountID)
}

// RevokeGrant disconnects a client: its grant and every token end at once.
func (service *Service) RevokeGrant(ctx context.Context, accountID, grantID string) error {
	return service.store.DeleteOAuthGrant(ctx, accountID, grantID)
}

// clientCache keeps fetched metadata documents briefly, so one authorization
// fetches its client's document once. It is per process and bounded.
type clientCache struct {
	mu      sync.Mutex
	entries map[string]cachedClient
}

type cachedClient struct {
	client  Client
	expires time.Time
}

const (
	clientCacheLifetime = 5 * time.Minute
	clientCacheSize     = 256
)

func (cache *clientCache) get(id string, now time.Time) (Client, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[id]
	if !ok || !now.Before(entry.expires) {
		return Client{}, false
	}
	return entry.client, true
}

func (cache *clientCache) put(client Client, now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) >= clientCacheSize {
		for id, entry := range cache.entries {
			if !now.Before(entry.expires) {
				delete(cache.entries, id)
			}
		}
	}
	if len(cache.entries) >= clientCacheSize {
		clear(cache.entries)
	}
	cache.entries[client.ID] = cachedClient{client: client, expires: now.Add(clientCacheLifetime)}
}
