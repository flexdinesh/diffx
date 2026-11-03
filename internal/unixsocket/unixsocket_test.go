//go:build linux || darwin

package unixsocket

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// socketDir stays short: test names would push t.TempDir past sun_path.
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestListenIsPrivateAndRemovedOnClose(t *testing.T) {
	path := filepath.Join(socketDir(t), "s.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Type() != fs.ModeSocket || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v", info.Mode(), err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket file survived close: %v", err)
	}
}

func TestListenReplacesStaleSocketButNotOtherFiles(t *testing.T) {
	dir := socketDir(t)
	path := filepath.Join(dir, "s.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	// A crashed owner leaves its socket file behind.
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := Listen(path)
	if err != nil {
		t.Fatalf("stale socket blocked listen: %v", err)
	}
	_ = listener.Close()

	regular := filepath.Join(dir, "file")
	if err := os.WriteFile(regular, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if listener, err := Listen(regular); err == nil {
		_ = listener.Close()
		t.Fatal("regular file replaced by socket")
	}
	if data, err := os.ReadFile(regular); err != nil || string(data) != "keep" {
		t.Fatalf("regular file changed: %q %v", data, err)
	}
}

func TestListenRejectsOverlongPath(t *testing.T) {
	path := filepath.Join(socketDir(t), strings.Repeat("x", maxPath)+".sock")
	listener, err := Listen(path)
	if err == nil {
		_ = listener.Close()
		t.Fatal("overlong path accepted")
	}
	if !strings.Contains(err.Error(), "DIFFX_RUNTIME_DIR") {
		t.Fatalf("error lacks recovery: %v", err)
	}
}

func TestDialerAndPeerUIDIdentifyThisUser(t *testing.T) {
	path := filepath.Join(socketDir(t), "s.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	peers := make(chan uint32, 1)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(r.Host)) }),
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			uid, err := PeerUID(conn)
			if err != nil {
				t.Errorf("peer uid: %v", err)
			}
			peers <- uid
			return ctx
		},
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: Dialer(path)}, Timeout: 2 * time.Second}
	response, err := client.Get("http://localhost/")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if uid := <-peers; uid != uint32(os.Getuid()) {
		t.Fatalf("peer uid = %d, want %d", uid, os.Getuid())
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		if conn, err := tcp.Accept(); err == nil {
			_ = conn.Close()
		}
	}()
	conn, err := net.Dial("tcp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := PeerUID(conn); err == nil {
		t.Fatal("tcp connection reported peer credentials")
	}
}
