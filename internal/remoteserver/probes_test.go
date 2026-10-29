package remoteserver

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

func TestProbesNeedNoCredentialsAndReadinessNeedsTheDatabase(t *testing.T) {
	store, err := pgstore.Open(t.Context(), pgtest.URL(t), pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler, closeServices, err := Handler(t.Context(), store, Settings{}, fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	probe := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		w := probe("GET", path)
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"status":"ok"}` || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q %v", path, w.Code, w.Body.String(), w.Header())
		}
		if w := probe("POST", path); w.Code != 405 {
			t.Fatalf("%s POST: %d", path, w.Code)
		}
	}
	if err := closeServices(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if w := probe("GET", "/healthz"); w.Code != 200 {
		t.Fatalf("liveness touched the database: %d %s", w.Code, w.Body.String())
	}
	w := probe("GET", "/readyz")
	if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"detail":"Not ready"`) || strings.Contains(w.Body.String(), "closed") {
		t.Fatalf("readiness without a database: %d %s", w.Code, w.Body.String())
	}
}
