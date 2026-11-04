package controlapi

import (
	"context"
	"net/http/httptest"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifetimeStreamEndsOnServerCancellationAndRejectsReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var reads atomic.Int32
	server := httptest.NewServer(NewWithLifetime(ctx, func() (Status, error) { reads.Add(1); return Status{State: "running", InstanceID: "one"}, nil }, func() { t.Error("lifetime request must not shut down server") }))
	defer server.Close()
	client := NewClient(dialing(server))
	if err := client.WaitInstance(t.Context(), "other"); err == nil {
		t.Fatal("accepted another instance")
	}
	flushed := make(chan struct{})
	waitCtx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotFirstResponseByte: func() { close(flushed) }})
	done := make(chan error, 1)
	go func() { done <- client.WaitInstance(waitCtx, "one") }()
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("lifetime headers not flushed")
	}
	select {
	case err := <-done:
		t.Fatalf("stream ended before cancellation: %v", err)
	default:
	}
	if reads.Load() != 2 {
		t.Fatalf("lifetime should read status only on admission: %d", reads.Load())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lifetime survived server cancellation")
	}
}

func TestLifetimeClientCancellationPreservesServer(t *testing.T) {
	var stopped atomic.Bool
	server := httptest.NewServer(NewWithLifetime(t.Context(), func() (Status, error) { return Status{State: "running", InstanceID: "one"}, nil }, func() { stopped.Store(true) }))
	defer server.Close()
	client := NewClient(dialing(server))
	ctx, cancel := context.WithCancel(t.Context())
	flushed := make(chan struct{})
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotFirstResponseByte: func() { close(flushed) }})
	done := make(chan error, 1)
	go func() { done <- client.WaitInstance(ctx, "one") }()
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("lifetime headers not flushed")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client cancellation stuck")
	}
	if stopped.Load() {
		t.Fatal("canceling attachment stopped server")
	}
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatalf("server unavailable after detach: %v", err)
	}
}
