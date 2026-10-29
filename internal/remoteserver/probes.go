package remoteserver

import (
	"context"
	"net/http"
	"time"
)

// readinessTimeout fails a probe quickly instead of waiting on a busy pool.
const readinessTimeout = 2 * time.Second

type pinger interface {
	Ping(ctx context.Context) error
}

// probes answers unauthenticated deployment probes before identity: /healthz
// reports a serving process without touching the database, and /readyz also
// requires a database that accepts queries. Neither explains a failure.
func probes(database pinger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			problem(w, 405, "Method not allowed")
			return
		}
		if r.URL.Path == "/readyz" {
			ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
			defer cancel()
			if database.Ping(ctx) != nil {
				problem(w, 503, "Not ready")
				return
			}
		}
		writeJSON(w, 200, struct {
			Status string `json:"status"`
		}{"ok"})
	})
}
