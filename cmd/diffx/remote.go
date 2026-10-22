package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/flexdinesh/diffx/internal/hooks"
	"github.com/flexdinesh/diffx/internal/submission"
)

// runRemote reports whether sync and agent hooks can deliver to the configured
// server. Hooks never write to the conversation, so this is where their failures surface.
func runRemote(ctx context.Context, arguments []string, stdout io.Writer) (failure error) {
	if len(arguments) == 0 || arguments[0] != "status" {
		return errors.New("usage: diffx remote status [--config-file FILE]")
	}
	flags := flag.NewFlagSet("diffx remote status", flag.ContinueOnError)
	flags.SetOutput(stdout)
	configFile := flags.String("config-file", "", "machine JSON config path; defaults to DIFFX_CONFIG_PATH")
	if err := flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("remote status takes no positional arguments")
	}
	values, err := resolvedCollectorSettings(options{configFile: *configFile})
	if err != nil {
		return err
	}
	if values.server == "" || values.token == "" {
		return errRemoteUnconfigured
	}
	defer func() {
		if failure != nil {
			failure = fmt.Errorf("%s", strings.ReplaceAll(failure.Error(), values.token, "[redacted]"))
		}
	}()
	fmt.Fprintf(stdout, "  remote:  %s\n", values.server)
	destination, err := submission.Resolve(ctx, values.server, values.token)
	if err != nil {
		return fmt.Errorf("remote unavailable: %w", err)
	}
	fmt.Fprintf(stdout, "  server:  ok, accepting agent and sync ingestion (database %.12s)\n", destination.Target.Identity)
	directory, err := hookDirectory()
	if err != nil {
		return err
	}
	statuses, err := newHookEngine(directory).Statuses()
	if err != nil {
		return err
	}
	var waiting []hooks.Activity
	for _, status := range statuses {
		if status.Status != "complete" && status.Status != "unchanged" && status.Status != "skipped" {
			waiting = append(waiting, status)
		}
	}
	if len(waiting) == 0 {
		fmt.Fprintln(stdout, "  hooks:   no waiting deliveries")
		return nil
	}
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].Time.After(waiting[j].Time) })
	fmt.Fprintf(stdout, "  hooks:   %d waiting; diffx collector retry delivers them now\n", len(waiting))
	latest := waiting[0]
	if latest.Error != "" {
		fmt.Fprintf(stdout, "  latest:  %s %s: %s\n", latest.Time.Local().Format("2006-01-02 15:04"), latest.Path, latest.Error)
	}
	return nil
}
