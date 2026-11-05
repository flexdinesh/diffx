// Package clientgateway serves browsers through native personal connections.
package clientgateway

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/packages/api"
	"github.com/flexdinesh/diffx/packages/connection"
)

type Gateway struct {
	URL       string
	server    *http.Server
	transport *connection.Transport
	done      chan error
}

func Start(assets fs.FS, target connection.Target, port int) (*Gateway, error) {
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, errors.New("browser assets unavailable")
	}
	transport, err := connection.NewTransport(target)
	if err != nil {
		return nil, err
	}
	var listener net.Listener
	if port < 0 {
		for candidate := 7991; candidate <= 8000; candidate++ {
			listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(candidate)))
			if err == nil {
				break
			}
		}
	} else {
		listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	g := &Gateway{URL: "http://" + listener.Addr().String(), transport: transport, done: make(chan error, 1)}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.Out.URL.Scheme = "http"; r.Out.URL.Host = "localhost" }, Transport: transport, FlushInterval: -1, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Connection unavailable", http.StatusBadGateway)
	}, ModifyResponse: func(r *http.Response) error {
		if r.StatusCode >= 300 && r.StatusCode < 400 {
			return errors.New("upstream redirect refused")
		}
		r.Header.Del("Set-Cookie")
		return nil
	}}
	files := http.FileServer(http.FS(assets))
	g.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if "http://"+r.Host != g.URL || len(r.Header.Values("Origin")) > 1 || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != g.URL || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Browser origin denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/control/") || strings.HasPrefix(r.URL.Path, "/mcp") {
			if !api.PublicReviewRoute(r.Method, r.URL.Path) {
				http.NotFound(w, r)
				return
			}
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(assets, name); err != nil {
			if strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".") {
				http.NotFound(w, r)
				return
			}
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		files.ServeHTTP(w, r)
	})}
	go func() { g.done <- g.server.Serve(listener) }()
	return g, nil
}

func (g *Gateway) Close() error {
	err := g.server.Close()
	g.transport.CloseIdleConnections()
	serveErr := <-g.done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(err, serveErr)
}
