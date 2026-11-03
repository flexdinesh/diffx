package localserver

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// runtimeDir stays short: test names would push t.TempDir past sun_path.
func runtimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path, err := prepareRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func testClient(t *testing.T) *Client {
	t.Helper()
	client := &Client{RuntimeDirectory: runtimeDir(t)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Stop(ctx); err != nil {
			t.Errorf("cleanup stop: %v", err)
		}
	})
	return client
}

// leaveStaleSocket simulates an owner that crashed without removing its socket.
func leaveStaleSocket(t *testing.T, dir string) {
	t.Helper()
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: SocketPath(dir), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStaleSocketReportsStopped(t *testing.T) {
	client := testClient(t)
	leaveStaleSocket(t, client.RuntimeDirectory)
	status, err := client.Status(context.Background())
	if err != nil || status.State != "stopped" {
		t.Fatalf("status = %+v %v", status, err)
	}
	if _, err := os.Lstat(SocketPath(client.RuntimeDirectory)); err != nil {
		t.Fatal("status removed the stale socket without ownership")
	}
}
