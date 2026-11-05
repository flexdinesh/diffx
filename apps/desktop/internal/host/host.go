// Package host serves the desktop build and the local viewer API on one origin.
// It never connects to the owner's control socket.
package host

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

type Host struct {
	Origin   string
	server   *http.Server
	done     chan error
	assets   fs.FS
	mu       sync.RWMutex
	target   *httputil.ReverseProxy
	lifetime context.Context
	cancel   context.CancelFunc
}

func Start(assets fs.FS, portFile string) (*Host, error) {
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, errors.New("desktop assets missing; run mise run desktop:stage before building")
	}
	listener, err := listenPersisted(portFile)
	if err != nil {
		return nil, err
	}
	h := &Host{Origin: "http://" + listener.Addr().String(), assets: assets, done: make(chan error, 1)}
	h.server = &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { h.done <- h.server.Serve(listener) }()
	return h, nil
}

// SetViewer disposes all requests to the old scope before publishing the new
// target. Keeping the original Host and Origin lets the sidecar revalidate its
// normal loopback browser policy, including mutations and EventSource requests.
func (h *Host) SetViewer(viewer string) error {
	u, err := url.Parse(viewer)
	if err != nil || !loopbackOrigin(u) {
		return errors.New("invalid loopback viewer origin")
	}
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 20 * time.Second}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme, r.Out.URL.Host = u.Scheme, u.Host
			r.Out.Host = r.In.Host
			// ReverseProxy removes forwarded headers. Never trust or create them.
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Local session unavailable", http.StatusBadGateway)
		},
		ModifyResponse: func(r *http.Response) error {
			if r.StatusCode >= 300 && r.StatusCode < 400 {
				return errors.New("sidecar redirect refused")
			}
			return nil
		},
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearLocked()
	h.lifetime, h.cancel = context.WithCancel(context.Background())
	context.AfterFunc(h.lifetime, transport.CloseIdleConnections)
	h.target = proxy
	return nil
}

func loopbackOrigin(u *url.URL) bool {
	if u == nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Host != "" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}

func (h *Host) clearLocked() {
	if h.cancel != nil {
		h.cancel()
	}
	h.target, h.lifetime, h.cancel = nil, nil, nil
}
func (h *Host) ClearViewer() { h.mu.Lock(); defer h.mu.Unlock(); h.clearLocked() }
func (h *Host) Close() error {
	h.ClearViewer()
	err := h.server.Close()
	serveErr := <-h.done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(err, serveErr)
}

// PublicRoute is deliberately smaller than the server's entire API. Desktop
// only exposes local review operations; no control, MCP, ingestion or auth API.
func PublicRoute(method, route string) bool {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	if path.Clean(route) != route || strings.ContainsAny(route, "\\%") {
		return false
	}
	switch route {
	case "/api/v2/bootstrap", "/api/v2/health", "/api/v2/events", "/api/v2/metrics", "/api/v2/contexts":
		return method == http.MethodGet
	case "/api/v2/collections":
		return method == http.MethodPost
	}
	p := strings.Split(strings.TrimPrefix(route, "/api/v2/contexts/"), "/")
	if !strings.HasPrefix(route, "/api/v2/contexts/") || len(p) == 0 || p[0] == "" {
		return false
	}
	if len(p) == 1 {
		return method == http.MethodGet || method == http.MethodDelete
	}
	if len(p) == 2 {
		switch p[1] {
		case "session", "commits":
			return method == http.MethodGet
		case "comments":
			return method == http.MethodGet || method == http.MethodPost
		case "review-marks":
			return method == http.MethodGet || method == http.MethodDelete
		}
	}
	if len(p) == 3 {
		switch p[1] {
		case "comments":
			if p[2] == "export" {
				return method == http.MethodGet
			}
			if p[2] == "import" {
				return method == http.MethodPost
			}
			return method == http.MethodPatch || method == http.MethodDelete
		case "review-marks":
			return method == http.MethodPut || method == http.MethodDelete
		case "diffs":
			return p[2] == "current" && method == http.MethodGet
		case "review":
			return p[2] == "comments" && method == http.MethodGet
		}
	}
	if len(p) == 4 {
		return method == http.MethodGet && p[1] == "commits" && p[3] == "diff" || method == http.MethodPost && p[1] == "comments" && p[3] == "carry"
	}
	if len(p) == 5 {
		return p[1] == "diffs" && p[3] == "versions" && method == http.MethodGet || p[1] == "review" && p[2] == "comments" && p[4] == "resolve" && method == http.MethodPost
	}
	if len(p) == 6 && (p[1] == "diffs" || p[1] == "commits") && p[3] == "files" && (p[5] == "patch" || p[5] == "contents") {
		return method == http.MethodGet
	}
	return false
}

func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Exact listener authority also excludes DNS rebinding through other names.
	if "http://"+r.Host != h.Origin || len(r.Header.Values("Origin")) > 1 || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != h.Origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Desktop origin denied", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/control/") || strings.HasPrefix(r.URL.Path, "/mcp") {
		if !PublicRoute(r.Method, r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		h.mu.RLock()
		target, lifetime := h.target, h.lifetime
		h.mu.RUnlock()
		if target == nil {
			http.Error(w, "Local session unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(lifetime, cancel)
		defer func() { stop(); cancel() }()
		target.ServeHTTP(w, r.Clone(ctx))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(h.assets, name); err != nil {
		if strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	if name == "index.html" {
		data, err := fs.ReadFile(h.assets, name)
		if err != nil {
			http.Error(w, "Desktop build unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
		return
	}
	clone := r.Clone(r.Context())
	clone.URL.Path = "/" + name
	http.FileServer(http.FS(h.assets)).ServeHTTP(w, clone)
}
