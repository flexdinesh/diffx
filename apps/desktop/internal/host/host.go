// Package host serves the desktop build and allowlisted review API on one origin.
// Native transports hold credentials; private control routes are never exposed.
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

	"github.com/flexdinesh/diffx/packages/api"
	"github.com/flexdinesh/diffx/packages/connection"
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
	client   *http.Client
	base     string
	requests *sync.WaitGroup
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

// SetViewer adapts a loopback viewer to the native connection contract.
func (h *Host) SetViewer(viewer string) error {
	u, err := url.Parse(viewer)
	if err != nil || !loopbackOrigin(u) {
		return errors.New("invalid loopback viewer origin")
	}
	return h.SetConnection(connection.Target{Kind: "tcp", URL: u.String()})
}

// SetConnection holds all credentials natively. The webview sees only the
// stable host origin and the shared public review route contract.
func (h *Host) SetConnection(target connection.Target) error {
	transport, err := connection.NewTransport(target)
	if err != nil {
		return errors.New("invalid desktop connection")
	}
	base := "http://localhost"
	if target.Kind != "unix" {
		base = target.URL
	}
	u, err := url.Parse(base)
	if err != nil {
		return errors.New("invalid desktop connection origin")
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme, r.Out.URL.Host = u.Scheme, u.Host
			r.Out.Host = r.In.Host
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Connection unavailable", http.StatusBadGateway)
		},
		ModifyResponse: func(r *http.Response) error {
			if r.StatusCode >= 300 && r.StatusCode < 400 {
				return errors.New("connection redirect refused")
			}
			r.Header.Del("Set-Cookie")
			return nil
		},
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearLocked()
	h.lifetime, h.cancel = context.WithCancel(context.Background())
	context.AfterFunc(h.lifetime, transport.CloseIdleConnections)
	h.target = proxy
	h.requests = &sync.WaitGroup{}
	h.base = base
	h.client = &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return nil
}

// Health probes are reads only. Failed mutations are never replayed.
func (h *Host) Health(ctx context.Context) error {
	h.mu.RLock()
	client, base, lifetime := h.client, h.base, h.lifetime
	h.mu.RUnlock()
	if client == nil {
		return errors.New("connection unavailable")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/health", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("connection unavailable")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("connection unavailable")
	}
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
		h.requests.Wait()
	}
	h.target, h.lifetime, h.cancel = nil, nil, nil
	h.client = nil
	h.base = ""
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
func PublicRoute(method, route string) bool { return api.PublicReviewRoute(method, route) }

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
		target, lifetime, requests := h.target, h.lifetime, h.requests
		if target != nil {
			requests.Add(1)
		}
		h.mu.RUnlock()
		if target == nil {
			http.Error(w, "Local session unavailable", http.StatusServiceUnavailable)
			return
		}
		defer requests.Done()
		ctx, cancel := context.WithCancel(r.Context())
		controller := http.NewResponseController(w)
		disposed := make(chan struct{})
		stop := context.AfterFunc(lifetime, func() {
			cancel()
			// Upstream cancellation cannot interrupt a blocked downstream Write.
			_ = controller.SetWriteDeadline(time.Now())
			close(disposed)
		})
		defer func() {
			if !stop() {
				<-disposed
			}
			cancel()
			_ = controller.SetWriteDeadline(time.Time{})
		}()
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
