package sshforward

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as a fake ssh: with FAKE_SSH_LOG set it records its
// arguments, answers the remote status command from FAKE_SSH_STATUS and
// FAKE_SSH_EXIT, and serves -L forwards itself. FAKE_SSH_REFUSE_UNIX makes
// socket forwards accept and drop connections, like a refusing sshd.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_SSH_LOG") != "" {
		os.Exit(fakeSSH(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSSH(args []string) int {
	log, err := os.OpenFile(os.Getenv("FAKE_SSH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 255
	}
	fmt.Fprintln(log, strings.Join(args, "\x1f"))
	_ = log.Close()
	forward := ""
	for index, arg := range args {
		if arg == "-L" && index+1 < len(args) {
			forward = args[index+1]
		}
	}
	if forward == "" {
		fmt.Print(os.Getenv("FAKE_SSH_STATUS"))
		var code int
		_, _ = fmt.Sscan(os.Getenv("FAKE_SSH_EXIT"), &code)
		return code
	}
	// 127.0.0.1:PORT:/path or 127.0.0.1:PORT:host:port
	parts := strings.SplitN(forward, ":", 3)
	listener, err := net.Listen("tcp", parts[0]+":"+parts[1])
	if err != nil {
		return 255 // ExitOnForwardFailure
	}
	network := "tcp"
	if strings.HasPrefix(parts[2], "/") {
		network = "unix"
	}
	refuse := network == "unix" && os.Getenv("FAKE_SSH_REFUSE_UNIX") != ""
	if os.Getenv("FAKE_SSH_DIE") != "" {
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(7) }()
	}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM)
		<-stop
		os.Exit(0)
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return 255
		}
		if refuse {
			_ = conn.Close()
			continue
		}
		go func() {
			defer conn.Close()
			upstream, err := net.Dial(network, parts[2])
			if err != nil {
				return
			}
			defer upstream.Close()
			go func() { _, _ = io.Copy(upstream, conn) }()
			_, _ = io.Copy(conn, upstream)
		}()
	}
}

type fakeRemote struct {
	log    string
	socket string
	viewer string
}

// newFakeRemote serves diffx's bootstrap on a unix socket and a TCP viewer,
// and points the fake ssh at them.
func newFakeRemote(t *testing.T, status map[string]any, exit int) fakeRemote {
	t.Helper()
	dir, err := os.MkdirTemp("", "dx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/bootstrap" {
			_, _ = w.Write([]byte(`{"mode":"local"}`))
			return
		}
		http.NotFound(w, r)
	})
	remote := fakeRemote{log: filepath.Join(dir, "ssh.log"), socket: filepath.Join(dir, "diffx.sock")}
	unixListener, err := net.Listen("unix", remote.socket)
	if err != nil {
		t.Fatal(err)
	}
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote.viewer = "http://" + tcpListener.Addr().String()
	for _, listener := range []net.Listener{unixListener, tcpListener} {
		server := &http.Server{Handler: handler}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() { _ = server.Close() })
	}
	if status != nil {
		if _, ok := status["socket"]; ok {
			status["socket"] = remote.socket
		}
		if _, ok := status["viewerUrl"]; ok {
			status["viewerUrl"] = remote.viewer
		}
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SSH_LOG", remote.log)
	t.Setenv("FAKE_SSH_STATUS", string(raw))
	t.Setenv("FAKE_SSH_EXIT", fmt.Sprint(exit))
	return remote
}

func (remote fakeRemote) calls(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(remote.log)
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		calls = append(calls, strings.Split(line, "\x1f"))
	}
	return calls
}

func connectOptions(t *testing.T) Options {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{SSH: executable, Port: 0, Ready: 2 * time.Second}
}

