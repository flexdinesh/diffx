package oauthservice

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"unicode"
)

const (
	maxRedirectURIs   = 10
	maxRedirectLength = 2048
	maxClientName     = 64
	// registeredPrefix marks dynamically registered client IDs, which are
	// never URLs, so they cannot be mistaken for metadata document clients.
	registeredPrefix = "diffx_client_"
)

// client resolves a client ID: an https URL names a metadata document the
// client serves (verified by its host), anything else a registered client.
// Failures are an *Error without Redirect, or a storage error.
func (service *Service) client(ctx context.Context, id string) (Client, error) {
	if !strings.HasPrefix(id, "https://") {
		client, err := service.store.OAuthClient(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return Client{}, protocolError("invalid_client", "unknown client_id")
		}
		return client, err
	}
	now := service.now()
	if client, ok := service.clients.get(id, now); ok {
		return client, nil
	}
	if !validMetadataURL(id) {
		return Client{}, protocolError("invalid_client", "client_id must be an https URL with a path")
	}
	raw, err := service.metadata.Fetch(ctx, id)
	if err != nil {
		return Client{}, protocolError("invalid_client", "client metadata document unavailable")
	}
	client, err := parseMetadata(id, raw)
	if err != nil {
		return Client{}, err
	}
	service.clients.put(client, now)
	return client, nil
}

// validMetadataURL accepts an https URL with a host and path, and no
// credentials, query, fragment or dot segments.
func validMetadataURL(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || strings.HasSuffix(raw, "#") {
		return false
	}
	if target.Path == "" || target.Path == "/" {
		return false
	}
	for _, segment := range strings.Split(target.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// metadata is the part of a client ID metadata document diffx reads.
type metadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientSecret            *string  `json:"client_secret"`
}

// parseMetadata accepts a public client's document only when its client_id
// is exactly the URL it was fetched from.
func parseMetadata(id string, raw []byte) (Client, error) {
	var document metadata
	if err := json.Unmarshal(raw, &document); err != nil {
		return Client{}, protocolError("invalid_client", "client metadata document is not valid JSON")
	}
	if document.ClientID != id {
		return Client{}, protocolError("invalid_client", "client metadata client_id does not match its URL")
	}
	if document.ClientSecret != nil || document.TokenEndpointAuthMethod != "" && document.TokenEndpointAuthMethod != "none" {
		return Client{}, protocolError("invalid_client", "only public clients are supported")
	}
	if err := validRedirects(document.RedirectURIs); err != nil {
		return Client{}, protocolError("invalid_client", err.Description)
	}
	name := clientName(document.ClientName)
	if name == "" {
		name = hostOf(id)
	}
	return Client{ID: id, Name: name, RedirectURIs: document.RedirectURIs, Verified: true}, nil
}

// Registration is a dynamic client registration request (RFC 7591).
type Registration struct {
	ClientName              string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
}

// Register stores a dynamically registered public client. Its name is the
// client's unverified claim. Failures are *Error.
func (service *Service) Register(ctx context.Context, registration Registration) (Client, error) {
	if !service.Enabled() {
		return Client{}, protocolError("invalid_request", "OAuth is not enabled")
	}
	if err := validRedirects(registration.RedirectURIs); err != nil {
		return Client{}, err
	}
	if registration.TokenEndpointAuthMethod != "" && registration.TokenEndpointAuthMethod != "none" {
		return Client{}, protocolError("invalid_client_metadata", "token_endpoint_auth_method must be none")
	}
	for _, grant := range registration.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return Client{}, protocolError("invalid_client_metadata", "grant_types may be authorization_code and refresh_token")
		}
	}
	for _, response := range registration.ResponseTypes {
		if response != "code" {
			return Client{}, protocolError("invalid_client_metadata", "response_types may only be code")
		}
	}
	name := clientName(registration.ClientName)
	if name == "" {
		name = "Unnamed app"
	}
	secret, err := opaqueSecret()
	if err != nil {
		return Client{}, err
	}
	client := Client{ID: registeredPrefix + secret, Name: name, RedirectURIs: slices.Clone(registration.RedirectURIs)}
	if err := service.store.CreateOAuthClient(ctx, client, service.now()); err != nil {
		return Client{}, err
	}
	return client, nil
}

// clientName keeps a client's claimed name displayable: printable, single
// line, trimmed and at most maxClientName characters.
func clientName(raw string) string {
	var name strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(raw) {
		if !unicode.IsPrint(r) {
			r = ' '
		}
		if count == maxClientName {
			break
		}
		name.WriteRune(r)
		count++
	}
	return strings.TrimSpace(name.String())
}

// validRedirects requires one to ten redirect URIs, each https or a
// loopback http address (RFC 8252), without fragments or credentials.
func validRedirects(uris []string) *Error {
	if len(uris) == 0 || len(uris) > maxRedirectURIs {
		return protocolError("invalid_redirect_uri", "redirect_uris must list 1 to 10 URIs")
	}
	for _, raw := range uris {
		target, err := url.Parse(raw)
		if err != nil || len(raw) > maxRedirectLength || target.Host == "" || target.User != nil || target.Fragment != "" || strings.Contains(raw, "#") {
			return protocolError("invalid_redirect_uri", "redirect URIs must be absolute https or loopback http URLs")
		}
		if target.Scheme != "https" && !(target.Scheme == "http" && loopback(target.Hostname())) {
			return protocolError("invalid_redirect_uri", "redirect URIs must be https or loopback http")
		}
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectAllowed matches a requested redirect URI exactly, except that a
// registered loopback URI matches any port (RFC 8252 section 7.3).
func redirectAllowed(registered []string, requested string) bool {
	if validRedirects([]string{requested}) != nil {
		return false
	}
	if slices.Contains(registered, requested) {
		return true
	}
	want, err := url.Parse(requested)
	if err != nil || want.Scheme != "http" || !loopback(want.Hostname()) {
		return false
	}
	for _, raw := range registered {
		have, err := url.Parse(raw)
		if err == nil && have.Scheme == "http" && loopback(have.Hostname()) && have.Hostname() == want.Hostname() && have.EscapedPath() == want.EscapedPath() && have.RawQuery == want.RawQuery {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	target, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return target.Host
}
