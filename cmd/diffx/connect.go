package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/flexdinesh/diffx/internal/browser"
	"github.com/flexdinesh/diffx/internal/sshforward"
)

// connectReady is the one-line `diffx connect --json` contract a desktop shell
// reads once the forward serves diffx. Fields are only added.
type connectReady struct {
	URL       string `json:"url"`
	Target    string `json:"target"`
	Transport string `json:"transport"`
}

// runConnect forwards a diffx running on another machine to a local loopback
// port over the system ssh, then stays in the foreground until Ctrl-C or ssh
// exits.
func runConnect(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("diffx connect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", -1, "local loopback port; defaults to the first available from 7991 to 8000, 0 lets the OS choose")
	noBrowser := flags.Bool("no-browser", false, "do not open a browser")
	asJSON := flags.Bool("json", false, "print one JSON line with the URL once ready")
	remoteDiffx := flags.String("remote-diffx", "diffx", "diffx command on the remote machine")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "  Usage: diffx connect ssh://[USER@]HOST[:PORT] [--port N] [--no-browser] [--json] [--remote-diffx PATH]")
		fmt.Fprintln(stderr, "  Forwards the remote diffx socket, or its viewer port, over ssh.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(normalizeConnectArguments(arguments)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("connect takes one target, like ssh://devbox")
	}
	if *port < -1 || *port > 65535 {
		return errors.New("port must be between 0 and 65535")
	}
	target, err := sshforward.ParseTarget(flags.Arg(0))
	if err != nil {
		return err
	}
	forward, err := sshforward.Connect(ctx, target, sshforward.Options{RemoteDiffx: *remoteDiffx, Port: *port, Stderr: stderr})
	if err != nil {
		return err
	}
	defer forward.Close()
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(connectReady{URL: forward.URL, Target: target.String(), Transport: forward.Transport}); err != nil {
			return err
		}
	} else {
		color := terminalColors(stdout)
		fmt.Fprintf(stdout, "  %s\n", color.paint(color.bold+color.cyan, "diffx "+displayVersion()))
		fmt.Fprintf(stdout, "  %s       %s\n", color.paint(color.dim, "connected to:"), target)
		fmt.Fprintf(stdout, "  %s         %s\n", color.paint(color.dim, "forwarding:"), forward.Transport)
		fmt.Fprintf(stdout, "  %s                %s\n", color.paint(color.dim, "url:"), color.paint(color.green, forward.URL))
		fmt.Fprintf(stdout, "  %s\n", color.paint(color.dim, "ctrl-c to stop."))
	}
	if !*noBrowser {
		if err := browser.Open(forward.URL); err != nil {
			fmt.Fprintf(stderr, "  diffx: %v\n", err)
		}
	}
	return forward.Wait(ctx)
}

// normalizeConnectArguments lets flags follow the target.
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
