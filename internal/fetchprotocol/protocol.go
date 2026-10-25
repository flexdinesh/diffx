// Package fetchprotocol is the internal contract between diffx-server and
// diffx-fetcher. The server owns GitHub credentials and identity; the fetcher
// leases one pull request at a time, mirrors it and returns an owner-free
// snapshot. It never claims an account or pull request metadata.
package fetchprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

const (
	LeasePath  = "/internal/v1/fetch-jobs/lease"
	ResultPath = "/internal/v1/fetch-jobs/result"
)

// Job leases one pull request head. Token is a short-lived installation token
// limited to Repository with contents:read; it is valid only for fetching.
type Job struct {
	ID             string     `json:"id"`
	LeaseToken     string     `json:"leaseToken"`
	LeaseExpiresAt int64      `json:"leaseExpiresAt"`
	Repository     Repository `json:"repository"`
	Number         int        `json:"number"`
	HeadSHA        string     `json:"headSha"`
	BaseRef        string     `json:"baseRef"`
	BaseSHA        string     `json:"baseSha"`
	Token          string     `json:"token"`
	TokenExpiresAt int64      `json:"tokenExpiresAt"`
}

type Repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"fullName"`
	CloneURL string `json:"cloneUrl"`
}

// Result completes a lease with a snapshot, or fails it with a detail for retry.
type Result struct {
	ID         string             `json:"id"`
	LeaseToken string             `json:"leaseToken"`
	Request    *ingestion.Request `json:"request,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// MaxResultBytes bounds a result: one snapshot plus envelope.
const MaxResultBytes = ingestion.MaxRequestBytes + 1<<20

// Client is the fetcher's view of the server's internal listener.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, secret string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil {
		return nil, errors.New("fetcher server URL must be an http(s) URL without credentials")
	}
	if secret == "" {
		return nil, errors.New("fetcher secret required")
	}
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), secret: secret, http: &http.Client{Timeout: 2 * time.Minute}}, nil
}

// Lease returns nil when no pull request needs fetching.
func (c *Client) Lease(ctx context.Context) (*Job, error) {
	response, err := c.post(ctx, LeasePath, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, statusError(response)
	}
	var job Job
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&job); err != nil {
		return nil, fmt.Errorf("decode fetch lease: %w", err)
	}
	return &job, nil
}

func (c *Client) Complete(ctx context.Context, job Job, request ingestion.Request) error {
	return c.result(ctx, Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request})
}

func (c *Client) Fail(ctx context.Context, job Job, detail string) error {
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	return c.result(ctx, Result{ID: job.ID, LeaseToken: job.LeaseToken, Error: detail})
}

func (c *Client) result(ctx context.Context, result Result) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	response, err := c.post(ctx, ResultPath, raw)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return statusError(response)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	request.Header.Set("Content-Type", "application/json")
	return c.http.Do(request)
}

// StatusError reports a non-success response; 409 means the lease was lost.
type StatusError struct {
	Status int
	Detail string
}

func (err *StatusError) Error() string {
	return fmt.Sprintf("fetch server responded %d: %s", err.Status, err.Detail)
}

func statusError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var problem struct {
		Detail string `json:"detail"`
	}
	detail := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &problem) == nil && problem.Detail != "" {
		detail = problem.Detail
	}
	return &StatusError{Status: response.StatusCode, Detail: detail}
}
