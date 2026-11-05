package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/connection"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// The test binary is the temporary sidecar. No shell, PATH or server internals.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == localprotocol.ConnectCommand {
		os.Exit(fakeConnect())
	}
	if len(os.Args) > 1 && os.Args[1] == localprotocol.Command {
		os.Exit(fakeSidecar())
	}
	os.Exit(m.Run())
}

func fakeConnect() int {
	if len(os.Args) != 6 || os.Args[2] != "--parent-fd" || os.Args[3] != "3" || os.Args[4] != "--intent-fd" || os.Args[5] != "4" {
		return 31
	}
	lease := os.NewFile(3, "lease")
	defer lease.Close()
	intent := os.NewFile(4, "intent")
	defer intent.Close()
	var request localprotocol.ConnectRequest
	if json.NewDecoder(intent).Decode(&request) != nil || len(request.Args) != 1 || request.Args[0] != "secret invitation" {
		return 32
	}
	directory, err := os.Getwd()
	if err != nil {
		return 33
	}
	ready := localprotocol.Readiness{ProtocolVersion: localprotocol.Version, Connection: connection.Target{Kind: "tcp", URL: "http://127.0.0.1:7981"}, InstanceID: "remote-instance", ScopeID: "remote-scope", Input: localprotocol.Input{Kind: "remote"}, Label: directory}
	if json.NewEncoder(os.Stdout).Encode(ready) != nil {
		return 34
	}
	_, _ = io.Copy(io.Discard, lease)
	return 0
}

