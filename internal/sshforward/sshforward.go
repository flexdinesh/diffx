// Package sshforward reaches a diffx running on another machine through the
// system ssh binary, so ~/.ssh/config, agents and jump hosts apply unchanged.
// It asks the remote diffx for its endpoints, forwards its owner-only socket
// to a local loopback port, and falls back to its loopback viewer port when
// the SSH server refuses socket forwarding. It holds no credentials and runs
// no server of its own: the forwarded port is ssh's.
package sshforward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Target is an SSH destination named as ssh://[user@]host[:port]. The host may
// be an ~/.ssh/config alias.
type Target struct {
	user, host, port string
}

// ParseTarget accepts only ssh:// destinations without paths, passwords,
// queries or fragments, so nothing in the target becomes an ssh option.
func ParseTarget(raw string) (Target, error) {
	invalid := fmt.Errorf("target %q must look like ssh://HOST, ssh://USER@HOST or ssh://USER@HOST:PORT", raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "ssh" || parsed.Hostname() == "" || strings.HasPrefix(parsed.Hostname(), "-") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return Target{}, invalid
	}
	target := Target{host: parsed.Hostname(), port: parsed.Port()}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword || parsed.User.Username() == "" || strings.HasPrefix(parsed.User.Username(), "-") {
			return Target{}, invalid
		}
		target.user = parsed.User.Username()
	}
	if target.port != "" {
		if port, err := strconv.Atoi(target.port); err != nil || port < 1 || port > 65535 {
			return Target{}, invalid
		}
	}
	return target, nil
}

func (target Target) String() string {
	value := url.URL{Scheme: "ssh", Host: target.host}
	if target.port != "" {
		value.Host = net.JoinHostPort(target.host, target.port)
	}
	if target.user != "" {
		value.User = url.User(target.user)
	}
	return value.String()
}

// sshArgs places the destination after "--" so it can never parse as an
// option, followed by the remote command, if any.
func (target Target) sshArgs(options []string, command ...string) []string {
	args := append([]string{}, options...)
	if target.port != "" {
		args = append(args, "-p", target.port)
	}
	destination := target.host
	if target.user != "" {
		destination = target.user + "@" + target.host
	}
	return append(append(args, "--", destination), command...)
}

// Options select the local and remote programs and the local port.
type Options struct {
	// SSH is the ssh binary; empty means "ssh" on PATH.
	SSH string
	// RemoteDiffx is the diffx command on the target; empty means "diffx".
	RemoteDiffx string
	// Port is the local loopback port: -1 picks the first free of 7991-8000,
	// 0 lets the operating system choose.
	Port int
	// Ready bounds each forward's readiness probe; zero means 10 seconds.
	Ready time.Duration
	// Stderr receives ssh's own diagnostics, such as authentication prompts.
	Stderr io.Writer
}

// Transport names what a forward reaches on the target.
const (
	TransportSocket = "unix"
	TransportViewer = "tcp"
)

// Forward is a running ssh port forward to the target's diffx.
type Forward struct {
	// URL is the local loopback address to open in a browser.
	URL string
	// Transport is TransportSocket or TransportViewer.
	Transport string
	command   *exec.Cmd
	// done closes when ssh exits; err is its exit error, set before.
	done chan struct{}
	err  error
}

// Wait blocks until ssh exits or ctx ends. It returns nil when ctx ended and
// the error that ended ssh otherwise.
func (forward *Forward) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case <-forward.done:
		if forward.err == nil {
			return errors.New("ssh forward exited")
		}
		return fmt.Errorf("ssh forward exited: %w", forward.err)
	}
}

// Close stops ssh and waits for it to exit.
func (forward *Forward) Close() error {
	select {
	case <-forward.done:
		return nil
	default:
	}
	_ = forward.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-forward.done:
	case <-time.After(5 * time.Second):
		_ = forward.command.Process.Kill()
		<-forward.done
	}
	return nil
}

// remoteStatus is the subset of the target's `diffx status --json` this
// package reads.
type remoteStatus struct {
	State     string `json:"state"`
	Socket    string `json:"socket"`
	ViewerURL string `json:"viewerUrl"`
}

