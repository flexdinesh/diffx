// Package session owns the bundled local sidecar and its parent lifetime lease.
package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/packages/connection"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type Options struct {
	Args        []string
	Stdin       *os.File
	Stderr      io.Writer
	SidecarPath string
	Directory   string
	Environment []string
}

type Session struct {
	Ready    localprotocol.Readiness
	cancel   context.CancelFunc
	finished chan struct{}
	done     chan error
	mu       sync.Mutex
	err      error
}

// ResolveSidecar never consults PATH. The app and sidecar are one installation.
func ResolveSidecar(executable string) (string, error) {
	abs, err := filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(abs)
	for _, path := range []string{filepath.Join(dir, "diffx"), filepath.Join(dir, "..", "Resources", "diffx")} {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return filepath.Clean(path), nil
		}
	}
	return "", errors.New("matching bundled diffx sidecar missing; reinstall the desktop application")
}

// Start consumes exactly one bounded readiness record. Closing a session closes
// the lease first, allowing the CLI to join its owned server before it is reaped.
func Start(ctx context.Context, options Options) (*Session, error) {
	path := options.SidecarPath
	if path == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		path, err = ResolveSidecar(executable)
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("sidecar path must be absolute")
	}
	for _, arg := range options.Args {
		name := strings.TrimLeft(arg, "-")
		if arg == localprotocol.Command || arg == localprotocol.ConnectCommand || name == "parent-fd" || strings.HasPrefix(name, "parent-fd=") || name == "intent-fd" || strings.HasPrefix(name, "intent-fd=") {
			return nil, errors.New("reserved desktop-session argument")
		}
	}
	leaseRead, leaseWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	args := append([]string{localprotocol.Command, "--parent-fd", "3"}, options.Args...)
	extra := []*os.File{leaseRead}
	var intentRead, intentWrite *os.File
	if len(options.Args) > 0 && options.Args[0] == "connect" {
		data, encodeErr := json.Marshal(localprotocol.ConnectRequest{Args: options.Args[1:]})
		if encodeErr != nil || len(data) > 48*1024 {
			leaseRead.Close()
			leaseWrite.Close()
			return nil, errors.New("connection intent too large")
		}
		intentRead, intentWrite, err = os.Pipe()
		if err != nil {
			leaseRead.Close()
			leaseWrite.Close()
			return nil, err
		}
		defer intentRead.Close()
		defer intentWrite.Close()
		args = []string{localprotocol.ConnectCommand, "--parent-fd", "3", "--intent-fd", "4"}
		extra = append(extra, intentRead)
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = options.Directory
	cmd.Env = localprotocol.SessionEnvironment(os.Environ(), options.Environment)
	// Stderr diagnostics use Cmd's copying pipe. A descendant retaining it must
	// not prevent the sidecar from being reaped after its process exits.
	cmd.WaitDelay = 2 * time.Second
	diagnostics := &startupDiagnostics{}
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	cmd.Stdin, cmd.Stderr, cmd.ExtraFiles = options.Stdin, io.MultiWriter(stderr, diagnostics), extra
	stdout, stdoutWrite, err := os.Pipe()
	if err != nil {
		leaseRead.Close()
		leaseWrite.Close()
		return nil, err
	}
	cmd.Stdout = stdoutWrite
	if err := cmd.Start(); err != nil {
		leaseRead.Close()
		leaseWrite.Close()
		stdout.Close()
		stdoutWrite.Close()
		return nil, err
	}
	leaseRead.Close()
	if intentRead != nil {
		intentRead.Close()
		// Bounded native JSON is written concurrently so a cancelled startup can
		// always close the parent lease and reap a child that stopped reading.
		go func() {
			_ = json.NewEncoder(intentWrite).Encode(localprotocol.ConnectRequest{Args: options.Args[1:]})
			intentWrite.Close()
		}()
	}
	stdoutWrite.Close()
	lifetime, cancel := context.WithCancel(ctx)
	s := &Session{cancel: cancel, finished: make(chan struct{}), done: make(chan error, 1)}
	wait := make(chan error, 1)
	// An explicit pipe keeps process reaping independent of a descendant that
	// accidentally inherits stdout. Cmd does not own the read end of this pipe.
	readDone := make(chan struct{})
	read := make(chan struct {
		ready localprotocol.Readiness
		err   error
	}, 1)
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		var ready localprotocol.Readiness
		var readErr error
		if scanner.Scan() {
			readErr = json.Unmarshal(scanner.Bytes(), &ready)
		} else {
			readErr = scanner.Err()
			if readErr == nil {
				readErr = errors.New("sidecar exited before readiness; inspect startup diagnostics")
			}
		}
		if readErr == nil {
			readErr = validate(ready)
		}
		read <- struct {
			ready localprotocol.Readiness
			err   error
		}{ready, readErr}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() { wait <- cmd.Wait() }()
	go func() {
		var exitErr error
		select {
		case exitErr = <-wait:
			if lifetime.Err() == nil && exitErr == nil {
				exitErr = errors.New("sidecar exited unexpectedly")
			}
		case <-lifetime.Done():
			leaseWrite.Close()
			timer := time.NewTimer(8 * time.Second)
			select {
			case exitErr = <-wait:
			case <-timer.C:
				_ = cmd.Process.Kill()
				exitErr = <-wait
				if exitErr == nil {
					exitErr = errors.New("sidecar did not stop after parent lease closed")
				}
			}
			timer.Stop()
		}
		leaseWrite.Close()
		stdout.Close()
		<-readDone
		cancel()
		s.mu.Lock()
		s.err = exitErr
		s.mu.Unlock()
		s.done <- exitErr
		close(s.done)
		close(s.finished)
	}()
	var setup <-chan time.Time
	if len(options.Args) > 0 && options.Args[0] == "connect" {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		setup = timer.C
	}
	select {
	case <-setup:
		_ = s.Close()
		return nil, errors.New("connection setup timed out")
	case result := <-read:
		if result.err != nil {
			_ = s.Close()
			if len(options.Args) > 0 && options.Args[0] == "connect" {
				return nil, errors.New("connection setup failed; retry the connection command")
			}
			return nil, diagnostics.failure(result.err)
		}
		s.Ready = result.ready
	case <-lifetime.Done():
		_ = s.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(options.Args) > 0 && options.Args[0] == "connect" {
			return nil, errors.New("connection setup failed; retry the connection command")
		}
		return nil, diagnostics.failure(errors.New("sidecar exited during startup"))
	}
	return s, nil
}

