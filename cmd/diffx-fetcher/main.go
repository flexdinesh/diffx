package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/fetcher"
	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("diffx-fetcher", flag.ContinueOnError)
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version.String())
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("diffx-fetcher takes no positional arguments; configure it with DIFFX_FETCHER_* environment")
	}
	values, err := config.LoadFetcher()
	if err != nil {
		return err
	}
	client, err := fetchprotocol.NewClient(values.ServerURL, values.Secret)
	if err != nil {
		return err
	}
	// Collection inherits this process environment; it must never fetch.
	if err := os.Setenv("GIT_NO_LAZY_FETCH", "1"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("diffx-fetcher polling %s\n", values.ServerURL)
	return fetcher.Run(ctx, client, fetcher.Settings{
		Mirrors: values.Mirrors, DiskLimit: values.DiskLimitBytes, Concurrency: values.Concurrency, CollectorVersion: version.String(),
	})
}
