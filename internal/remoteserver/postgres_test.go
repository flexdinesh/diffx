//go:build postgres

package remoteserver

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport/postgrestest"
)

func TestRunServesFromPostgresAndRefusesSecondOwner(t *testing.T) {
	url := postgrestest.URL(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Settings{Listen: "127.0.0.1:0", DatabaseURL: url, InsecureCookies: true}, func(address string) { ready <- address })
	}()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("server did not start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server startup timed out")
	}
	response, err := http.Get("http://" + address + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Fatalf("readyz: %d %q", response.StatusCode, body)
	}
	if err := Run(t.Context(), Settings{Listen: "127.0.0.1:0", DatabaseURL: url, InsecureCookies: true}, nil); err == nil {
		t.Fatal("second server opened an owned database")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server shutdown timed out")
	}
}
