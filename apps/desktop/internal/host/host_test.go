package host

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

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
		if "http://"+r.Host != h.Origin || r.Header.Get("Origin") != h.Origin || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			t.Errorf("browser policy changed: host=%s headers=%v", r.Host, r.Header)
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
