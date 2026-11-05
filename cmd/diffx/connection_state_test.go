package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/pairing"
	"github.com/flexdinesh/diffx/packages/connection"
)

func TestNativeCredentialCacheReusesConsumedInvitation(t *testing.T) {
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	identity, err := pairing.New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(identity.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/health" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})))
	server.TLS = identity.TLSConfig()
	server.StartTLS()
	defer server.Close()
	invitations, err := identity.Rotate([]string{strings.TrimPrefix(server.URL, "https://")})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := connection.Parse(invitations[0])
	if err != nil {
		t.Fatal(err)
	}
	first, err := pairedTarget(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Pair(t.Context(), inv); err == nil {
		t.Fatal("invitation was reusable")
	}
	second, err := pairedTarget(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	if first.Credential == "" || second != first {
		t.Fatal("cached connection did not preserve credential")
	}
	files, err := filepath.Glob(filepath.Join(os.Getenv("DIFFX_RUNTIME_DIR"), "connections", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files: %v %v", files, err)
	}
	info, err := os.Stat(files[0])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v %v", info, err)
	}
	file, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var cached connection.Target
	if json.NewDecoder(file).Decode(&cached) != nil || cached.Credential != first.Credential {
		t.Fatal("credential not persisted natively")
	}
	// A structurally valid but refused cache entry must be repairable using a
	// new invitation, rather than permanently shadowing it.
	cached.Credential = strings.Repeat("A", 43)
	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := identity.Rotate([]string{inv.Address})
	if err != nil {
		t.Fatal(err)
	}
	freshInvitation, err := connection.Parse(fresh[0])
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := pairedTarget(t.Context(), freshInvitation)
	if err != nil || repaired.Credential == cached.Credential || repaired.Credential == first.Credential {
		t.Fatalf("refused credential did not recover: %v", err)
	}
}

func TestConnectionArgumentsRejectSecretsWithoutEcho(t *testing.T) {
	secret := "diffx1_sensitive-invalid-payload"
	_, err := parseConnect([]string{secret}, io.Discard)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid invitation leaked: %v", err)
	}
	for _, args := range [][]string{{"ssh://host", "--browser", "--no-browser"}, {"ssh://host", "--port", "65536"}} {
		if _, err := parseConnect(args, io.Discard); err == nil {
			t.Fatalf("invalid options accepted: %v", args)
		}
	}
}

func TestRemoteArgumentsKeepPathsAndAccessSeparate(t *testing.T) {
	args, network, err := remoteArguments([]string{"workspace", "--listen", ":0", "--advertise=review.example:443", "--base", "HEAD"})
	if err != nil || strings.Join(args, " ") != "workspace --base HEAD" || network.Listen != ":0" || network.Advertise != "review.example:443" {
		t.Fatalf("arguments=%v network=%+v error=%v", args, network, err)
	}
	args, network, err = remoteArguments([]string{"--", "status"})
	if err != nil || strings.Join(args, " ") != "-- status" || network.Listen != ":7981" {
		t.Fatalf("escaped path: %v %+v %v", args, network, err)
	}
}
