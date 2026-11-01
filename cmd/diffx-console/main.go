package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/consoleserver"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/version"
)

// settings resolves the console from DIFFX_CONSOLE_* environment variables.
func settings() (consoleserver.Settings, error) {
	values, err := config.LoadConsole()
	if err != nil {
		return consoleserver.Settings{}, err
	}
	if err := values.Validate(); err != nil {
		return consoleserver.Settings{}, err
	}
	listen, err := values.ListenAddress()
	if err != nil {
		return consoleserver.Settings{}, err
	}
	result := consoleserver.Settings{Listen: listen, DatabaseURL: values.DatabaseURL, PublicURL: values.PublicURL, AppURL: values.AppURL, InsecureCookies: values.InsecureCookies}
	if values.Google.Enabled() {
		result.Google = &consoleserver.GoogleSettings{ClientID: values.Google.ClientID, ClientSecret: values.Google.ClientSecret}
	}
	if values.AuthRateLimit {
		result.ClientIPHeader = values.ClientIPHeader
	}
	return result, nil
}

// manageUser issues a password reset link for an account, so a locked-out
// admin or user recovers without the console.
func manageUser(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "reset-password" {
		return errors.New("usage: diffx-console user reset-password --email EMAIL")
	}
	flags := flag.NewFlagSet("diffx-console user reset-password", flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "password account email")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *email == "" {
		return errors.New("provide --email")
	}
	console, err := settings()
	if err != nil {
		return err
	}
	store, err := consoleserver.Open(ctx, console.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	link, err := consoleserver.ResetPassword(ctx, store, console, *email)
	if errors.Is(err, identityservice.ErrNotFound) {
		return fmt.Errorf("no password account with email %s", *email)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Reset link for %s, valid for %s and usable once:\n%s\n", *email, identityservice.PasswordResetLifetime, link)
	return err
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) > 0 && arguments[0] == "user" {
		return manageUser(ctx, arguments[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("diffx-console", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: diffx-console [--version] | diffx-console user reset-password --email EMAIL")
		fmt.Fprintln(stderr, "Configure with DIFFX_CONSOLE_* environment variables.")
	}
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *showVersion {
		_, err := fmt.Fprintln(stdout, version.String())
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("diffx-console takes no positional arguments")
	}
	console, err := settings()
	if err != nil {
		return err
	}
	return consoleserver.Run(ctx, console, func(address string) { fmt.Fprintf(stdout, "diffx-console listening on %s\n", address) })
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
