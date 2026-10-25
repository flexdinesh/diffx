// Package github adapts GitHub App REST, OAuth and webhook wire formats.
// It holds no storage and no diffx policy.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultAPIURL    = "https://api.github.com"
	defaultWebURL    = "https://github.com"
	apiVersion       = "2022-11-28"
	userAgent        = "diffx"
	maxResponseBytes = 10 << 20
	maxMessageBytes  = 512
	pageSize         = "100"
)

var (
	// ErrUnauthorized matches 401 responses and rejected OAuth codes or refresh tokens.
	ErrUnauthorized      = errors.New("github: unauthorized")
	ErrInvalidRepository = errors.New("github: repository must be owner/name")
)

// Error is a non-2xx GitHub API response. Rate-limited 403s report Status 429.
type Error struct {
	Status  int
	Message string
}

func (err *Error) Error() string {
	if err.Message == "" {
		return fmt.Sprintf("github: HTTP %d", err.Status)
	}
	return fmt.Sprintf("github: HTTP %d: %s", err.Status, err.Message)
}

func (err *Error) Is(target error) bool {
	return target == ErrUnauthorized && err.Status == http.StatusUnauthorized
}

type App struct {
	ID            int64
	ClientID      string
	ClientSecret  string
	PrivateKey    *rsa.PrivateKey
	WebhookSecret []byte
}

type Option func(*Client)

func WithAPIURL(url string) Option {
	return func(client *Client) { client.apiURL = strings.TrimRight(url, "/") }
}

// WithWebURL sets the origin of the OAuth authorize and token endpoints.
func WithWebURL(url string) Option {
	return func(client *Client) { client.webURL = strings.TrimRight(url, "/") }
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) { client.http = httpClient }
}

type Client struct {
	app    App
	apiURL string
	webURL string
	http   *http.Client
	mu     sync.Mutex
	tokens map[int64]Token
}

func New(app App, options ...Option) (*Client, error) {
	switch {
	case app.ID <= 0:
		return nil, errors.New("github app ID is required")
	case app.ClientID == "":
		return nil, errors.New("github app client ID is required")
	case app.ClientSecret == "":
		return nil, errors.New("github app client secret is required")
	case app.PrivateKey == nil:
		return nil, errors.New("github app private key is required")
	case len(app.WebhookSecret) == 0:
		return nil, errors.New("github app webhook secret is required")
	}
	app.WebhookSecret = bytes.Clone(app.WebhookSecret)
	client := &Client{app: app, apiURL: defaultAPIURL, webURL: defaultWebURL,
		http: &http.Client{Timeout: 30 * time.Second}, tokens: map[int64]Token{}}
	for _, option := range options {
		option(client)
	}
	if client.http == nil {
		return nil, errors.New("github HTTP client is required")
	}
	if err := validateBaseURL(client.apiURL); err != nil {
		return nil, fmt.Errorf("github API URL: %w", err)
	}
	if err := validateBaseURL(client.webURL); err != nil {
		return nil, fmt.Errorf("github web URL: %w", err)
	}
	return client, nil
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an HTTP(S) URL without credentials, query, or fragment")
	}
	return nil
}

func setHeaders(header http.Header, accept string) {
	header.Set("Accept", accept)
	header.Set("X-GitHub-Api-Version", apiVersion)
	header.Set("User-Agent", userAgent)
}

func (c *Client) newRequest(ctx context.Context, method, target, bearer string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	setHeaders(request.Header, "application/vnd.github+json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	return request, nil
}

func (c *Client) get(ctx context.Context, target, bearer string, out any) (http.Header, error) {
	request, err := c.newRequest(ctx, http.MethodGet, target, bearer, nil)
	if err != nil {
		return nil, err
	}
	return c.send(request, out)
}

// send decodes a bounded 2xx JSON response into out and returns its headers.
func (c *Client) send(request *http.Request, out any) (http.Header, error) {
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("github response exceeds 10 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, responseError(response, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("invalid github response: %w", err)
	}
	return response.Header, nil
}

func responseError(response *http.Response, data []byte) *Error {
	status := response.StatusCode
	if (status == http.StatusForbidden || status == http.StatusTooManyRequests) &&
		(response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
		status = http.StatusTooManyRequests
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &body) != nil {
		return &Error{Status: status}
	}
	return &Error{Status: status, Message: bounded(body.Message)}
}

func bounded(text string) string {
	if len(text) <= maxMessageBytes {
		return text
	}
	text = text[:maxMessageBytes]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

// collect follows Link rel="next" from path until limit items are gathered.
func collect[P, T any](ctx context.Context, c *Client, bearer, path string, limit int, items func(P) []T) ([]T, error) {
	all := []T{}
	for target := c.apiURL + path; target != "" && len(all) < limit; {
		var page P
		header, err := c.get(ctx, target, bearer, &page)
		if err != nil {
			return nil, err
		}
		batch := items(page)
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if target, err = c.nextPage(header); err != nil {
			return nil, err
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// nextPage refuses links off the API origin so credentials stay with GitHub.
func (c *Client) nextPage(header http.Header) (string, error) {
	next := nextLink(header)
	if next == "" {
		return "", nil
	}
	parsed, err := url.Parse(next)
	if err != nil {
		return "", errors.New("invalid github pagination link")
	}
	base, err := url.Parse(c.apiURL)
	if err != nil || parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		return "", errors.New("github pagination link leaves the API origin")
	}
	return next, nil
}

func nextLink(header http.Header) string {
	for _, value := range header.Values("Link") {
		for _, link := range strings.Split(value, ",") {
			target, params, ok := strings.Cut(strings.TrimSpace(link), ";")
			target = strings.TrimSpace(target)
			if !ok || len(target) < 2 || target[0] != '<' || target[len(target)-1] != '>' {
				continue
			}
			for _, param := range strings.Split(params, ";") {
				if strings.TrimSpace(param) == `rel="next"` {
					return target[1 : len(target)-1]
				}
			}
		}
	}
	return ""
}
