package controlapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOwnerPairingContractAndBrowserRefusal(t *testing.T) {
	var rotations atomic.Int32
	server := httptest.NewServer(NewWithPairing(context.Background(), func() (Status, error) { return Status{State: "running"}, nil }, func() {}, func(advertise string) ([]string, error) {
		rotations.Add(1)
		if advertise != "example.com:7981" {
			t.Errorf("advertise = %q", advertise)
		}
		return []string{"private-invitation"}, nil
	}))
	defer server.Close()
	client := NewClient(dialing(server))
	values, err := client.Pair(t.Context(), "example.com:7981")
	if err != nil || len(values) != 1 || values[0] != "private-invitation" {
		t.Fatalf("private pair: %v %v", values, err)
	}
	for _, header := range []string{"Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode"} {
		r, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/control/v1/pair", strings.NewReader(`{"advertise":"example.com:7981"}`))
		r.Header.Set(header, "browser")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden || strings.Contains(string(body), "private-invitation") {
			t.Fatalf("browser %s: %d", header, response.StatusCode)
		}
	}
	for _, body := range []string{`{"unknown":true}`, `{} {}`, strings.Repeat("x", 4097)} {
		response, err := server.Client().Post(server.URL+"/control/v1/pair", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("malformed private request: %d", response.StatusCode)
		}
	}
	if rotations.Load() != 1 {
		t.Fatalf("rejected requests rotated invitation: %d", rotations.Load())
	}
}
