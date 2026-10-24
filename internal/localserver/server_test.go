package localserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	osuser "os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

const serverPatch = "diff --git a/value.txt b/value.txt\n--- a/value.txt\n+++ b/value.txt\n@@ -1 +1 @@\n-old\n+new\n"

func TestCheckoutServerServesBeforeCollectionAndWaitsForCollectorShutdown(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.db")
	store, err := reviewstore.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	processUser, err := osuser.Current()
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.User(processUser.Uid, processUser.Username)
	if err != nil {
		t.Fatal(err)
	}
	input, err := collector.CollectPatch(t.Context(), serverPatch, t.TempDir(), collector.Options{SourceID: "prior-process", SubmissionID: "prior-capture"})
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.New(store, user)
	if _, err := service.Ingest(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(service.Close(), store.Close()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan Status, 1)
	collectionCanceled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		done <- runServer(ctx, cancel, Settings{Host: "127.0.0.1", Port: 0, State: state}, "", "async-test", Collection{},
			func(ctx context.Context, _ func(ingestion.Request) error) ([]CollectionFailure, error) {
				<-ctx.Done()
				close(collectionCanceled)
				<-release
				return nil, ctx.Err()
			}, StateReset{}, func(status Status, published Published) {
				if len(published.Publications) != 0 {
					t.Error("ready must not wait for checkout collection")
				}
				ready <- status
			}, log.New(io.Discard, "", 0))
	}()
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	var status Status
	select {
	case status = <-ready:
	case err := <-done:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server readiness blocked on checkout collection")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, expected := range []struct{ path, body string }{
		{"/", "<html"},
		{"/api/v2/bootstrap", `"collect":true`},
		{"/api/v2/bootstrap", `"input":"checkouts"`},
		{"/api/v2/contexts", `"contexts":[]`},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, status.BrowserURL+expected.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s unavailable during collection: %v", expected.path, err)
		}
		raw, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(raw), expected.body) {
			t.Fatalf("%s during collection: status %d, body %s, error %v", expected.path, response.StatusCode, raw, err)
		}
	}
	cancel()
	select {
	case <-collectionCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel startup collection")
	}
	select {
	case err := <-done:
		t.Fatalf("server returned before collector stopped: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not finish after collector stopped")
	}
}

func TestRunForcesCloseWhenIdleConnectionExhaustsShutdownGrace(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan Status, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Settings{Host: "127.0.0.1", Port: 0, State: "memory"}, "", nil, StateReset{},
			func(status Status, _ *contextservice.Publication) { ready <- status })
	}()
	var status Status
	select {
	case status = <-ready:
	case err := <-done:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	address, err := url.Parse(status.BrowserURL)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", address.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	// An incomplete request remains active beyond the shutdown grace period.
	if _, err := connection.Write([]byte("GET / HTTP/1.1\r\nHost: ")); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("forced shutdown should succeed: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("shutdown exceeded grace and cleanup budget")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, connection); err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			t.Fatal("connection survived service shutdown")
		}
	}
}

func TestDefaultPortExhaustionLeavesListenersRunning(t *testing.T) {
	var listeners []net.Listener
	for port := 7981; port <= 7990; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			listeners = append(listeners, listener)
			t.Cleanup(func() { _ = listener.Close() })
		}
	}
	listener, err := listenWeb(Settings{Host: "127.0.0.1", Port: -1})
	if err == nil {
		_ = listener.Close()
		t.Fatal("all default ports should be occupied")
	}
	for _, listener := range listeners {
		connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf("listener affected: %v", err)
		}
		_ = connection.Close()
	}
}
