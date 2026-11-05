package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flexdinesh/diffx/internal/localserver"
)

func remoteArguments(arguments []string) ([]string, localserver.NetworkOptions, error) {
	network := localserver.NetworkOptions{Listen: ":7981"}
	var out []string
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		if arg == "--" {
			return append(out, arguments[i:]...), network, nil
		}
		name, value, inline := strings.Cut(arg, "=")
		if name != "--listen" && name != "--advertise" {
			out = append(out, arg)
			continue
		}
		if !inline {
			i++
			if i == len(arguments) {
				return nil, network, fmt.Errorf("%s requires a value", name)
			}
			value = arguments[i]
		}
		if value == "" {
			return nil, network, fmt.Errorf("%s requires a value", name)
		}
		if name == "--listen" {
			network.Listen = value
		} else {
			network.Advertise = value
		}
	}
	return out, network, nil
}

func printInvitations(output io.Writer, invitations []string) error {
	for _, invitation := range invitations {
		if _, err := fmt.Fprintln(output, "diffx connect "+invitation); err != nil {
			return err
		}
	}
	return nil
}

func runRemoteCommand(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	if len(arguments) > 0 && arguments[0] == "status" {
		return runRemote(ctx, arguments, stdout)
	}
	if len(arguments) > 0 && arguments[0] == "pair" {
		flags := flag.NewFlagSet("diffx remote pair", flag.ContinueOnError)
		flags.SetOutput(stderr)
		advertise := flags.String("advertise", "", "address included in the invitation, HOST:PORT")
		if err := flags.Parse(arguments[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("remote pair takes no paths")
		}
		client, err := localserver.NewClient()
		if err != nil {
			return err
		}
		invitations, err := client.Pair(ctx, *advertise)
		if err != nil {
			return err
		}
		return printInvitations(stdout, invitations)
	}
	localArgs, network, err := remoteArguments(arguments)
	if err != nil {
		return err
	}
	values, err := parseOptionsMode(localArgs, stderr, true)
	if errors.Is(err, errHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if values.serverSet || values.tokenSet || values.fixture != "" || values.branch != "" || values.print || values.retry || values.browser || values.headless || values.webDirSet || values.portSet {
		return errors.New("remote serving accepts personal checkout options; use --listen for its TLS listener")
	}
	values.diagnosticWriter = stderr
	input, err := acquireLocalInput(ctx, values, stdin)
	if err != nil {
		return err
	}
	if input.Kind == "capture" && values.base != "" {
		return errors.New("base is only supported for a checkout path")
	}
	settings, err := resolvedServerSettings(values)
	if err != nil {
		return err
	}
	selected, err := prepareLocalSession(ctx, &values, input, stderr)
	if err != nil {
		return err
	}
	reset, err := localStateReset(ctx, values, settings, stdin, stderr)
	if err != nil {
		return err
	}
	return withResetHint(localserver.RunRemote(ctx, settings, selected, localserver.SessionOptions{Replace: values.replace}, reset, network, func(ready localserver.RemoteReady) error {
		fmt.Fprintln(stderr, "diffx: personal TLS server running. Invitation expires in ten minutes; Ctrl-C stops the server.")
		return printInvitations(stdout, ready.Invitations)
	}, stderr))
}
