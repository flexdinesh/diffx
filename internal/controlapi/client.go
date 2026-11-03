package controlapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// baseURL addresses the server over whatever transport the client dials;
// the local socket ignores the host, and localhost satisfies its host policy.
const baseURL = "http://localhost"

type Client struct {
	http *http.Client
}

// NewClient talks to the control API through client, whose transport selects
// the endpoint (the local socket). Redirects are never followed.
func NewClient(client *http.Client) *Client {
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copied}
}

func (client *Client) Status(ctx context.Context) (Status, error) {
	var result Status
	err := client.do(ctx, http.MethodGet, "/control/v1/status", &result)
	return result, err
}

func (client *Client) Shutdown(ctx context.Context) error {
	var result struct{}
	return client.do(ctx, http.MethodPost, "/control/v1/shutdown", &result)
}

func (client *Client) do(ctx context.Context, method, path string, result interface{}) error {
	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	const limit = 64 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read service response: %w", err)
	}
	if len(data) > limit {
		return fmt.Errorf("service response exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		problem := &Problem{Status: response.StatusCode, Detail: "Service request failed: " + http.StatusText(response.StatusCode)}
		_ = json.Unmarshal(data, problem)
		problem.Status = response.StatusCode
		return problem
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("invalid service response: %w", err)
	}
	return nil
}