// Keep GUI startup failures useful even when the app has no terminal. Bound
// diagnostics independently of sidecar output; read only after process reaping.
type startupDiagnostics struct{ data []byte }

func (d *startupDiagnostics) Write(p []byte) (int, error) {
	remaining := 8192 - len(d.data)
	if remaining > 0 {
		d.data = append(d.data, p[:min(remaining, len(p))]...)
	}
	return len(p), nil
}
func (d *startupDiagnostics) failure(err error) error {
	if detail := strings.TrimSpace(string(d.data)); detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

func (s *Session) Done() <-chan error { return s.done }
func (s *Session) Close() error {
	s.cancel()
	<-s.finished
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func validate(r localprotocol.Readiness) error {
	if r.ProtocolVersion != localprotocol.Version {
		return fmt.Errorf("unsupported sidecar protocol %d", r.ProtocolVersion)
	}
	if r.InstanceID == "" || r.ScopeID == "" {
		return errors.New("incomplete sidecar readiness")
	}
	target := r.Connection
	if target.Kind == "" && filepath.IsAbs(r.Socket) {
		target = connection.Target{Kind: "unix", Socket: r.Socket}
	}
	transport, err := connection.NewTransport(target)
	if err != nil {
		return errors.New("invalid sidecar connection")
	}
	transport.CloseIdleConnections()
	if r.ViewerURL != "" {
		u, err := url.Parse(r.ViewerURL)
		if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("invalid sidecar viewer origin")
		}
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("sidecar viewer must be loopback")
		}
	}
	switch r.Input.Kind {
	case "checkout", "checkouts":
		if target.Kind != "unix" {
			return errors.New("local checkout requires Unix connection")
		}
		if r.Input.Selection != r.Input.Kind || len(r.Input.Paths) == 0 {
			return errors.New("incomplete checkout input")
		}
		for _, path := range r.Input.Paths {
			if !filepath.IsAbs(path) {
				return errors.New("checkout path must be absolute")
			}
		}
	case "remote":
		if r.Owned || (target.Kind != "tls" && target.Kind != "tcp") || target.Kind == "tls" && target.Credential == "" {
			return errors.New("invalid remote session ownership")
		}
	case "capture":
		if target.Kind != "unix" {
			return errors.New("local capture requires Unix connection")
		}
		if r.Input.ContextID == "" {
			return errors.New("capture context missing")
		}
	default:
		return errors.New("unknown sidecar input kind")
	}
	return nil
}