// Connect forwards the target's running diffx to a local loopback port,
// preferring its socket and falling back to its viewer port.
func Connect(ctx context.Context, target Target, options Options) (*Forward, error) {
	if options.SSH == "" {
		options.SSH = "ssh"
	}
	if options.RemoteDiffx == "" {
		options.RemoteDiffx = "diffx"
	}
	if options.Ready == 0 {
		options.Ready = 10 * time.Second
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	status, err := queryStatus(ctx, target, options)
	if err != nil {
		return nil, err
	}
	var destinations []struct{ transport, address string }
	if status.Socket != "" {
		destinations = append(destinations, struct{ transport, address string }{TransportSocket, status.Socket})
	}
	if status.ViewerURL != "" {
		viewer, err := url.Parse(status.ViewerURL)
		if err != nil || viewer.Port() == "" {
			return nil, fmt.Errorf("diffx on %s reported an invalid viewer URL %q", target, status.ViewerURL)
		}
		// The viewer binds loopback on the target; never forward elsewhere.
		destinations = append(destinations, struct{ transport, address string }{TransportViewer, net.JoinHostPort("127.0.0.1", viewer.Port())})
	}
	if len(destinations) == 0 {
		return nil, fmt.Errorf("diffx on %s reported neither a socket nor a viewer", target)
	}
	var failures []error
	for _, destination := range destinations {
		forward, err := start(ctx, target, options, destination.transport, destination.address)
		if err == nil {
			return forward, nil
		}
		failures = append(failures, fmt.Errorf("%s forward: %w", destination.transport, err))
	}
	if status.ViewerURL == "" {
		failures = append(failures, fmt.Errorf("the SSH server may refuse socket forwarding (AllowStreamLocalForwarding); start diffx on %s without --headless to allow the viewer fallback", target))
	}
	return nil, errors.Join(failures...)
}

func queryStatus(ctx context.Context, target Target, options Options) (remoteStatus, error) {
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, options.SSH, target.sshArgs([]string{"-o", "ClearAllForwardings=yes"}, shellQuote(options.RemoteDiffx)+" status --json")...)
	command.Stdout, command.Stderr = &stdout, io.MultiWriter(&stderr, options.Stderr)
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == 3:
		return remoteStatus{}, fmt.Errorf("diffx is not running on %s; start it there, for example with diffx PATH --headless", target)
	case errors.As(err, &exit) && exit.ExitCode() == 127:
		return remoteStatus{}, fmt.Errorf("%s did not find %q for non-interactive SSH; pass --remote-diffx with its full path", target, options.RemoteDiffx)
	case err != nil:
		return remoteStatus{}, fmt.Errorf("read diffx status on %s: %w: %s", target, err, strings.TrimSpace(stderr.String()))
	}
	var status remoteStatus
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return remoteStatus{}, fmt.Errorf("diffx on %s returned an unreadable status; upgrade it to a version with diffx status --json: %w", target, err)
	}
	if status.State != "running" {
		return remoteStatus{}, fmt.Errorf("diffx on %s is %s", target, status.State)
	}
	return status, nil
}

func start(ctx context.Context, target Target, options Options, transport, address string) (*Forward, error) {
	port, err := localPort(options.Port)
	if err != nil {
		return nil, err
	}
	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	command := exec.CommandContext(ctx, options.SSH, target.sshArgs([]string{"-N", "-o", "ExitOnForwardFailure=yes", "-L", local + ":" + address})...)
	command.Stdout, command.Stderr = io.Discard, options.Stderr
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = 5 * time.Second
	if err := command.Start(); err != nil {
		return nil, err
	}
	forward := &Forward{URL: "http://" + local, Transport: transport, command: command, done: make(chan struct{})}
	go func() {
		forward.err = command.Wait()
		close(forward.done)
	}()
	if err := forward.ready(ctx, options.Ready); err != nil {
		_ = forward.Close()
		return nil, err
	}
	return forward, nil
}

// refusedProbes is how many consecutive accepted-then-dropped probes mean
// the SSH server refused the forward's channel.
const refusedProbes = 5

// ready waits until the forwarded port serves diffx. A refused forward accepts
// locally but drops each connection, so only a real response counts, and
// repeated drops (unlike "connection refused" while ssh starts) end early.
func (forward *Forward) ready(ctx context.Context, limit time.Duration) error {
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	deadline := time.Now().Add(limit)
	dropped := 0
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, forward.URL+"/api/v2/bootstrap", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		switch {
		case err == nil:
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			dropped = 0
		case errors.Is(err, syscall.ECONNREFUSED):
			dropped = 0
		default:
			dropped++
			if dropped >= refusedProbes {
				return errors.New("the SSH server refused the forward")
			}
		}
		select {
		case <-forward.done:
			return fmt.Errorf("ssh exited before the forward served diffx: %v", forward.err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return errors.New("the forwarded port did not serve diffx in time")
		}
	}
}

// localPort picks the local loopback port. Another process may take it before
// ssh binds; ExitOnForwardFailure then makes ssh exit and the forward fail.
func localPort(requested int) (int, error) {
	if requested > 0 {
		return requested, nil
	}
	if requested == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		defer listener.Close()
		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			return 0, errors.New("unexpected local address")
		}
		return address.Port, nil
	}
	for port := 7991; port <= 8000; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}
	return 0, errors.New("no available connect port (7991-8000); choose --port")
}

// shellQuote quotes a word for the remote shell that runs ssh commands.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}
