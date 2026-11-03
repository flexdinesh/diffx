package mcpapi

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/reviewservice"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startBridge connects an MCP client to endpoint through Bridge's stdio side.
func startBridge(t *testing.T, server *httptest.Server) (*mcp.ClientSession, chan error) {
	t.Helper()
	toBridge, clientWriter := io.Pipe()
	clientReader, fromBridge := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Bridge(context.Background(), server.Client(), server.URL, toBridge, fromBridge) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-test", Version: "test"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(t.Context(), &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, &mcp.ClientSessionOptions{ProtocolVersion: ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	return session, done
}

func TestBridgeRelaysToolsAndEndsWithItsClient(t *testing.T) {
	service := fakeReviewService{
		list: func(context.Context, bool) ([]reviewservice.Comment, error) { return nil, nil },
		resolve: func(commentID string) (reviewservice.Resolution, error) {
			return reviewservice.Resolution{CommentID: commentID, Status: "resolved"}, nil
		},
	}
	server := httptest.NewServer(New(service, true, "test"))
	defer server.Close()
	session, done := startBridge(t, server)
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) == 0 {
		t.Fatalf("tools through bridge: %+v %v", tools, err)
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "resolve_review_comment", Arguments: map[string]any{"comment_id": "comment-1"}})
	if err != nil || result.IsError {
		t.Fatalf("call through bridge: %+v %v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge after client closed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge outlived its client")
	}
}

func TestBridgeReportsAnUnreachableServer(t *testing.T) {
	server := httptest.NewServer(New(fakeReviewService{}, false, "test"))
	server.Close()
	toBridge, clientWriter := io.Pipe()
	_, fromBridge := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Bridge(context.Background(), server.Client(), server.URL, toBridge, fromBridge) }()
	go func() {
		_, _ = clientWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"))
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unreachable server reported as a clean exit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge hung on an unreachable server")
	}
}
