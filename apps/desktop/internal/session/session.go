// Package session owns the bundled local sidecar and its Unix-socket transport.
package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

const Authority = "http://localhost"

type Options struct {
	Args        []string
	Stdin       *os.File
	Stderr      io.Writer
	SidecarPath string
}

type Session struct {
	Ready    localprotocol.Readiness
	Client   *goclient.ClientWithResponses
	HTTP     *http.Client
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
	return "", errors.New("matching bundled diffx sidecar missing; reinstall the native application")
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
		if arg == localprotocol.Command || name == "parent-fd" || strings.HasPrefix(name, "parent-fd=") {
			return nil, errors.New("reserved desktop-session argument")
		}
	}
	leaseRead, leaseWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	args := append([]string{localprotocol.Command, "--parent-fd", "3"}, options.Args...)
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stderr, cmd.ExtraFiles = options.Stdin, options.Stderr, []*os.File{leaseRead}
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
	select {
	case result := <-read:
		if result.err != nil {
			_ = s.Close()
			return nil, result.err
		}
		s.Ready = result.ready
	case <-lifetime.Done():
		_ = s.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("sidecar exited during startup")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(request context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(request, "unix", s.Ready.Socket)
		},
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 90 * time.Second,
	}
	s.HTTP = &http.Client{Transport: socketTransport{base: transport, lifetime: lifetime}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("sidecar redirects refused") }}
	s.Client, err = goclient.NewClientWithResponses(Authority, goclient.WithHTTPClient(s.HTTP))
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	go func() { <-lifetime.Done(); transport.CloseIdleConnections() }()
	return s, nil
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
	if !filepath.IsAbs(r.Socket) || r.InstanceID == "" || r.ScopeID == "" {
		return errors.New("incomplete sidecar readiness")
	}
	u, err := url.Parse(r.ViewerURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid sidecar viewer origin")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("sidecar viewer must be loopback")
	}
	switch r.Input.Kind {
	case "checkout", "checkouts":
		if r.Input.Selection != r.Input.Kind || len(r.Input.Paths) == 0 {
			return errors.New("incomplete checkout input")
		}
		for _, path := range r.Input.Paths {
			if !filepath.IsAbs(path) {
				return errors.New("checkout path must be absolute")
			}
		}
	case "capture":
		if r.Input.ContextID == "" {
			return errors.New("capture context missing")
		}
	default:
		return errors.New("unknown sidecar input kind")
	}
	return nil
}

type socketTransport struct {
	base     *http.Transport
	lifetime context.Context
}

func (t socketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "http" || req.URL.Host != "localhost" || req.URL.User != nil || (req.Host != "" && req.Host != "localhost") {
		return nil, errors.New("request outside sidecar authority refused")
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.lifetime, cancel)
	response, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: func() { stop(); cancel() }}
	return response, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel func()
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.cancel(); return err }
