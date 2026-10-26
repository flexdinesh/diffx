package google

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func idToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestAuthorizeURLUsesPKCEAndNonce(t *testing.T) {
	client := New("client", "secret", "https://diffx.example.com/api/v2/auth/google/callback")
	parsed, err := url.Parse(client.AuthorizeURL("state", "nonce", "verifier"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "accounts.google.com" || query.Get("state") != "state" || query.Get("nonce") != "nonce" || query.Get("code_challenge") != Challenge("verifier") ||
		query.Get("code_challenge_method") != "S256" || query.Get("scope") != "openid email" || query.Get("redirect_uri") != "https://diffx.example.com/api/v2/auth/google/callback" {
		t.Fatalf("authorize URL: %s", parsed)
	}
	if query.Get("code_challenge") == "verifier" {
		t.Fatal("verifier leaked into the browser URL")
	}
}

func TestExchangeValidatesIDTokenClaims(t *testing.T) {
	valid := map[string]any{"iss": "https://accounts.google.com", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce", "sub": "subject", "email": "a@example.com", "email_verified": true}
	var claims map[string]any
	var form url.Values
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		form = r.PostForm
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken(t, claims)})
	}))
	defer server.Close()
	client := New("client", "secret", "https://diffx.example.com/callback", WithEndpoints(server.URL+"/auth", server.URL+"/token"))
	claims = valid
	identity, err := client.Exchange(t.Context(), "code", "verifier", "nonce")
	if err != nil || identity != (Identity{Subject: "subject", Email: "a@example.com", EmailVerified: true}) {
		t.Fatalf("exchange: %+v %v", identity, err)
	}
	if form.Get("code_verifier") != "verifier" || form.Get("client_secret") != "secret" || form.Get("grant_type") != "authorization_code" || form.Get("redirect_uri") != "https://diffx.example.com/callback" {
		t.Fatalf("token request: %v", form)
	}
	for name, change := range map[string]func(map[string]any){
		"issuer":   func(c map[string]any) { c["iss"] = "https://evil.example.com" },
		"audience": func(c map[string]any) { c["aud"] = "other" },
		"expiry":   func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
		"nonce":    func(c map[string]any) { c["nonce"] = "replayed" },
		"subject":  func(c map[string]any) { delete(c, "sub") },
	} {
		claims = map[string]any{}
		for key, value := range valid {
			claims[key] = value
		}
		change(claims)
		if _, err := client.Exchange(t.Context(), "code", "verifier", "nonce"); !errors.Is(err, ErrRejected) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	claims, status = valid, http.StatusBadRequest
	if _, err := client.Exchange(t.Context(), "code", "verifier", "nonce"); !errors.Is(err, ErrRejected) {
		t.Fatalf("rejected code accepted: %v", err)
	}
	if _, err := client.Exchange(t.Context(), "", "verifier", "nonce"); !errors.Is(err, ErrRejected) {
		t.Fatalf("missing code accepted: %v", err)
	}
}
