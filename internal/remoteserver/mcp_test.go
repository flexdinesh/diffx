package remoteserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/mcpapi"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sessionTransport authenticates requests as a browser session would.
type sessionTransport struct {
	base    http.RoundTripper
	session identityservice.Session
}

func (transport sessionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.AddCookie(&http.Cookie{Name: authhttp.SessionCookie, Value: transport.session.Secret})
	request.Header.Set("X-CSRF-Token", transport.session.CSRFToken)
	return transport.base.RoundTrip(request)
}

// Sessions and API tokens reach MCP as callers, so agent writes name the
// client and, for API tokens, the token.
func TestMCPWritesNameTheAuthenticatedCaller(t *testing.T) {
	store := pgtest.Store(t)
	account := testsupport.RemoteAccount(t, store, "writer")
	token, err := identityservice.New(store).CreateToken(t.Context(), account.ID, "laptop token", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	session := testsupport.RemoteSession(t, store, account)
	handler, closeServices, err := MultiHandler(t.Context(), store, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeServices()
	server := httptest.NewServer(handler)
	defer server.Close()
	patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n"
	input, err := collector.CollectPatch(t.Context(), patch, t.TempDir(), collector.Options{SourceID: "source", SubmissionID: "submission"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := testsupport.NewClient(server.URL, token.Secret).Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		transport http.RoundTripper
		want      string
	}{
		{"api token", credentialTransport{server.Client().Transport, token.Secret}, "Claude Code (laptop token)"},
		{"session", sessionTransport{server.Client().Transport, session}, "Claude Code"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Title: "Claude Code", Version: "1"}, nil)
			mcpSession, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: test.transport}, DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
			if err != nil {
				t.Fatal(err)
			}
			defer mcpSession.Close()
			result, err := mcpSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "create_review_comment", Arguments: map[string]any{
				"context_id": receipt.ContextID, "path": "file.txt", "target": "file", "body": test.name,
			}})
			if err != nil || result.IsError {
				t.Fatalf("create: %#v %v", result, err)
			}
			var created struct {
				Comment review.ReviewComment `json:"comment"`
			}
			raw, err := json.Marshal(result.StructuredContent)
			if err == nil {
				err = json.Unmarshal(raw, &created)
			}
			if err != nil {
				t.Fatal(err)
			}
			if author := created.Comment.Author; author == nil || author.Kind != review.AuthorAgent || author.Name != test.want || author.Verified {
				t.Fatalf("author = %#v, want %q", author, test.want)
			}
		})
	}
}
