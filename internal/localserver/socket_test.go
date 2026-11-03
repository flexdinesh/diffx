package localserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
)

type foreground struct {
	status Status
	done   chan error
}

// startForeground runs a foreground instance in the runtime directory and
// waits for readiness; confirm answers a replacement prompt.
func startForeground(t *testing.T, dir string, settings Settings, confirm func(Status) (bool, error)) foreground {
	t.Helper()
	t.Setenv("DIFFX_RUNTIME_DIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan Status, 1)
	done := make(chan error, 1)
	stopped := make(chan struct{})
	settings.State = "memory"
	go func() {
		defer close(stopped)
		done <- RunForeground(ctx, settings, Input{}, confirm, StateReset{}, func(status Status, _ Published) { ready <- status })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(8 * time.Second):
			t.Error("foreground did not stop")
		}
	})
	select {
	case status := <-ready:
		return foreground{status: status, done: done}
	case err := <-done:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	return foreground{}
}

func request(t *testing.T, client *http.Client, method, url string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		if key == "Host" {
			req.Host = value
			continue
		}
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func TestSocketServesAppAndControlWhileViewerServesOnlyTheApp(t *testing.T) {
	dir := runtimeDir(t)
	instance := startForeground(t, dir, Settings{Port: 0}, nil)
	client := &Client{RuntimeDirectory: dir}
	status, err := client.Status(t.Context())
	if err != nil || status.State != "running" || status.Socket != SocketPath(dir) || status.ViewerURL == "" || status.InstanceID != instance.status.InstanceID || status.ProtocolVersion != 6 {
		t.Fatalf("socket status: %+v %v", status, err)
	}
	info, err := os.Lstat(status.Socket)
	if err != nil || info.Mode().Type() != fs.ModeSocket || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode: %v %v", info, err)
	}
	socket := client.HTTPClient()
	viewer := &http.Client{Timeout: 5 * time.Second}
	for _, scenario := range []struct {
		name    string
		client  *http.Client
		base    string
		headers map[string]string
		want    int
	}{
		{"socket app", socket, "http://localhost", nil, http.StatusOK},
		{"socket via forwarded port", socket, "http://localhost", map[string]string{"Host": "127.0.0.1:7999", "Origin": "http://127.0.0.1:7999"}, http.StatusOK},
		{"socket DNS rebinding", socket, "http://localhost", map[string]string{"Host": "attacker.example:7999"}, http.StatusForbidden},
		{"socket cross origin", socket, "http://localhost", map[string]string{"Origin": "http://attacker.example"}, http.StatusForbidden},
		{"socket cross site", socket, "http://localhost", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"viewer app", viewer, status.ViewerURL, nil, http.StatusOK},
		{"viewer DNS rebinding", viewer, status.ViewerURL, map[string]string{"Host": "attacker.example"}, http.StatusForbidden},
		{"viewer other local origin", viewer, status.ViewerURL, map[string]string{"Origin": "http://127.0.0.1:3000"}, http.StatusForbidden},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if code, body := request(t, scenario.client, http.MethodGet, scenario.base+"/api/v2/bootstrap", scenario.headers); code != scenario.want {
				t.Fatalf("status %d, want %d: %s", code, scenario.want, body)
			}
		})
	}
	// Control is only on the socket, and refuses browsers there too.
	if code, _ := request(t, viewer, http.MethodPost, status.ViewerURL+"/control/v1/shutdown", nil); code != http.StatusNotFound {
		t.Fatalf("viewer exposed control: %d", code)
	}
	if code, _ := request(t, socket, http.MethodPost, "http://localhost/control/v1/shutdown", map[string]string{"Origin": "http://localhost", "Sec-Fetch-Mode": "cors"}); code != http.StatusForbidden {
		t.Fatalf("browser reached control: %d", code)
	}
	if status, err := client.Status(t.Context()); err != nil || status.State != "running" {
		t.Fatalf("rejected requests changed lifecycle: %+v %v", status, err)
	}

	replacement := startForeground(t, dir, Settings{Port: 0}, func(Status) (bool, error) { return true, nil })
	select {
	case err := <-instance.done:
		if err != nil {
			t.Fatalf("replaced instance: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replaced instance kept running")
	}
	if status, err := client.Status(t.Context()); err != nil || status.InstanceID != replacement.status.InstanceID {
		t.Fatalf("replacement not served on socket: %+v %v", status, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(SocketPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket survived shutdown: %v", err)
	}
}

func TestHeadlessServesOnlyTheSocket(t *testing.T) {
	dir := runtimeDir(t)
	leaveStaleSocket(t, dir)
	instance := startForeground(t, dir, Settings{Port: -1, Headless: true}, nil)
	if instance.status.ViewerURL != "" || instance.status.Settings.Port != -1 {
		t.Fatalf("headless bound a viewer: %+v", instance.status)
	}
	client := &Client{RuntimeDirectory: dir}
	code, body := request(t, client.HTTPClient(), http.MethodGet, "http://localhost/api/v2/bootstrap", nil)
	var bootstrap struct {
		Mode string `json:"mode"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &bootstrap) != nil || bootstrap.Mode != "local" {
		t.Fatalf("headless socket app: %d %s", code, body)
	}
}

func TestHeadlessRequiresTheSocket(t *testing.T) {
	err := Run(t.Context(), Settings{Port: -1, Headless: true, State: "memory"}, "", nil, StateReset{}, func(Status, *contextservice.Publication) {
		t.Error("headless started without a socket")
	})
	if err == nil || !strings.Contains(err.Error(), "headless") {
		t.Fatalf("headless without socket: %v", err)
	}
}
