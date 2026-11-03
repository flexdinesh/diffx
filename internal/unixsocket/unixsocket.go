//go:build linux || darwin

// Package unixsocket binds, dials and identifies peers of private Unix domain
// sockets. It is mechanism only: callers own ownership locks and decide which
// peers to admit.
package unixsocket

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
)

// Listen binds path with owner-only permissions, replacing a stale socket file
// left by a process that exited without cleanup. The caller must hold the
// exclusive ownership of path (a process lock) so a live owner's socket is
// never replaced. Closing the listener removes the socket file.
func Listen(path string) (net.Listener, error) {
	if len(path) > maxPath {
		return nil, fmt.Errorf("socket path %s is %d bytes; the limit is %d. Set DIFFX_RUNTIME_DIR to a shorter directory", path, len(path), maxPath)
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode().Type() != fs.ModeSocket:
		return nil, fmt.Errorf("refusing to replace %s: not a socket", path)
	case err == nil:
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// The runtime directory is already private; the mode also protects the
	// socket if it is ever exposed through a shared directory.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

// Dialer returns a dial function for HTTP transports that connects every
// request to path, whatever the request's host.
func Dialer(path string) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", path)
	}
}

// PeerUID reports the effective user ID of the process connected to conn.
func PeerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("peer credentials require a unix socket connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var credentialErr error
	if err := raw.Control(func(fd uintptr) { uid, credentialErr = peerUID(int(fd)) }); err != nil {
		return 0, err
	}
	return uid, credentialErr
}
