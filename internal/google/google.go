// Package google is the Google OpenID Connect wire adapter: authorization URLs
// and the authorization-code exchange. It holds no storage or diffx policy.
package google

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL     = "https://oauth2.googleapis.com/token"
	// maxTokenResponse bounds the token endpoint body.
	maxTokenResponse = 64 << 10
)

// ErrRejected reports an exchange or ID token Google or this client refused.
var ErrRejected = errors.New("google sign-in rejected")

// Identity is the verified subject of an ID token. Subject is stable for the
// Google account; Email can change.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

type Client struct {
	clientID, clientSecret, redirectURI string
	authorizeURL, tokenURL              string
	http                                *http.Client
	now                                 func() time.Time
}

type Option func(*Client)

// WithEndpoints replaces Google's endpoints in tests.
func WithEndpoints(authorize, token string) Option {
	return func(c *Client) { c.authorizeURL, c.tokenURL = authorize, token }
}

func New(clientID, clientSecret, redirectURI string, options ...Option) *Client {
	client := &Client{clientID: clientID, clientSecret: clientSecret, redirectURI: redirectURI, authorizeURL: authorizeURL, tokenURL: tokenURL,
		http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}
	for _, option := range options {
		option(client)
	}
	return client
}

// Challenge derives the S256 PKCE challenge for a verifier.
func Challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c *Client) AuthorizeURL(state, nonce, verifier string) string {
	query := url.Values{
		"client_id": {c.clientID}, "redirect_uri": {c.redirectURI}, "response_type": {"code"}, "scope": {"openid email"},
		"state": {state}, "nonce": {nonce}, "code_challenge": {Challenge(verifier)}, "code_challenge_method": {"S256"}, "prompt": {"select_account"},
	}
	return c.authorizeURL + "?" + query.Encode()
}

// Exchange redeems the code and validates the returned ID token's claims. The
// token comes directly from Google's token endpoint over TLS, so its signature
// is not checked (OpenID Connect Core 3.1.3.7).
func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	if code == "" {
		return Identity{}, fmt.Errorf("%w: missing code", ErrRejected)
	}
	form := url.Values{"code": {code}, "client_id": {c.clientID}, "client_secret": {c.clientSecret}, "redirect_uri": {c.redirectURI},
		"grant_type": {"authorization_code"}, "code_verifier": {verifier}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Identity{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenResponse))
	if err != nil {
		return Identity{}, err
	}
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
		return Identity{}, fmt.Errorf("%w: token endpoint returned %d", ErrRejected, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("google token endpoint returned %d", response.StatusCode)
	}
	var token struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.IDToken == "" {
		return Identity{}, fmt.Errorf("%w: token response missing id_token", ErrRejected)
	}
	return c.identity(token.IDToken, nonce)
}

func (c *Client) identity(idToken, nonce string) (Identity, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Identity{}, fmt.Errorf("%w: malformed id_token", ErrRejected)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, fmt.Errorf("%w: malformed id_token", ErrRejected)
	}
	var claims struct {
		Issuer        string `json:"iss"`
		Audience      string `json:"aud"`
		Expiry        int64  `json:"exp"`
		Nonce         string `json:"nonce"`
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Identity{}, fmt.Errorf("%w: malformed id_token claims", ErrRejected)
	}
	switch {
	case claims.Issuer != "https://accounts.google.com" && claims.Issuer != "accounts.google.com":
		return Identity{}, fmt.Errorf("%w: unexpected issuer", ErrRejected)
	case claims.Audience != c.clientID:
		return Identity{}, fmt.Errorf("%w: unexpected audience", ErrRejected)
	case claims.Expiry <= c.now().Unix():
		return Identity{}, fmt.Errorf("%w: expired id_token", ErrRejected)
	case nonce == "" || claims.Nonce != nonce:
		return Identity{}, fmt.Errorf("%w: nonce mismatch", ErrRejected)
	case claims.Subject == "" || claims.Email == "":
		return Identity{}, fmt.Errorf("%w: id_token missing subject or email", ErrRejected)
	}
	return Identity{Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}
