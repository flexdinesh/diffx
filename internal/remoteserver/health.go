package remoteserver

import (
	"context"
	"net/http"
	"time"
)

// probe answers orchestrator health checks without credentials or state:
// /livez while the process serves, /readyz while the store is ready and owned.
func (remote *transport) probe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		problem(w, 405, "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Path == "/readyz" {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := remote.ready(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable\n"))
			return
		}
	}
	_, _ = w.Write([]byte("ok\n"))
}
