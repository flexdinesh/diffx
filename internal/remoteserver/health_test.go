package remoteserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbesAnswerWithoutCredentialsAndReflectStoreReadiness(t *testing.T) {
	store, handler := identityHandler(t, Settings{InsecureCookies: true})
	for _, path := range []string{"/livez", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
			t.Fatalf("%s: %d %q", path, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/readyz", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /readyz: %d", response.Code)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "unavailable\n" {
		t.Fatalf("closed store /readyz: %d %q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("closed store /livez: %d", response.Code)
	}
}
