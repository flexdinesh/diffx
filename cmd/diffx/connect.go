package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flexdinesh/diffx/internal/browser"
	"github.com/flexdinesh/diffx/internal/clientgateway"
	"github.com/flexdinesh/diffx/internal/desktoplaunch"
	"github.com/flexdinesh/diffx/internal/sshforward"
	"github.com/flexdinesh/diffx/internal/webui"
	"github.com/flexdinesh/diffx/packages/connection"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// connectReady retains the public SSH --json fields. Secrets are never emitted.
type connectReady struct {
	URL       string `json:"url"`
	Target    string `json:"target"`
	Transport string `json:"transport"`
}

type connectOptions struct {
	target                     string
	port                       int
	noBrowser, browser, asJSON bool
	remoteDiffx                string
}

func parseConnect(arguments []string, stderr io.Writer) (connectOptions, error) {
	values := connectOptions{}
	flags := flag.NewFlagSet("diffx connect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.IntVar(&values.port, "port", -1, "local loopback port, 0 selects a free port")
	flags.BoolVar(&values.noBrowser, "no-browser", false, "serve a foreground browser connection without opening it")
	flags.BoolVar(&values.browser, "browser", false, "open a foreground browser connection instead of desktop")
	flags.BoolVar(&values.asJSON, "json", false, "serve a foreground connection and print its ready URL as JSON")
	flags.StringVar(&values.remoteDiffx, "remote-diffx", "diffx", "diffx command on the remote SSH machine")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: diffx connect STRING|ssh://[USER@]HOST[:PORT] [--browser|--no-browser|--json]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(normalizeConnectArguments(arguments)); err != nil {
		return values, err
	}
	if flags.NArg() != 1 {
		return values, errors.New("connect takes one target, a connection invitation or ssh://HOST")
	}
	if values.port < -1 || values.port > 65535 {
		return values, errors.New("port must be between 0 and 65535")
	}
	if values.browser && values.noBrowser {
		return values, errors.New("--browser conflicts with --no-browser")
	}
	values.target = flags.Arg(0)
	if strings.HasPrefix(values.target, "ssh://") {
		if _, err := sshforward.ParseTarget(values.target); err != nil {
			return values, err
		}
	} else if _, err := connection.Parse(values.target); err != nil {
		return values, errors.New("target must be a connection invitation or ssh://HOST")
	}
	return values, nil
}

type activeConnection struct {
	target           connection.Target
	label, transport string
	forward          *sshforward.Forward
}

func openConnection(ctx context.Context, values connectOptions, stderr io.Writer) (*activeConnection, error) {
	if strings.HasPrefix(values.target, "ssh://") {
		target, err := sshforward.ParseTarget(values.target)
		if err != nil {
			return nil, err
		}
		forward, err := sshforward.Connect(ctx, target, sshforward.Options{RemoteDiffx: values.remoteDiffx, Port: values.port, Stderr: stderr})
		if err != nil {
			return nil, err
		}
		return &activeConnection{target: connection.Target{Kind: "tcp", URL: forward.URL}, label: target.String(), transport: forward.Transport, forward: forward}, nil
	}
	inv, err := connection.Parse(values.target)
	if err != nil {
		return nil, err
	}
	target, err := pairedTarget(ctx, inv)
	if err != nil {
		return nil, err
	}
	return &activeConnection{target: target, label: inv.Address, transport: "tls"}, nil
}

func (active *activeConnection) Close() {
	if active.forward != nil {
		_ = active.forward.Close()
	}
}

func (active *activeConnection) Wait(ctx context.Context) error {
	if active.forward != nil {
		return active.forward.Wait(ctx)
	}
	<-ctx.Done()
	return nil
}

func runConnect(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	values, err := parseConnect(arguments, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if !values.browser && !values.noBrowser && !values.asJSON {
		accepted, launchErr := desktoplaunch.Open(ctx, append([]string{"connect"}, arguments...), nil, stderr)
		if accepted {
			return nil
		}
		fmt.Fprintln(stderr, "diffx: desktop unavailable; opening browser.")
		if launchErr != nil {
			fmt.Fprintln(stderr, "diffx:", launchErr)
		}
	}
	upstream := values
	// Keep the requested public port on the filtered browser gateway.
	// The raw SSH forward remains a separate ephemeral native endpoint.
	if strings.HasPrefix(values.target, "ssh://") {
		upstream.port = 0
	}
	active, err := openConnection(ctx, upstream, stderr)
	if err != nil {
		return err
	}
	defer active.Close()
	port := values.port
	gateway, err := clientgateway.Start(webui.Assets(), active.target, port)
	if err != nil {
		return err
	}
	defer gateway.Close()
	if values.asJSON {
		if err := json.NewEncoder(stdout).Encode(connectReady{URL: gateway.URL, Target: active.label, Transport: active.transport}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "  connected to: %s\n  url: %s\n  ctrl-c to disconnect.\n", active.label, gateway.URL)
	}
	if !values.noBrowser {
		if err := browser.Open(gateway.URL); err != nil {
			fmt.Fprintln(stderr, "diffx:", err)
		}
	}
	return active.Wait(ctx)
}

func runConnectionSession(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet(localprotocol.ConnectCommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	parentFD := flags.Int("parent-fd", -1, "inherited parent pipe")
	intentFD := flags.Int("intent-fd", -1, "inherited native connection request")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *parentFD < 3 || *intentFD < 3 || *parentFD == *intentFD {
		return errors.New("connection session requires distinct inherited parent and intent pipes")
	}
	ctx, closeLifetime, err := parentLifetime(ctx, *parentFD)
	if err != nil {
		return err
	}
	defer closeLifetime()
	intent := os.NewFile(uintptr(*intentFD), "connection-intent")
	if intent == nil {
		return errors.New("connection intent missing")
	}
	defer intent.Close()
	stop := context.AfterFunc(ctx, func() { _ = intent.Close() })
	defer stop()
	var request localprotocol.ConnectRequest
	decoder := json.NewDecoder(io.LimitReader(intent, 64*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(struct{})) != io.EOF {
		return errors.New("invalid native connection request")
	}
	values, err := parseConnect(request.Args, io.Discard)
	if err != nil {
		return errors.New("invalid native connection target")
	}
	active, err := openConnection(ctx, values, stderr)
	if err != nil {
		return sessionResult(ctx, err)
	}
	defer active.Close()
	identity := active.target.RunID
	if identity == "" {
		identity = active.label
	}
	ready := localprotocol.Readiness{ProtocolVersion: localprotocol.Version, Connection: active.target, Label: active.label, InstanceID: identity, ScopeID: identity, Input: localprotocol.Input{Kind: "remote"}}
	if err := json.NewEncoder(stdout).Encode(ready); err != nil {
		return errors.New("write connection readiness failed")
	}
	return active.Wait(ctx)
}

// Flags may follow the target; invitation data is never evaluated as shell text.
func normalizeConnectArguments(arguments []string) []string {
	values := map[string]bool{"--port": true, "-port": true, "--remote-diffx": true, "-remote-diffx": true}
	flags, positionals := []string{}, []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			positionals = append(positionals, arguments[index+1:]...)
			break
		}
		if len(argument) > 1 && argument[0] == '-' {
			flags = append(flags, argument)
			if values[argument] && index+1 < len(arguments) {
				index++
				flags = append(flags, arguments[index])
			}
			continue
		}
		positionals = append(positionals, argument)
	}
	return append(append(flags, "--"), positionals...)
}
