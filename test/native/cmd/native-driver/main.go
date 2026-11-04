// native-driver is a browserless JSON automation client. In acceptance runs it
// also gates the sidecar handshake so the initial native frame is observable.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/flexdinesh/diffx/packages/localprotocol"
	"github.com/flexdinesh/diffx/test/native/driver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if len(os.Args) > 1 && os.Args[1] == localprotocol.Command {
		// Keep descriptor 3 intact: it is the native parent's lifetime lease.
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", os.Getenv("DIFFX_NATIVE_GATE"))
		if err != nil {
			return err
		}
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			conn.Close()
			return err
		}
		var release [1]byte
		_, err = io.ReadFull(conn, release[:])
		conn.Close()
		if err != nil {
			return fmt.Errorf("sidecar gate: %w", err)
		}
		if release[0] != 1 {
			return fmt.Errorf("sidecar gate: invalid release byte")
		}
		path := os.Getenv("DIFFX_NATIVE_REAL_SIDECAR")
		if path == "" || !os.IsPathSeparator(path[0]) {
			return fmt.Errorf("DIFFX_NATIVE_REAL_SIDECAR must be absolute")
		}
		return syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: native-driver SOCKET < requests.jsonl")
	}
	c, err := driver.Dial(ctx, os.Args[1])
	if err != nil {
		return err
	}
	defer c.Close()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		var result json.RawMessage
		if err := c.Call(ctx, request.Method, request.Params, &result); err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return err
		}
	}
	return scanner.Err()
}
