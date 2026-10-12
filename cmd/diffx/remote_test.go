package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/webui"
)

func TestRemoteStatusRequiresRemoteModeAndVerifiesCredentials(t *testing.T) {
	t.Setenv("DIFFX_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	t.Setenv("DIFFX_REMOTE_URL", "")
	t.Setenv("DIFFX_REMOTE_TOKEN", "")
	if err := run(t.Context(), []string{"remote", "status"}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "remote.url") {
		t.Fatalf("unconfigured remote: %v", err)
	}
	token := strings.Repeat("b", 40)
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler, closeServices, err := remoteserver.Handler(t.Context(), store, "admin", token, webui.Assets())
	if err != nil {
		t.Fatal(err)
	}
	defer closeServices()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("DIFFX_REMOTE_URL", server.URL)
	t.Setenv("DIFFX_REMOTE_TOKEN", "wrong-token")
	if err := run(t.Context(), []string{"remote", "status"}, nil, io.Discard, io.Discard); err == nil || strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("rejected credential: %v", err)
	}
	t.Setenv("DIFFX_REMOTE_TOKEN", token)
	var output bytes.Buffer
	if err := run(t.Context(), []string{"remote", "status"}, nil, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), server.URL) || !strings.Contains(output.String(), "server:  ok") || strings.Contains(output.String(), token) {
		t.Fatalf("status output: %s", output.String())
	}
}
