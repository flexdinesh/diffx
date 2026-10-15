package serverapp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

func TestLocalIdentityIsAutomaticWithoutAccountManagement(t *testing.T) {
	t.Setenv("DIFFX_SERVER_SIGNUP_ENABLED", "true")
	store, err := reviewstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner, err := store.User("local-os-user", "Local user")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.NewWithContext(context.Background(), store, owner)
	defer service.Close()
	handler := serverapp.Handler(t.Context(), service, store, fstest.MapFS{}, "")
	request := httptest.NewRequest(http.MethodGet, "/api/v2/bootstrap", nil)
	request.Header.Set("Authorization", "Bearer unrelated-remote-credential")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var bootstrap struct {
		Mode          string          `json:"mode"`
		SignupEnabled bool            `json:"signupEnabled"`
		User          json.RawMessage `json:"user"`
		CSRFToken     json.RawMessage `json:"csrfToken"`
		Collect       json.RawMessage `json:"collect"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || bootstrap.Mode != "local" || bootstrap.SignupEnabled || string(bootstrap.User) != "null" || string(bootstrap.CSRFToken) != "null" || string(bootstrap.Collect) != "false" {
		t.Fatalf("local composition exposed remote identity: %d %s", response.Code, response.Body.String())
	}
	for method, paths := range map[string][]string{
		http.MethodPost: {"/api/v2/auth/signup", "/api/v2/auth/login", "/api/v2/auth/logout", "/api/v2/account/tokens"},
		http.MethodGet:  {"/api/v2/account/tokens"},
		http.MethodPut:  {"/api/v2/account/password"},
	} {
		for _, path := range paths {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("local management route %s %s: %d", method, path, response.Code)
			}
		}
	}
	accounts, err := store.IdentityAccounts(t.Context())
	if err != nil || len(accounts) != 0 {
		t.Fatalf("local composition created remote accounts: %+v %v", accounts, err)
	}
}
