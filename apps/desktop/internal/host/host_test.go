package host

import (
	"bufio"
	"context"
	"fmt"
	"github.com/flexdinesh/diffx/packages/connection"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

type stalledResponse struct {
	header            http.Header
	entered, released chan struct{}
	once              sync.Once
}

func (w *stalledResponse) Header() http.Header { return w.header }
func (w *stalledResponse) WriteHeader(int)     {}
func (w *stalledResponse) Write([]byte) (int, error) {
	close(w.entered)
	<-w.released
	return 0, io.ErrClosedPipe
}
func (w *stalledResponse) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		w.once.Do(func() { close(w.released) })
	}
	return nil
}

func TestDisposalInterruptsStalledDownstreamWrite(t *testing.T) {
	h := newHost(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "review body") }))
	defer upstream.Close()
	if err := h.SetViewer(upstream.URL); err != nil {
		t.Fatal(err)
	}
	w := &stalledResponse{header: make(http.Header), entered: make(chan struct{}), released: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, h.Origin+"/api/v2/bootstrap", nil))
	}()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("downstream never blocked")
	}
	cleared := make(chan struct{})
	go func() { h.ClearViewer(); close(cleared) }()
	select {
	case <-cleared:
	case <-time.After(time.Second):
		t.Fatal("stalled browser blocked connection disposal")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old response survived disposal")
	}
}

func newHost(t *testing.T) *Host {
	t.Helper()
	h, err := Start(fstest.MapFS{"index.html": {Data: []byte("<main>desktop</main>")}, "assets/app.js": {Data: []byte("export {}")}}, filepath.Join(t.TempDir(), "desktop-origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	return h
}

func TestHostPreservesBrowserPolicyAndOnlyProxiesPublicReview(t *testing.T) {
	h := newHost(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == strings.TrimPrefix(h.Origin, "http://") || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			t.Errorf("native transport leaked browser authority: host=%s headers=%v", r.Host, r.Header)
		}
		if r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-Host") != "" {
			t.Error("trusted forged forwarded headers")
		}
		if r.URL.Path == "/api/v2/contexts/review/comments/export" {
			http.Redirect(w, r, "http://attacker.example", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"method":%q,"path":%q}`, r.Method, r.URL.Path)
	}))
	defer upstream.Close()
	if err := h.SetViewer(upstream.URL); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, route string
		code          int
	}{
		{"GET", "/api/v2/bootstrap", 200}, {"POST", "/api/v2/collections", 200},
		{"POST", "/api/v2/contexts/review/comments", 200}, {"PATCH", "/api/v2/contexts/review/comments/comment", 200},
		{"PUT", "/api/v2/contexts/review/review-marks/file", 200}, {"DELETE", "/api/v2/contexts/review/review-marks", 200},
		{"POST", "/control/v1/shutdown", 404}, {"GET", "/control/v1/status", 404},
		{"POST", "/mcp", 404}, {"POST", "/api/v2/ingestion-jobs", 404},
		{"POST", "/api/v2/auth/login", 404}, {"POST", "/api/v2/contexts", 404},
		{"GET", "/api/v2/contexts/review/comments/export", 502},
	} {
		t.Run(tc.method+tc.route, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, h.Origin+tc.route, nil)
			r.Header.Set("Origin", h.Origin)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Forwarded", "host=attacker.example")
			r.Header.Set("X-Forwarded-Host", "attacker.example")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("status=%d want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Error("CORS was relaxed")
			}
		})
	}
	for _, headers := range []map[string]string{{"Host": "attacker.example"}, {"Host": "localhost:42"}, {"Origin": "null"}, {"Origin": "http://127.0.0.1:42"}, {"Sec-Fetch-Site": "cross-site"}} {
		r := httptest.NewRequest("POST", h.Origin+"/api/v2/collections", nil)
		for key, value := range headers {
			if key == "Host" {
				r.Host = value
			} else {
				r.Header.Set(key, value)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("accepted untrusted browser request %v: %d", headers, w.Code)
		}
	}
}

func TestAssetsAndNoSession(t *testing.T) {
	h := newHost(t)
	for _, tc := range []struct {
		route string
		code  int
		text  string
	}{
		{"/", 200, "<main>desktop</main>"}, {"/streams/example", 200, "<main>desktop</main>"},
		{"/assets/app.js", 200, "export {}"}, {"/assets/missing.js", 404, ""}, {"/api/v2/bootstrap", 503, ""},
	} {
		response, err := http.Get(h.Origin + tc.route)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != tc.code || !strings.Contains(string(data), tc.text) {
			t.Fatalf("%s: %s %q %v", tc.route, response.Status, data, err)
		}
		if tc.code == 200 && response.Header.Get("Content-Security-Policy") == "" {
			t.Error("missing desktop content policy")
		}
	}
	for _, target := range []string{"http://attacker.example", "http://127.0.0.1/control", "http://user@localhost", "https://localhost", "http://127.0.0.1?redirect=true"} {
		if err := h.SetViewer(target); err == nil {
			t.Fatalf("accepted viewer %s", target)
		}
	}
}

func TestScopeDisposalCancelsStreamingAndInflightRequests(t *testing.T) {
	h := newHost(t)
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	if err := h.SetViewer(upstream.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", h.Origin+"/api/v2/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("SSE did not flush: %q %v", line, err)
	}
	h.ClearViewer()
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("old SSE request survived scope disposal")
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Error("old stream reported clean EOF rather than cancellation")
	}
}

func TestRouteCanonicalization(t *testing.T) {
	for _, route := range []string{"/api/v2/contexts/../control", "/api/v2/contexts//comments", "/api/v2/contexts/%2e%2e/comments", "/api/v2/contexts/a\\b/comments", "/control/v1/shutdown"} {
		if PublicRoute("POST", route) {
			t.Errorf("accepted %s", route)
		}
	}
}

func TestSocketOnlyNativeProxyStripsBrowserCredentialsAndNeverExposesControl(t *testing.T) {
	h := newHost(t)
	socket := filepath.Join(t.TempDir(), "server.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 4)
	upstream := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "localhost" || r.Header.Get("Origin") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("webview authority escaped native proxy")
		}
		requests <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})}
	go upstream.Serve(listener)
	defer upstream.Close()
	if err := h.SetConnection(connection.Target{Kind: "unix", Socket: socket}); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/v2/collections", "/control/v1/shutdown"} {
		request := httptest.NewRequest(http.MethodPost, h.Origin+route, nil)
		request.Header.Set("Authorization", "Bearer forged-browser-token")
		request.Header.Set("Cookie", "forged-browser-cookie")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		want := http.StatusOK
		if route == "/control/v1/shutdown" {
			want = http.StatusNotFound
		}
		if response.Code != want {
			t.Fatalf("route %s: %d", route, response.Code)
		}
	}
	if got := <-requests; got != "/api/v2/collections" {
		t.Fatal(got)
	}
	select {
	case route := <-requests:
		t.Fatalf("unexpected mutation or private request: %s", route)
	default:
	}
}
