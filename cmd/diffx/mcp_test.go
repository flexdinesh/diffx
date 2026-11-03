package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/mcpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPRefusesWithoutARunningInstance(t *testing.T) {
	shortRuntime(t)
	in, _ := io.Pipe()
	_, out := io.Pipe()
	if err := runMCP(t.Context(), nil, in, out, io.Discard); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("mcp without instance: %v", err)
	}
}

// An agent's stdio client reaches a headless instance's tools over its socket.
func TestMCPBridgesStdioToTheLocalSocket(t *testing.T) {
	shortRuntime(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan localserver.Status, 1)
	stopped := make(chan error, 1)
	go func() {
		stopped <- localserver.RunForeground(ctx, localserver.Settings{Port: -1, Headless: true, State: "memory"}, localserver.Input{}, nil, localserver.StateReset{},
			func(status localserver.Status, _ localserver.Published) { ready <- status })
	}()
	select {
	case <-ready:
	case err := <-stopped:
		t.Fatalf("startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	toBridge, clientWriter := io.Pipe()
	clientReader, fromBridge := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- runMCP(context.Background(), nil, toBridge, fromBridge, io.Discard) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(t.Context(), &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, &mcp.ClientSessionOptions{ProtocolVersion: mcpapi.ProtocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) == 0 {
		t.Fatalf("tools over socket: %+v %v", tools, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge outlived its client")
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}
