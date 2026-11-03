package controlapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// dialing returns a client whose every request reaches server, standing in
// for the local socket transport.
func dialing(server *httptest.Server) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}}
}

func TestBrowserRefusalAndIdempotentShutdown(t *testing.T) {
	var stops atomic.Int32
	server := httptest.NewServer(New(func() (Status, error) {
		return Status{State: "running", ProtocolVersion: ProtocolVersion, InstanceID: "instance"}, nil
	}, func() { stops.Add(1) }))
	defer server.Close()
	for _, scenario := range []struct {
		header, value string
		status        int
	}{
		{"Origin", "http://browser", http.StatusForbidden},
		{"Sec-Fetch-Site", "same-origin", http.StatusForbidden},
		{"Sec-Fetch-Mode", "cors", http.StatusForbidden},
		{"", "", http.StatusOK},
	} {
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/control/v1/status", nil)
		if scenario.header != "" {
			request.Header.Set(scenario.header, scenario.value)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != scenario.status {
			t.Fatalf("%s: status=%d want %d", scenario.header, response.StatusCode, scenario.status)
		}
	}
	client := NewClient(dialing(server))
	for range 2 {
		if err := client.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stops.Load() != 1 {
		t.Fatalf("shutdown called %d times", stops.Load())
	}
	status, err := client.Status(context.Background())
	if err != nil || status.State != "draining" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/control/v1/ingest", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("retired ingestion status=%d", response.StatusCode)
	}
}
