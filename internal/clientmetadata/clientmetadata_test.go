package clientmetadata

import (
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func tlsServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return server, roots
}

func anyAddress(netip.Addr) bool { return true }

func TestFetchRefusesNonPublicAddresses(t *testing.T) {
	server, roots := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	// The test server listens on loopback, which production guards refuse
	// after resolution, before any request is sent.
	if _, err := newFetcher(Public, roots).Fetch(t.Context(), server.URL+"/client"); !errors.Is(err, ErrAddress) {
		t.Fatalf("loopback fetch: %v", err)
	}
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fe80::1", "fd00::1", "::ffff:10.0.0.1", "64:ff9b::a00:1", "224.0.0.1"} {
		if Public(netip.MustParseAddr(address)) {
			t.Fatalf("%s treated as public", address)
		}
	}
	for _, address := range []string{"93.184.216.34", "2606:2800:220:1::1"} {
		if !Public(netip.MustParseAddr(address)) {
			t.Fatalf("%s refused", address)
		}
	}
}

func TestFetchBoundsDocuments(t *testing.T) {
	server, roots := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("x", MaxBytes+1)))
		case "/redirect":
			http.Redirect(w, r, "/client", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"client_id":"x"}`))
		}
	})
	fetcher := newFetcher(anyAddress, roots)
	body, err := fetcher.Fetch(t.Context(), server.URL+"/client")
	if err != nil || string(body) != `{"client_id":"x"}` {
		t.Fatalf("fetch: %q %v", body, err)
	}
	if _, err := fetcher.Fetch(t.Context(), server.URL+"/large"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized: %v", err)
	}
	if _, err := fetcher.Fetch(t.Context(), server.URL+"/redirect"); !errors.Is(err, ErrStatus) {
		t.Fatalf("redirect followed: %v", err)
	}
	for _, raw := range []string{strings.Replace(server.URL, "https", "http", 1) + "/client", "https://user@" + strings.TrimPrefix(server.URL, "https://") + "/client", "not a url"} {
		if _, err := fetcher.Fetch(t.Context(), raw); !errors.Is(err, ErrURL) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}