func TestParseTargetAcceptsOnlySSHDestinations(t *testing.T) {
	for raw, want := range map[string][]string{
		"ssh://devbox":                {"--", "devbox"},
		"ssh://dee@devbox":            {"--", "dee@devbox"},
		"ssh://dee@devbox.local:2222": {"-p", "2222", "--", "dee@devbox.local"},
		"ssh://[::1]:22/":             {"-p", "22", "--", "::1"},
	} {
		target, err := ParseTarget(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got := target.sshArgs(nil); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%s: args %v, want %v", raw, got, want)
		}
	}
	for _, raw := range []string{"devbox", "http://devbox", "ssh://", "ssh://-oProxyCommand=x", "ssh://-x@devbox", "ssh://a:b@devbox", "ssh://devbox/path", "ssh://devbox?x=1", "ssh://devbox#f", "ssh://devbox:0", "ssh://devbox:99999"} {
		if _, err := ParseTarget(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestConnectForwardsTheRemoteSocket(t *testing.T) {
	remote := newFakeRemote(t, map[string]any{"state": "running", "socket": "", "viewerUrl": ""}, 0)
	target, err := ParseTarget("ssh://dee@devbox:2222")
	if err != nil {
		t.Fatal(err)
	}
	options := connectOptions(t)
	options.RemoteDiffx = "/opt/my diffx/diffx"
	forward, err := Connect(t.Context(), target, options)
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	if forward.Transport != TransportSocket || !strings.HasPrefix(forward.URL, "http://127.0.0.1:") {
		t.Fatalf("forward: %+v", forward)
	}
	response, err := http.Get(forward.URL + "/api/v2/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	calls := remote.calls(t)
	if len(calls) != 2 {
		t.Fatalf("ssh calls: %q", calls)
	}
	wantStatus := []string{"-o", "ClearAllForwardings=yes", "-p", "2222", "--", "dee@devbox", `'/opt/my diffx/diffx' status --json`}
	if strings.Join(calls[0], " ") != strings.Join(wantStatus, " ") {
		t.Fatalf("status call %q, want %q", calls[0], wantStatus)
	}
	local := strings.TrimPrefix(forward.URL, "http://")
	wantForward := []string{"-N", "-o", "ExitOnForwardFailure=yes", "-L", local + ":" + remote.socket, "-p", "2222", "--", "dee@devbox"}
	if strings.Join(calls[1], " ") != strings.Join(wantForward, " ") {
		t.Fatalf("forward call %q, want %q", calls[1], wantForward)
	}
}

func TestConnectFallsBackToTheViewerWhenSocketForwardingIsRefused(t *testing.T) {
	remote := newFakeRemote(t, map[string]any{"state": "running", "socket": "", "viewerUrl": ""}, 0)
	t.Setenv("FAKE_SSH_REFUSE_UNIX", "1")
	target, _ := ParseTarget("ssh://devbox")
	forward, err := Connect(t.Context(), target, connectOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	calls := remote.calls(t)
	viewerPort := remote.viewer[strings.LastIndex(remote.viewer, ":")+1:]
	if forward.Transport != TransportViewer || len(calls) != 3 || !strings.HasSuffix(calls[2][4], ":127.0.0.1:"+viewerPort) {
		t.Fatalf("fallback: %+v %q", forward, calls)
	}
}

func TestConnectExplainsRemoteFailures(t *testing.T) {
	target, _ := ParseTarget("ssh://devbox")
	for _, scenario := range []struct {
		name   string
		status map[string]any
		exit   int
		refuse bool
		want   string
	}{
		{"not running", map[string]any{"state": "stopped", "viewerUrl": ""}, 3, false, "not running on ssh://devbox"},
		{"missing diffx", nil, 127, false, "pass --remote-diffx"},
		{"headless refused", map[string]any{"state": "running", "socket": ""}, 0, true, "without --headless"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			newFakeRemote(t, scenario.status, scenario.exit)
			if scenario.refuse {
				t.Setenv("FAKE_SSH_REFUSE_UNIX", "1")
			}
			options := connectOptions(t)
			options.Ready = 500 * time.Millisecond
			forward, err := Connect(t.Context(), target, options)
			if err == nil {
				_ = forward.Close()
				t.Fatal("connected")
			}
			if !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("error %q lacks %q", err, scenario.want)
			}
		})
	}
}

func TestWaitReportsAnSSHExitAndNotCancellation(t *testing.T) {
	newFakeRemote(t, map[string]any{"state": "running", "socket": ""}, 0)
	target, _ := ParseTarget("ssh://devbox")
	forward, err := Connect(t.Context(), target, connectOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := forward.Wait(ctx); err != nil {
		t.Fatalf("cancelled wait: %v", err)
	}
	_ = forward.Close()

	t.Setenv("FAKE_SSH_DIE", "1")
	forward, err = Connect(t.Context(), target, connectOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	waited := make(chan error, 1)
	go func() { waited <- forward.Wait(t.Context()) }()
	select {
	case err := <-waited:
		if err == nil || !strings.Contains(err.Error(), "ssh forward exited") {
			t.Fatalf("wait after ssh died: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait missed the ssh exit")
	}
}
