// Package driver speaks MyGo's public newline JSON automation protocol without
// linking the native framework into the server module.
package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const maxResponse = 32 << 20

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type Rect struct{ X, Y, Width, Height float64 }
type Window struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Kind          string `json:"kind"`
	URL           string `json:"url"`
	Bounds        Rect   `json:"bounds"`
	ContentBounds Rect   `json:"contentBounds"`
	Focused       bool   `json:"focused"`
	Visible       bool   `json:"visible"`
}
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
	id     uint64
}

func Dial(ctx context.Context, path string) (*Client, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, reader: bufio.NewReader(conn)}, nil
}
func (c *Client) Close() error { return c.conn.Close() }

// Call serializes requests and bounds both reading and writing by ctx. Losing a
// response closes the connection; a subsequent call cannot consume a stale ID.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()
	c.id++
	req := struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{c.id, method, params}
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		_ = c.Close()
		return fmt.Errorf("%s send: %w", method, err)
	}
	var line []byte
	for {
		part, prefix, err := c.reader.ReadLine()
		if err != nil {
			_ = c.Close()
			return fmt.Errorf("%s receive: %w", method, err)
		}
		if len(line)+len(part) > maxResponse {
			_ = c.Close()
			return fmt.Errorf("%s response exceeds %d bytes", method, maxResponse)
		}
		line = append(line, part...)
		if !prefix {
			break
		}
	}
	var response struct {
		ID     uint64          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		_ = c.Close()
		return fmt.Errorf("%s decode: %w", method, err)
	}
	if response.ID != c.id {
		_ = c.Close()
		return fmt.Errorf("%s response ID %d, want %d", method, response.ID, c.id)
	}
	if response.Error != nil {
		return response.Error
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("%s missing result: %w", method, io.ErrUnexpectedEOF)
	}
	if result != nil {
		return json.Unmarshal(response.Result, result)
	}
	return nil
}

func (c *Client) Windows(ctx context.Context) ([]Window, error) {
	var result struct {
		Windows []Window `json:"windows"`
	}
	err := c.Call(ctx, "windows", nil, &result)
	return result.Windows, err
}

// Eval awaits page promises and decodes their actual WebKit result.
func (c *Client) Eval(ctx context.Context, window int, script string, result any) error {
	var response struct {
		Value json.RawMessage `json:"value"`
	}
	if err := c.Call(ctx, "eval", struct {
		Window int    `json:"window"`
		Script string `json:"script"`
	}{window, script}, &response); err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(response.Value, result)
	}
	return nil
}

// Until waits on observable conditions. Each observation receives a deadline;
// no ready state is inferred from elapsed time. The final error preserves the
// last condition's diagnostic, including protocol/child failures from callers.
func Until(ctx context.Context, description string, condition func(context.Context) error) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for %s: %w; last observation: %v", description, err, last)
		}
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		last = condition(attempt)
		cancel()
		if last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %w; last observation: %v", description, ctx.Err(), last)
		case <-ticker.C:
		}
	}
}