func TestConnectSecretsUsePrivateDescriptorAndRemoteLeaseNeverOwnsServer(t *testing.T) {
	options := fakeOptions(t, "connect", "secret invitation")
	options.Directory = t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	s, err := Start(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Ready.Owned || s.Ready.Input.Kind != "remote" || s.Ready.Label != options.Directory || s.Ready.ViewerURL != "" {
		t.Fatalf("remote contract: %+v", s.Ready)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSocketOnlyReadinessAndRemoteOwnershipValidation(t *testing.T) {
	local := localprotocol.Readiness{ProtocolVersion: localprotocol.Version, Connection: connection.Target{Kind: "unix", Socket: "/socket"}, InstanceID: "instance", ScopeID: "scope", Owned: true, Input: localprotocol.Input{Kind: "capture", ContextID: "capture"}}
	if err := validate(local); err != nil {
		t.Fatalf("socket-only readiness refused: %v", err)
	}
	remote := local
	remote.Owned = false
	remote.Input = localprotocol.Input{Kind: "remote"}
	remote.Connection = connection.Target{Kind: "tcp", URL: "http://127.0.0.1:7981"}
	if err := validate(remote); err != nil {
		t.Fatal(err)
	}
	remote.Owned = true
	if err := validate(remote); err == nil {
		t.Fatal("remote ownership admitted")
	}
	remote.Owned = false
	remote.Connection = local.Connection
	if err := validate(remote); err == nil {
		t.Fatal("remote intent admitted private local lifecycle target")
	}
}
func fakeSidecar() int {
	args := os.Args[2:]
	if len(args) < 3 || args[0] != "--parent-fd" || args[1] != "3" {
		return 21
	}
	lease := os.NewFile(3, "lease")
	defer lease.Close()
	if args[2] == "--fail" {
		_, _ = io.WriteString(os.Stderr, "another checkout scope is running")
		return 22
	}
	if args[2] == "--diagnostics" {
		_, _ = io.WriteString(os.Stderr, strings.Repeat("diagnostic ", 2000))
		return 22
	}
	if args[2] == "--hang" {
		_, _ = io.Copy(io.Discard, lease)
		return 0
	}
	if args[2] == "--oversized" {
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 70*1024)+"\n")
		_, _ = io.Copy(io.Discard, lease)
		return 0
	}
	socket := args[2]
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return 23
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "localhost" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"mode":"local","input":"checkout","collect":false,"user":null,"csrfToken":null,"googleEnabled":false}`)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com/escaped", 302) })
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	ready := localprotocol.Readiness{ProtocolVersion: localprotocol.Version, Socket: socket, InstanceID: "instance", ScopeID: "scope", ViewerURL: "http://127.0.0.1:7981", Owned: true, Input: localprotocol.Input{Kind: "checkout", Selection: "checkout", Paths: []string{"/captured"}}}
	if len(args) > 3 && args[3] == "--bad-version" {
		ready.ProtocolVersion++
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		return 24
	}
	_, _ = io.Copy(io.Discard, lease)
	server.Close()
	return 0
}

func fakeOptions(t *testing.T, args ...string) Options {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{SidecarPath: executable, Args: args, Stderr: os.Stderr}
}

func TestSessionOwnsLeaseCancellationAndReaping(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "sidecar.sock")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s, err := Start(ctx, fakeOptions(t, socket))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.Ready.Owned {
		t.Fatal("lost owned readiness")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-s.Done():
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("Close returned before process reaping")
	}
	if _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sidecar socket survived: %v", err)
	}
}

func TestStartupFailuresAreBoundedAndReaped(t *testing.T) {
	for _, arg := range []string{"--fail", "--oversized", "--hang"} {
		t.Run(arg, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			s, err := Start(ctx, fakeOptions(t, arg))
			if err == nil {
				s.Close()
				t.Fatal("expected startup failure")
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Start(ctx, fakeOptions(t, filepath.Join(t.TempDir(), "sidecar.sock"), "--bad-version"))
	if err == nil {
		s.Close()
		t.Fatal("accepted unsupported protocol")
	}
}

func TestStartupFailureIncludesBoundedDiagnostics(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := Start(ctx, fakeOptions(t, "--fail"))
	if err == nil || !strings.Contains(err.Error(), "another checkout scope is running") {
		t.Fatalf("GUI lost sidecar failure: %v", err)
	}
	_, err = Start(ctx, fakeOptions(t, "--diagnostics"))
	if err == nil || !strings.Contains(err.Error(), "diagnostic") || len(err.Error()) > 9000 {
		t.Fatalf("unbounded or missing diagnostics: %v", err)
	}
}

func TestRejectsEveryFlagSpellingForParentLease(t *testing.T) {
	for _, argument := range []string{"--parent-fd", "--parent-fd=4", "-parent-fd", "-parent-fd=4"} {
		t.Run(argument, func(t *testing.T) {
			_, err := Start(t.Context(), Options{SidecarPath: "/must-not-start", Args: []string{argument}})
			if err == nil || !strings.Contains(err.Error(), "reserved desktop-session argument") {
				t.Fatalf("caller could override the owned lifetime descriptor: %v", err)
			}
		})
	}
}

func TestResolveOnlyMatchingInstallation(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "diffx-desktop")
	sibling := filepath.Join(dir, "diffx")
	if err := os.WriteFile(app, []byte("app"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSidecar(app); err == nil {
		t.Fatal("missing bundled sidecar accepted")
	}
	if err := os.WriteFile(sibling, []byte("sidecar"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSidecar(app)
	if err != nil || got != sibling {
		t.Fatalf("resolved %q: %v", got, err)
	}
	for _, ready := range []localprotocol.Readiness{
		{ProtocolVersion: localprotocol.Version, Socket: "relative", ScopeID: "s", InstanceID: "i"},
		{ProtocolVersion: localprotocol.Version, Socket: "/socket", ScopeID: "s", InstanceID: "i", ViewerURL: "http://example.com", Input: localprotocol.Input{Kind: "capture", ContextID: "c"}},
		{ProtocolVersion: localprotocol.Version, Socket: "/socket", ScopeID: "s", InstanceID: "i", ViewerURL: "http://127.0.0.1", Input: localprotocol.Input{Kind: "capture"}},
	} {
		if validate(ready) == nil {
			t.Fatalf("accepted malformed readiness %#v", ready)
		}
	}
}

// The parent acceptance runner builds the matching root binary and injects its
// absolute path. This verifies the real public process/REST boundary without
// importing the root module's private test helpers or application packages.
func TestRealBundledSidecarCaptureAndAttachment(t *testing.T) {
	path := os.Getenv("DIFFX_DESKTOP_SIDECAR")
	if path == "" {
		if os.Getenv("DIFFX_CHECK_RUNNER") == "1" {
			t.Fatal("desktop validation must inject the matching sidecar binary")
		}
		t.Skip("DIFFX_DESKTOP_SIDECAR must name the built matching root binary")
	}
	runtime, err := os.MkdirTemp("", "dxn-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtime)
	t.Setenv("DIFFX_RUNTIME_DIR", runtime)
	input, err := os.CreateTemp(t.TempDir(), "capture-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	patch := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+after\n"
	if _, err = input.WriteString(patch); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	owner, err := Start(ctx, Options{SidecarPath: path, Stdin: input, Args: []string{"--state=memory"}, Stderr: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if !owner.Ready.Owned || owner.Ready.Input.Kind != "capture" || owner.Ready.Input.ContextID == "" || owner.Ready.ViewerURL != "" || owner.Ready.Connection.Kind != "unix" {
		t.Fatalf("capture readiness %#v", owner.Ready)
	}
	attached, err := Start(ctx, Options{SidecarPath: path, Args: []string{"--attach"}, Stderr: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	if attached.Ready.Owned || attached.Ready.InstanceID != owner.Ready.InstanceID {
		t.Fatal("attachment transferred ownership")
	}
	if err = attached.Close(); err != nil {
		t.Fatal(err)
	}
	transport, err := connection.NewTransport(owner.Ready.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/api/v2/contexts/"+owner.Ready.Input.ContextID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("attachment quit stopped owner: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("capture unavailable after attachment quit: %s", response.Status)
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
}
