package mcpapi

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Bridge relays MCP JSON-RPC messages unchanged between a newline-delimited
// stdio client (in, out) and the streamable HTTP endpoint reached through
// client, so agents can use a command instead of a URL. It returns nil when
// the stdio client closes its input, and the first failure otherwise.
func Bridge(ctx context.Context, client *http.Client, endpoint string, in io.ReadCloser, out io.WriteCloser) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	local, err := (&mcp.IOTransport{Reader: in, Writer: out}).Connect(ctx)
	if err != nil {
		return err
	}
	defer local.Close()
	// The server is stateless with JSON responses: no session, no standalone
	// stream, and a failed request is reported rather than retried.
	remote, err := (&mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}).Connect(ctx)
	if err != nil {
		return err
	}
	defer remote.Close()
	failures := make(chan error, 2)
	relay := func(from, to mcp.Connection) {
		for {
			message, err := from.Read(ctx)
			if err != nil {
				failures <- err
				return
			}
			if err := to.Write(ctx, message); err != nil {
				failures <- err
				return
			}
		}
	}
	go relay(local, remote)
	go relay(remote, local)
	err = <-failures
	cancel()
	_, _ = local.Close(), remote.Close()
	<-failures
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
