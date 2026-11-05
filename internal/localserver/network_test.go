package localserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/connection"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

func TestRemoteRealTLSOwnershipPrivateRotationAndShutdown(t *testing.T) {
	runtime := sessionRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	readiness, done := make(chan RemoteReady, 1), make(chan error, 1)
	go func() {
		done <- RunRemote(ctx, Settings{State: filepath.Join(runtime, "state.db")}, sessionInputForTest(t), SessionOptions{}, StateReset{}, NetworkOptions{Listen: "127.0.0.1:0"}, func(value RemoteReady) error { readiness <- value; return nil }, io.Discard)
	}()
	var ready RemoteReady
	select {
	case ready = <-readiness:
	case err := <-done:
		t.Fatalf("remote start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("remote readiness timeout")
	}
	if ready.Status.Socket != SocketPath(runtime) || ready.Status.ViewerURL != "" || ready.Status.InstanceID == "" || len(ready.Invitations) != 1 {
		t.Fatalf("remote readiness: %+v", ready.Status)
	}
	inv, err := connection.Parse(ready.Invitations[0])
	if err != nil {
		t.Fatal(err)
	}
	target, err := connection.Pair(t.Context(), inv)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := connection.NewTransport(target)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	if code, body := request(t, client, http.MethodGet, "http://localhost/api/v2/bootstrap", nil); code != http.StatusOK || !strings.Contains(body, `"mode":"local"`) {
		t.Fatalf("remote review bootstrap: %d %s", code, body)
	}
	for _, path := range []string{"/control/v1/status", "/control/v1/pair", "/mcp", "/api/v2/auth/session", "/api/v2/ingestion-jobs"} {
		if code, _ := request(t, client, http.MethodGet, "http://localhost"+path, nil); code != http.StatusNotFound {
			t.Fatalf("TLS exposed %s: %d", path, code)
		}
	}
	owner := &Client{RuntimeDirectory: runtime}
	rotated, err := owner.Pair(t.Context(), "")
	if err != nil || len(rotated) != 1 {
		t.Fatalf("private rotation: %d %v", len(rotated), err)
	}
	newInv, err := connection.Parse(rotated[0])
	if err != nil || newInv.RunID != inv.RunID || newInv.Pin != inv.Pin || newInv.Secret == inv.Secret {
		t.Fatalf("rotation identity: %v", err)
	}
	if _, err := connection.Pair(t.Context(), newInv); err != nil {
		t.Fatal(err)
	}
	if code, _ := request(t, client, http.MethodGet, "http://localhost/api/v2/bootstrap", nil); code != http.StatusOK {
		t.Fatal("rotation revoked established credential")
	}
	if code, _ := request(t, owner.HTTPClient(), http.MethodPost, "http://localhost/control/v1/pair", map[string]string{"Origin": "http://localhost"}); code != http.StatusForbidden {
		t.Fatal("browser rotated private invitation")
	}
	// A native desktop attachment owns only its lifetime pipe, not this server.
	attachCtx, stopAttach := context.WithCancel(t.Context())
	attached, attachDone := make(chan localprotocol.Readiness, 1), make(chan error, 1)
	go func() {
		attachDone <- RunSession(attachCtx, Settings{}, Input{}, SessionOptions{Attach: true}, StateReset{}, func(value localprotocol.Readiness) error { attached <- value; return nil }, io.Discard)
	}()
	select {
	case value := <-attached:
		if value.Owned || value.Connection.Kind != "unix" || value.ViewerURL != "" {
			t.Fatalf("remote desktop attachment: %+v", value)
		}
	case err := <-attachDone:
		t.Fatalf("attach: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("attach timeout")
	}
	stopAttach()
	if err := awaitSession(t, attachDone); err != nil {
		t.Fatal(err)
	}
	if value, err := owner.Status(t.Context()); err != nil || value.State != "running" {
		t.Fatalf("attachment stopped remote: %v", err)
	}
	cancel()
	if err := awaitSession(t, done); err != nil {
		t.Fatal(err)
	}
	if value, err := owner.Status(t.Context()); err != nil || value.State != "stopped" {
		t.Fatalf("ownership survived remote shutdown: %v", err)
	}
}

func TestRemoteNeverPromotesExistingLocalAndReadinessFailureReleasesOwnership(t *testing.T) {
	runtime := sessionRuntime(t)
	instance := startForeground(t, runtime, Settings{Port: 0}, nil)
	err := RunRemote(t.Context(), Settings{State: "memory"}, Input{}, SessionOptions{}, StateReset{}, NetworkOptions{Listen: "127.0.0.1:0"}, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("implicit promotion: %v", err)
	}
	owner := &Client{RuntimeDirectory: runtime}
	if status, err := owner.Status(t.Context()); err != nil || status.InstanceID != instance.status.InstanceID {
		t.Fatal("remote start changed existing owner")
	}
	if _, err := owner.Pair(t.Context(), ""); err == nil {
		t.Fatal("ordinary local server exposed pairing")
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := awaitSession(t, instance.done); err != nil {
		t.Fatal(err)
	}
	err = RunRemote(t.Context(), Settings{State: "memory"}, Input{}, SessionOptions{}, StateReset{}, NetworkOptions{Listen: "127.0.0.1:0"}, func(RemoteReady) error { return errors.New("readiness failed") }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "readiness failed") {
		t.Fatalf("ready failure: %v", err)
	}
	if status, err := owner.Status(t.Context()); err != nil || status.State != "stopped" {
		t.Fatal("failed readiness retained ownership")
	}
}

func TestSocketOnlyDesktopSessionOwnsAndStopsItsServer(t *testing.T) {
	runtime := sessionRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan localprotocol.Readiness, 1), make(chan error, 1)
	go func() {
		done <- RunSession(ctx, Settings{Headless: true, State: "memory"}, sessionInputForTest(t), SessionOptions{}, StateReset{}, func(value localprotocol.Readiness) error { ready <- value; return nil }, io.Discard)
	}()
	select {
	case value := <-ready:
		if !value.Owned || value.ViewerURL != "" || value.Connection.Kind != "unix" || value.Connection.Socket != SocketPath(runtime) || value.ScopeID != "scope-one" || value.Label == "" {
			t.Fatalf("socket-only readiness: %+v", value)
		}
	case err := <-done:
		t.Fatalf("socket-only startup: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("socket-only readiness timeout")
	}
	cancel()
	if err := awaitSession(t, done); err != nil {
		t.Fatal(err)
	}
	if status, err := (&Client{RuntimeDirectory: runtime}).Status(t.Context()); err != nil || status.State != "stopped" {
		t.Fatal("owned server survived desktop lifetime")
	}
}

func TestRemoteExplicitReplacementReusesListenerPort(t *testing.T) {
	sessionRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan RemoteReady, 1), make(chan error, 1)
	go func() {
		done <- RunRemote(ctx, Settings{State: "memory"}, Input{}, SessionOptions{}, StateReset{}, NetworkOptions{Listen: "127.0.0.1:0"}, func(value RemoteReady) error { ready <- value; return nil }, io.Discard)
	}()
	var value RemoteReady
	select {
	case value = <-ready:
	case err := <-done:
		t.Fatalf("first remote: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("remote readiness timeout")
	}
	inv, err := connection.Parse(value.Invitations[0])
	if err != nil {
		t.Fatal(err)
	}
	err = RunRemote(t.Context(), Settings{State: "memory"}, Input{}, SessionOptions{Replace: true}, StateReset{}, NetworkOptions{Listen: inv.Address}, func(next RemoteReady) error {
		newInv, err := connection.Parse(next.Invitations[0])
		if err != nil {
			t.Fatal(err)
		}
		if newInv.Pin == inv.Pin || newInv.RunID == inv.RunID || next.Status.InstanceID == value.Status.InstanceID {
			t.Error("replacement retained old run identity")
		}
		return errors.New("replacement accepted")
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "replacement accepted") {
		t.Fatalf("same-port replacement: %v", err)
	}
	if err := awaitSession(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisementUsesExplicitBoundSSHThenCandidates(t *testing.T) {
	bound := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 7981}
	got, err := advertisedAddresses(bound, "example.com:443")
	if err != nil || len(got) != 1 || got[0] != "example.com:443" {
		t.Fatalf("explicit advertisement: %v %v", got, err)
	}
	got, err = advertisedAddresses(bound, "")
	if err != nil || got[0] != "127.0.0.1:7981" {
		t.Fatalf("bound advertisement: %v %v", got, err)
	}
	t.Setenv("SSH_CONNECTION", "192.0.2.1 1234 198.51.100.1 22")
	got, err = advertisedAddresses(&net.TCPAddr{IP: net.IPv6unspecified, Port: 7981}, "")
	if err != nil || len(got) != 1 || got[0] != "198.51.100.1:7981" {
		t.Fatalf("SSH advertisement: %v %v", got, err)
	}
	if _, err := listenNetwork(NetworkOptions{Listen: "127.0.0.1:0", Advertise: "user:secret@example.com:443"}); err == nil {
		t.Fatal("accepted credential-bearing advertisement")
	}
}
