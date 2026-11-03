package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/flexdinesh/diffx/internal/localserver"
)

// exitStopped is the status exit code when no local instance runs, so
// scripts and `diffx connect` can tell "not running" from a failed probe.
const exitStopped exitStatus = 3

// statusReport is the versioned `diffx status --json` contract read by
// `diffx connect` and future clients over SSH. Fields are only added.
type statusReport struct {
	State           string `json:"state"`
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
	InstanceID      string `json:"instanceId,omitempty"`
	PID             int    `json:"pid,omitempty"`
	Version         string `json:"version,omitempty"`
	Socket          string `json:"socket,omitempty"`
	// ViewerURL is empty when the instance is headless.
	ViewerURL    string `json:"viewerUrl"`
	Observations int    `json:"observations"`
}

func runStatus(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("diffx status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the status as one JSON object")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "  Usage: diffx status [--json]")
		fmt.Fprintln(stderr, "  Exits 3 when no local diffx is running.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return errors.New("status takes no arguments")
	}
	client, err := localserver.NewClient()
	if err != nil {
		return err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if status.State == "stopped" {
		if *asJSON {
			if err := json.NewEncoder(stdout).Encode(statusReport{State: status.State}); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(stdout, "  diffx is not running")
		}
		return exitStopped
	}
	report := statusReport{
		State: status.State, ProtocolVersion: status.ProtocolVersion, InstanceID: status.InstanceID, PID: status.PID,
		Version: status.Version, Socket: status.Socket, ViewerURL: status.ViewerURL, Observations: status.Observations,
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(report)
	}
	fmt.Fprintf(stdout, "  diffx %s %s (PID %d)\n", report.Version, report.State, report.PID)
	fmt.Fprintf(stdout, "  socket:        %s\n", report.Socket)
	if report.ViewerURL != "" {
		fmt.Fprintf(stdout, "  viewer:        %s\n", report.ViewerURL)
	} else {
		fmt.Fprintln(stdout, "  viewer:        none (headless)")
	}
	fmt.Fprintf(stdout, "  observations:  %d\n", report.Observations)
	return nil
}
