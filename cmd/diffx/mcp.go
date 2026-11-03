package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/mcpapi"
)

// mcpEndpoint addresses the running instance's MCP route over its socket.
const mcpEndpoint = "http://localhost/mcp"

// runMCP bridges an agent's stdio MCP client to the running local instance,
// so agent configuration names a command instead of a changing port. Over SSH,
// `ssh HOST diffx mcp` reaches that host's instance the same way.
func runMCP(ctx context.Context, arguments []string, stdin io.ReadCloser, stdout io.WriteCloser, stderr io.Writer) error {
	flags := flag.NewFlagSet("diffx mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "  Usage: diffx mcp")
		fmt.Fprintln(stderr, "  Serves the running local diffx's MCP tools over stdio.")
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return errors.New("mcp takes no arguments")
	}
	client, err := localserver.NewClient()
	if err != nil {
		return err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if status.State != "running" {
		return errors.New("diffx is not running; start it with diffx [PATH] or diffx [PATH] --headless")
	}
	return mcpapi.Bridge(ctx, client.HTTPClient(), mcpEndpoint, stdin, stdout)
}

// stdoutCloser keeps the process's stdout open when the bridge closes its side.
type stdoutCloser struct{ io.Writer }

func (stdoutCloser) Close() error { return nil }

func runMCPProcess(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	if stdin == nil {
		return errors.New("mcp needs stdin")
	}
	return runMCP(ctx, arguments, stdin, stdoutCloser{stdout}, stderr)
}
