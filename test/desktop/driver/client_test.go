package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func pipeClient(t *testing.T, serve func(net.Conn)) *Client {
	t.Helper()
	conn, peer := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); defer peer.Close(); serve(peer) }()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close(); <-done })
	return &Client{conn: conn, reader: bufio.NewReader(conn)}
}

func TestPageEvaluationPreservesLargeFragmentedResults(t *testing.T) {
	text := strings.Repeat("webview", 5000)
	c := pipeClient(t, func(peer net.Conn) {
		var request struct {
			ID     uint64
			Method string
			Params struct {
				Window int
				Script string
			}
		}
		if err := json.NewDecoder(peer).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Method != "eval" || request.Params.Window != 7 || request.Params.Script != "document.body.textContent" {
			t.Errorf("wrong evaluation: %+v", request)
		}
		response := fmt.Sprintf("{\"id\":%d,\"result\":{\"value\":%q}}\n", request.ID, text)
		for len(response) > 0 {
			n, err := peer.Write([]byte(response[:min(123, len(response))]))
			if err != nil {
				return
			}
			response = response[n:]
		}
	})
	var got string
	if err := c.Eval(t.Context(), 7, "document.body.textContent", &got); err != nil {
		t.Fatal(err)
	}
	if got != text {
		t.Fatal("page result truncated")
	}
}

func TestProtocolErrorsRemainTyped(t *testing.T) {
	c := pipeClient(t, func(peer net.Conn) {
		var req struct{ ID uint64 }
		if json.NewDecoder(peer).Decode(&req) != nil {
			return
		}
		_ = json.NewEncoder(peer).Encode(map[string]any{"id": req.ID, "error": map[string]string{"code": "unsupported", "message": "window is native content"}})
	})
	err := c.Call(t.Context(), "eval", nil, nil)
	var protocol *Error
	if !errors.As(err, &protocol) || protocol.Code != "unsupported" {
		t.Fatalf("protocol error lost: %v", err)
	}
}

func TestProtocolRejectsUnrelatedResponse(t *testing.T) {
	c := pipeClient(t, func(peer net.Conn) {
		var req json.RawMessage
		if json.NewDecoder(peer).Decode(&req) != nil {
			return
		}
		_, _ = fmt.Fprintln(peer, `{"id":42,"result":{}}`)
	})
	if err := c.Call(t.Context(), "windows", nil, nil); err == nil || !strings.Contains(err.Error(), "response ID 42, want 1") {
		t.Fatalf("mismatched response accepted: %v", err)
	}
}

func TestCancellationInterruptsBlockedProtocolRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := pipeClient(t, func(peer net.Conn) {
		var req json.RawMessage
		if json.NewDecoder(peer).Decode(&req) != nil {
			return
		}
		cancel()
		var b [1]byte
		_, _ = peer.Read(b[:])
	})
	if err := c.Call(ctx, "tree", nil, nil); err == nil {
		t.Fatal("blocked server did not fail on cancellation")
	}
}

func TestConditionTimeoutIncludesLastObservedFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err := Until(ctx, "desktop page", func(context.Context) error { return errors.New("process exited before drawing") })
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "process exited before drawing") {
		t.Fatalf("wait lost meaningful diagnostic: %v", err)
	}
}
