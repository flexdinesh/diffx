package remoteserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/mcpapi"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRunSetupAdminPersistsAcrossRestart(t *testing.T) {
	databaseURL := pgtest.URL(t)
	var logs syncBuffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	var accountID string
	client := &http.Client{Timeout: 3 * time.Second}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithCancel(t.Context())
		ready, done := make(chan string, 1), make(chan error, 1)
		go func() {
			done <- Run(ctx, Settings{DatabaseURL: databaseURL, Listen: "127.0.0.1:0", PublicURL: "https://diffx.example.com", InsecureCookies: true}, func(address string) { ready <- address })
		}()
		var address string
		select {
		case address = <-ready:
		case err := <-done:
			cancel()
			t.Fatalf("start: %v", err)
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("startup timeout")
		}
		path, body := "/api/v2/auth/login", `{"username":"admin","password":"`+testPassword+`"}`
		if attempt == 0 {
			// The link is logged just after ready reports the address.
			var link string
			found := false
			for deadline := time.Now().Add(3 * time.Second); !found && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				_, link, found = strings.Cut(logs.String(), "open https://diffx.example.com/setup#t=")
			}
			if !found {
				cancel()
				t.Fatalf("setup link not logged: %q", logs.String())
			}
			path, body = "/api/v2/setup", `{"token":"`+strings.TrimSpace(link)+`","username":"admin","password":"`+testPassword+`"}`
		} else if strings.Count(logs.String(), "setup#t=") != 1 {
			cancel()
			t.Fatal("setup link logged after admin exists")
		}
		response, err := client.Post("http://"+address+path, "application/json", strings.NewReader(body))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		var result bootstrapJSON
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || result.User == nil || !result.User.Admin {
			cancel()
			t.Fatalf("%s: %v status%d", path, err, response.StatusCode)
		}
		if attempt == 0 {
			accountID = result.User.ID
		} else if accountID != result.User.ID {
			cancel()
			t.Fatal("restart changed identity")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown timeout")
		}
	}
}

// syncBuffer collects log output written by the server goroutine.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *syncBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

type credentialTransport struct {
	base  http.RoundTripper
	token string
}

func (transport credentialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(request)
}

func TestGlobalMCPOwnershipAcrossUsers(t *testing.T) {
	store := pgtest.Store(t)
	tokens := []string{provisionToken(t, store, "admin", identityservice.PurposeAPI), provisionToken(t, store, "other", identityservice.PurposeAPI)}
	handler, closeServices, err := MultiHandler(t.Context(), store, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeServices()
	server := httptest.NewServer(handler)
	defer server.Close()
	patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n"
	input, err := collector.CollectPatch(t.Context(), patch, t.TempDir(), collector.Options{SourceID: "shared", SubmissionID: "same"})
	if err != nil {
		t.Fatal(err)
	}
	contexts := make([]string, 2)
	for index, token := range tokens {
		receipt, err := testsupport.NewClient(server.URL, token).Submit(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		contexts[index] = receipt.ContextID
	}
	for index, token := range tokens {
		client := mcp.NewClient(&mcp.Implementation{Name: "ownership-test", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: credentialTransport{server.Client().Transport, token}}, DisableStandaloneSSE: true}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_contexts", Arguments: map[string]string{}})
		if err != nil || result.IsError {
			t.Fatalf("list_contexts failed: %v", err)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), contexts[index]) || strings.Contains(string(raw), contexts[1-index]) {
			t.Fatal("global MCP catalog crossed user boundary")
		}
		for _, name := range []string{"get_diff", "get_review_comments", "resolve_review_comment", "get_file_patch"} {
			arguments := map[string]string{"context_id": contexts[1-index]}
			if name == "resolve_review_comment" {
				arguments["comment_id"] = "foreign-comment"
			}
			if name == "get_file_patch" {
				arguments["file_id"] = "foreign-file"
			}
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("foreign context accepted by %s", name)
			}
		}
		result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_diff", Arguments: map[string]string{"context_id": contexts[index]}})
		if err != nil || result.IsError {
			t.Fatalf("own global MCP diff failed: %v", err)
		}
	}
}
