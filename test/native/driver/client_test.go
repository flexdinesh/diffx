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

func TestProtocolRetainsLargeNodeIDsAndFragmentedFrames(t *testing.T) {
	serverErr := make(chan error, 1)
	c := pipeClient(t, func(peer net.Conn) {
		var request struct {
			ID     uint64
			Method string
		}
		if err := json.NewDecoder(peer).Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request.ID != 1 || request.Method != "tree" {
			serverErr <- fmt.Errorf("request: %+v", request)
			return
		}
		// A uint64 accessibility ID must remain a string; floating-point JSON
		// decoding would round it and act on the wrong native element.
		response := fmt.Sprintf(`{"id":%d,"result":{"window":1,"focus":"18446744073709551614","nodes":[{"id":"18446744073709551614","role":"textField","value":%q}]}}`+"\n", request.ID, strings.Repeat("native", 2000))
		for len(response) > 0 {
			n, err := peer.Write([]byte(response[:min(123, len(response))]))
			if err != nil {
				serverErr <- err
				return
			}
			response = response[n:]
		}
		serverErr <- nil
	})
	tree, err := c.Tree(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Focus != "18446744073709551614" || len(tree.Nodes) != 1 || tree.Nodes[0].ID != tree.Focus || len(tree.Nodes[0].Value) != 12000 {
		t.Fatalf("decoded tree: %+v", tree)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
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
	err := Until(ctx, "native frame", func(context.Context) error { return errors.New("process exited before drawing") })
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "process exited before drawing") {
		t.Fatalf("wait lost meaningful diagnostic: %v", err)
	}
}
