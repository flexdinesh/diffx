package mcpapi

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startBridge connects an MCP client speaking protocolVersion to endpoint
// through Bridge's stdio side.
func startBridge(t *testing.T, server *httptest.Server, protocolVersion string) (*mcp.ClientSession, chan error) {
	t.Helper()
	toBridge, clientWriter := io.Pipe()
	clientReader, fromBridge := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Bridge(context.Background(), server.Client(), server.URL, toBridge, fromBridge) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-test", Version: "test"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(t.Context(), &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	return session, done
}

func TestBridgeRelaysToolsAndEndsWithItsClient(t *testing.T) {
	f := newFixture(t)
	head := f.ingest(observation(featureCheckout, "one", 100))
	f.comment(head.ID, "deadbeef01aa", "Check this.")
	server := f.serve("", "", nil)
	for _, version := range ProtocolVersions {
		t.Run(version, func(t *testing.T) {
			session, done := startBridge(t, server, version)
			tools, err := session.ListTools(t.Context(), nil)
			if err != nil || len(tools.Tools) == 0 {
				t.Fatalf("tools through bridge: %+v %v", tools, err)
			}
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "find_feedback", Arguments: map[string]any{"ref": "deadbeef01"}})
			if err != nil || result.IsError || !strings.Contains(fmt.Sprint(result.StructuredContent), "deadbeef01aa") {
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
		})
	}
}

func TestBridgeReportsAnUnreachableServer(t *testing.T) {
	server := newFixture(t).serve("", "", nil)
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
