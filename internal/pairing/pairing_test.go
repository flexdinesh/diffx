package pairing

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/connection"
)

func start(t *testing.T, app http.Handler) (*Server, *httptest.Server, connection.Invitation) {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.Handler(app))
	server.TLS = s.TLSConfig()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	values, err := s.Rotate([]string{server.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := connection.Parse(values[0])
	if err != nil {
		t.Fatal(err)
	}
	return s, server, inv
}

func targetClient(t *testing.T, target connection.Target) *http.Client {
	t.Helper()
	transport, err := connection.NewTransport(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func get(t *testing.T, client *http.Client, path string, want int) {
	t.Helper()
	response, err := client.Get("https://localhost" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("%s: status %d, want %d", path, response.StatusCode, want)
	}
}

func TestRealTLSPinPairCredentialReuseRotationAndRouteDenial(t *testing.T) {
	var calls atomic.Int32
	s, server, inv := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS == nil || r.Header.Get("Authorization") == "" {
			t.Error("missing native TLS authority")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	badPin := inv
	badPin.Pin = secret()
	if _, err := connection.Pair(t.Context(), badPin); err == nil || strings.Contains(err.Error(), inv.Secret) {
		t.Fatalf("pin mismatch: %v", err)
	}
	target, err := connection.Pair(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	if target.Credential == "" || target.RunID != inv.RunID || target.Pin != inv.Pin {
		t.Fatal("incomplete native target")
	}
	client := targetClient(t, target)
	for range 3 {
		get(t, client, "/api/v2/bootstrap", http.StatusNoContent)
	}
	if _, err := connection.Pair(t.Context(), inv); err == nil {
		t.Fatal("replayed invitation")
	}
	values, err := s.Rotate([]string{server.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := connection.Parse(values[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Pair(t.Context(), rotated); err != nil {
		t.Fatal(err)
	}
	get(t, client, "/api/v2/bootstrap", http.StatusNoContent)
	for _, path := range []string{"/control/v1/status", "/control/v1/shutdown", "/control/v1/pair", "/mcp", "/api/v2/account", "/api/v2/auth/session", "/api/v2/ingestion-jobs", "/api/v2/contexts/../account", "/api/v2/contexts/%61", "/"} {
		get(t, client, path, http.StatusNotFound)
	}
	if calls.Load() != 4 {
		t.Fatalf("private route reached app: %d", calls.Load())
	}
	noCredential := target
	noCredential.Credential = ""
	get(t, targetClient(t, noCredential), "/api/v2/bootstrap", http.StatusUnauthorized)
	wrongRun := target
	wrongRun.RunID = secret()
	get(t, targetClient(t, wrongRun), "/api/v2/bootstrap", http.StatusUnauthorized)
	_, _, restarted := start(t, http.NotFoundHandler())
	stale := target
	stale.URL = "https://" + restarted.Address
	stale.Pin = restarted.Pin
	get(t, targetClient(t, stale), "/api/v2/bootstrap", http.StatusUnauthorized)
}

func TestInvitationConcurrentConsumptionExpiryAndRotation(t *testing.T) {
	s, server, inv := start(t, http.NotFoundHandler())
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if _, err := connection.Pair(t.Context(), inv); err == nil {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("atomic consumption: %d accepted", accepted.Load())
	}
	// A fresh run avoids coupling expiry to the independent request limiter.
	s, server, inv = start(t, http.NotFoundHandler())
	values, err := s.Rotate([]string{server.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Pair(t.Context(), inv); err == nil {
		t.Fatal("rotation retained unused invitation")
	}
	inv, _ = connection.Parse(values[0])
	s.mu.Lock()
	s.expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, err := connection.Pair(t.Context(), inv); err == nil {
		t.Fatal("accepted expired invitation")
	}
}

func TestPairingBoundsMalformedInputAndBrowserDenial(t *testing.T) {
	s, server, inv := start(t, http.NotFoundHandler())
	client := targetClient(t, connection.Target{Kind: "tls", URL: server.URL, RunID: inv.RunID, Pin: inv.Pin})
	for i := range AttemptsPerMinute + 1 {
		response, err := client.Post(server.URL+connection.PairPath, "application/json", strings.NewReader("{invalid"))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusBadRequest
		if i == AttemptsPerMinute {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("attempt %d: %d want %d", i, response.StatusCode, want)
		}
	}
	// Advancing the rate window restores admission, without restoring consumed invitations.
	s.mu.Lock()
	s.window = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if _, err := connection.Pair(t.Context(), inv); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{strings.Repeat("x", 4097), `{"version":1,"unknown":true}`, `{} {}`} {
		response, err := client.Post(server.URL+connection.PairPath, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || strings.Contains(string(data), inv.Secret) {
			t.Fatalf("malformed response %d %q", response.StatusCode, data)
		}
	}
	// Bypass the native adapter to prove browser headers are refused at admission.
	raw := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	defer raw.CloseIdleConnections()
	for _, header := range []string{"Origin", "Cookie", "Sec-Fetch-Site", "Sec-Fetch-Mode"} {
		r, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v2/bootstrap", nil)
		r.Header.Set(header, "browser")
		response, err := raw.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("browser %s: %d", header, response.StatusCode)
		}
	}
	plain := httptest.NewRecorder()
	s.Handler(http.NotFoundHandler()).ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "http://localhost/api/v2/bootstrap", nil))
	if plain.Code != http.StatusForbidden {
		t.Fatal("admitted plaintext")
	}
}

func TestCredentialMemoryBoundAndExactExpiry(t *testing.T) {
	s, _, inv := start(t, http.NotFoundHandler())
	s.mu.Lock()
	now := s.expires
	s.now = func() time.Time { return now }
	s.mu.Unlock()
	input := connection.PairRequest{Version: connection.Version, RunID: inv.RunID, Secret: inv.Secret}
	if _, status := s.exchange(input); status != http.StatusUnauthorized {
		t.Fatal("accepted exact expiry")
	}
	s.mu.Lock()
	s.expires = now.Add(time.Minute)
	for range MaxCredentials {
		s.credentials[sha256.Sum256([]byte(secret()))] = struct{}{}
	}
	s.mu.Unlock()
	if _, status := s.exchange(input); status != http.StatusTooManyRequests {
		t.Fatal("credential memory unbounded")
	}
}

func TestTLSEventsCancel(t *testing.T) {
	stopped := make(chan struct{})
	_, _, inv := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	target, err := connection.Pair(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	client := targetClient(t, target)
	ctx, cancel := context.WithCancel(t.Context())
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost/api/v2/events", nil)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(": connected\n\n"))
	if _, err := io.ReadFull(response.Body, data); err != nil {
		t.Fatal(err)
	}
	cancel()
	response.Body.Close()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("TLS stream survived cancellation")
	}
}
