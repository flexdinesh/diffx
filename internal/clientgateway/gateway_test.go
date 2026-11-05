package clientgateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flexdinesh/diffx/packages/connection"
)

func TestGatewayAllowsReviewAndRefusesOtherAuthority(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-Host") != "" {
			t.Errorf("untrusted browser authority forwarded: %v", r.Header)
		}
		w.Header().Set("Set-Cookie", "secret=untrusted")
		_, _ = io.WriteString(w, "review")
	}))
	defer upstream.Close()
	gateway, err := Start(fstest.MapFS{"index.html": {Data: []byte("workspace")}}, connection.Target{Kind: "tcp", URL: upstream.URL}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	for route, want := range map[string]int{"/api/v2/bootstrap": 200, "/control/v1/status": 404, "/api/v2/auth/login": 404, "/mcp": 404, "/api/v2/connections/pair": 404, "/streams/review": 200} {
		r, err := http.NewRequest(http.MethodGet, gateway.URL+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", gateway.URL)
		r.Header.Set("Cookie", "secret=browser")
		r.Header.Set("Authorization", "Bearer browser")
		r.Header.Set("Forwarded", "host=attacker")
		r.Header.Set("X-Forwarded-Host", "attacker")
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != want || response.Header.Get("Set-Cookie") != "" {
			t.Fatalf("%s: %d %s %v", route, response.StatusCode, data, readErr)
		}
	}
	for _, origin := range []string{"null", "https://attacker.example"} {
		r, _ := http.NewRequest(http.MethodGet, gateway.URL+"/api/v2/bootstrap", nil)
		r.Header.Set("Origin", origin)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("cross-origin browser reached native credential")
		}
	}
}

func TestGatewayRefusesRedirectAndPreservesMissingAsset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://attacker.example", 302) }))
	defer upstream.Close()
	g, err := Start(fstest.MapFS{"index.html": {Data: []byte("workspace")}}, connection.Target{Kind: "tcp", URL: upstream.URL}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for route, want := range map[string]int{"/api/v2/bootstrap": 502, "/assets/missing.js": 404} {
		response, err := http.Get(g.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != want || strings.Contains(string(data), "attacker") {
			t.Fatalf("%s: %d %s", route, response.StatusCode, data)
		}
	}
}
