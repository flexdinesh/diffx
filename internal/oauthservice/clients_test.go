package oauthservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/caller"
)

type documents struct {
	served  map[string]string
	fetches int
}

func (docs *documents) Fetch(_ context.Context, raw string) ([]byte, error) {
	docs.fetches++
	document, ok := docs.served[raw]
	if !ok {
		return nil, errors.New("unavailable")
	}
	return []byte(document), nil
}

func invalidClient(err error) bool {
	var failure *Error
	return errors.As(err, &failure) && failure.Code == "invalid_client" && failure.Redirect == ""
}

func TestMetadataDocumentClients(t *testing.T) {
	const id = "https://app.example/oauth/client.json"
	docs := &documents{served: map[string]string{
		id:                                       `{"client_id":"` + id + `","client_name":"  Agent\nApp  ","redirect_uris":["http://localhost/cb","https://app.example/cb"]}`,
		"https://mismatch.example/client.json":   `{"client_id":"https://app.example/oauth/client.json","redirect_uris":["https://app.example/cb"]}`,
		"https://secret.example/client.json":     `{"client_id":"https://secret.example/client.json","redirect_uris":["https://secret.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"https://scheme.example/client.json":     `{"client_id":"https://scheme.example/client.json","redirect_uris":["javascript:alert(1)"]}`,
		"https://unnamed.example/app/client":     `{"client_id":"https://unnamed.example/app/client","redirect_uris":["https://unnamed.example/cb"]}`,
		"https://broken.example/client.json":     `{"client_id":`,
		"https://no-redirects.example/client.js": `{"client_id":"https://no-redirects.example/client.js","redirect_uris":[]}`,
	}}
	service := New(nil, docs, "https://diffx.example")
	client, err := service.client(t.Context(), id)
	if err != nil || client.Name != "Agent App" || !client.Verified || client.Host() != "app.example" {
		t.Fatalf("client: %+v %v", client, err)
	}
	// Documents are cached briefly, so one authorization fetches once.
	if _, err := service.client(t.Context(), id); err != nil || docs.fetches != 1 {
		t.Fatalf("cached fetch: %d %v", docs.fetches, err)
	}
	if client, err := service.client(t.Context(), "https://unnamed.example/app/client"); err != nil || client.Name != "unnamed.example" {
		t.Fatalf("unnamed client: %+v %v", client, err)
	}
	for _, refused := range []string{
		"https://mismatch.example/client.json", "https://secret.example/client.json", "https://scheme.example/client.json",
		"https://broken.example/client.json", "https://no-redirects.example/client.js", "https://unavailable.example/client.json",
		"https://app.example", "https://app.example/", "https://app.example/a/../client.json", "https://app.example/client.json?x=1",
		"https://user@app.example/client.json",
	} {
		if _, err := service.client(t.Context(), refused); !invalidClient(err) {
			t.Fatalf("%s: %v", refused, err)
		}
	}
}

func TestRedirectMatching(t *testing.T) {
	registered := []string{"http://127.0.0.1/callback", "http://localhost:8080/cb?x=1", "http://[::1]/cb", "https://app.example/cb"}
	for requested, want := range map[string]bool{
		"https://app.example/cb":            true,
		"https://app.example/cb/":           false,
		"https://app.example:8443/cb":       false,
		"http://127.0.0.1/callback":         true,
		"http://127.0.0.1:53682/callback":   true,
		"http://127.0.0.1:53682/other":      false,
		"http://localhost:1/cb?x=1":         true,
		"http://localhost:1/cb":             false,
		"http://localhost:1/callback":       false,
		"http://[::1]:9000/cb":              true,
		"https://127.0.0.1:53682/callback":  false,
		"http://127.0.0.1:53682/callback#x": false,
	} {
		if redirectAllowed(registered, requested) != want {
			t.Fatalf("%s: want %v", requested, want)
		}
	}
	for _, uri := range []string{"http://app.example/cb", "cursor://anysphere.cursor-mcp/oauth/callback", "javascript:alert(1)", "/relative", "https://user@app.example/cb"} {
		if validRedirects([]string{uri}) == nil {
			t.Fatalf("%s accepted", uri)
		}
	}
	if validRedirects(make([]string, 11)) == nil {
		t.Fatal("eleven redirect URIs accepted")
	}
}

func TestRequestedScopes(t *testing.T) {
	read, write := caller.ScopeReviewsRead, caller.ScopeCommentsWrite
	for _, test := range []struct {
		scope string
		given bool
		want  string
	}{
		{"", false, read + " " + write},
		{"", true, read},
		{write, true, read + " " + write},
		{"offline_access " + read, true, read},
		{"unknown", true, read},
	} {
		if got := strings.Join(requestedScopes(test.scope, test.given), " "); got != test.want {
			t.Fatalf("%q given=%v: %q want %q", test.scope, test.given, got, test.want)
		}
	}
}

func TestPKCEFormats(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	if !validVerifier(verifier) || validVerifier(verifier[:42]) || validVerifier(strings.Repeat("a", 129)) || validVerifier(verifier+"/") {
		t.Fatal("verifier rules")
	}
	if !validChallenge(challengeOf(verifier)) || validChallenge("short") || validChallenge(strings.Repeat("=", 43)) {
		t.Fatal("challenge rules")
	}
}
