package connection

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func opaque() string { return base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }

func TestInvitationRoundTripAndSecretFreeErrors(t *testing.T) {
	inv := Invitation{Version: Version, Address: "[2001:db8::1]:7981", RunID: opaque(), Pin: opaque(), Secret: opaque()}
	got, err := Parse(inv.String())
	if err != nil || got != inv {
		t.Fatalf("round trip: %#v %v", got, err)
	}
	for _, input := range []string{inv.String() + "!", "https://user:secret@example.com", "diffx1_", strings.Repeat("x", 8193)} {
		if _, err := Parse(input); err == nil || strings.Contains(err.Error(), input) {
			t.Fatalf("unsafe parse result: %v", err)
		}
	}
	inv.Version++
	if inv.String() != "" {
		t.Fatal("encoded unsupported protocol")
	}
}

func TestTargetValidation(t *testing.T) {
	tls := Target{Kind: "tls", URL: "https://example.com:7981", RunID: opaque(), Pin: opaque(), Credential: opaque()}
	for _, target := range []Target{{Kind: "unix", Socket: "/tmp/diffx.sock"}, {Kind: "tcp", URL: "http://127.0.0.1:1"}, {Kind: "tcp", URL: "http://[::1]:65535"}, tls} {
		transport, err := NewTransport(target)
		if err != nil {
			t.Fatalf("valid target %+v: %v", target, err)
		}
		transport.CloseIdleConnections()
	}
	for _, target := range []Target{
		{}, {Kind: "unix", Socket: "relative"}, {Kind: "unix", Socket: "/tmp/socket", URL: "http://127.0.0.1:1"},
		{Kind: "tcp", URL: "http://localhost:7981"}, {Kind: "tcp", URL: "http://192.0.2.1:7981"}, {Kind: "tcp", URL: "http://127.0.0.1:0"},
		{Kind: "tcp", URL: "http://127.0.0.1:65536"}, {Kind: "tcp", URL: "http://127.0.0.1:1/"}, {Kind: "tcp", URL: "http://user:secret@127.0.0.1:1"},
		{Kind: "tcp", URL: "http://127.0.0.1:1", Credential: opaque()}, {Kind: "tls", URL: tls.URL, RunID: opaque(), Pin: "bad-pin"},
		{Kind: "tls", URL: "http://example.com:7981", RunID: opaque(), Pin: opaque()},
	} {
		if _, err := NewTransport(target); err == nil {
			t.Fatalf("accepted invalid target %+v", target)
		}
	}
	for _, endpoint := range []string{"https://example.com", "https://example.com:1?", "https://example.com:1#secret", "https://example.com:1/path", "https://bad_host:1", "https://-bad:1", "https://example.com:1@evil:1"} {
		target := tls
		target.URL = endpoint
		if _, err := NewTransport(target); err == nil {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
}

func TestNativeUnixAndTCPDialsAndSanitizesBrowserAuthority(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"Cookie", "Authorization", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "X-Diffx-Run"} {
			if r.Header.Get(name) != "" {
				t.Errorf("forwarded %s", name)
			}
		}
		_, _ = io.WriteString(w, r.URL.Path+"?"+r.URL.RawQuery)
	})
	tcp := httptest.NewServer(handler)
	defer tcp.Close()
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	for _, target := range []Target{{Kind: "unix", Socket: socket}, {Kind: "tcp", URL: tcp.URL}} {
		transport, err := NewTransport(target)
		if err != nil {
			t.Fatal(err)
		}
		defer transport.CloseIdleConnections()
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://attacker.invalid/api/v2/bootstrap?a=b", nil)
		for _, name := range []string{"Cookie", "Authorization", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "X-Diffx-Run"} {
			request.Header.Set(name, "browser-value")
		}
		response, err := (&http.Client{Transport: transport}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "/api/v2/bootstrap?a=b" {
			t.Fatalf("native route: %q %v", body, err)
		}
		if request.Header.Get("Authorization") != "browser-value" {
			t.Fatal("mutated caller headers")
		}
	}
}

func TestTransportCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	transport, err := NewTransport(Target{Kind: "tcp", URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithCancel(t.Context())
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/api/v2/events", nil)
	done := make(chan error, 1)
	go func() { _, err := transport.RoundTrip(r); done <- err }()
	<-started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	<-stopped
}
