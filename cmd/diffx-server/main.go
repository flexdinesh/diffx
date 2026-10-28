package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/version"
)

func parseSettings(arguments []string, stderr io.Writer) (remoteserver.Settings, bool, error) {
	var settings remoteserver.Settings
	flags := flag.NewFlagSet("diffx-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&settings.Listen, "listen", net.JoinHostPort("127.0.0.1", strconv.Itoa(config.DefaultServerPort)), "HTTP listen address; terminate TLS at the deployment boundary")
	flags.StringVar(&settings.State, "state", "", "SQLite database path")
	flags.StringVar(&settings.PublicURL, "public-url", "", "browser-facing https origin used in the logged admin setup link; also enables Web Push")
	flags.BoolVar(&settings.InsecureCookies, "insecure-cookies", false, "allow browser sessions over HTTP for development")
	flags.IntVar(&settings.RetentionDays, "retention-days", 7, "snapshot retention in days")
	path := flags.String("config-file", "", "optional read-only server JSON config path; defaults to DIFFX_SERVER_CONFIG")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(arguments); err != nil {
		return settings, false, err
	}
	if *showVersion {
		return settings, true, nil
	}
	if flags.NArg() != 0 {
		return settings, false, fmt.Errorf("diffx-server takes no positional arguments")
	}
	values, err := config.LoadServerFile(*path)
	if err != nil {
		return settings, false, err
	}
	visited := make(map[string]bool)
	flags.Visit(func(value *flag.Flag) { visited[value.Name] = true })
	if !visited["listen"] {
		settings.Listen, err = values.ListenAddress()
		if err != nil {
			return settings, false, err
		}
	}
	if !visited["state"] || settings.State == "" {
		settings.State = values.State
	}
	if !visited["public-url"] {
		settings.PublicURL = values.PublicURL
	}
	if !visited["insecure-cookies"] {
		settings.InsecureCookies = values.InsecureCookies
	}
	if !visited["retention-days"] {
		settings.RetentionDays = values.RetentionDays
	}
	if settings.RetentionDays <= 0 {
		return settings, false, fmt.Errorf("retention days must be positive")
	}
	if settings.State == "" {
		return settings, false, fmt.Errorf("state must be a database path or memory")
	}
	values.PublicURL, values.InsecureCookies = settings.PublicURL, settings.InsecureCookies
	if err := values.ValidateIdentity(); err != nil {
		return settings, false, err
	}
	if values.Google.Enabled() {
		settings.Google = &remoteserver.GoogleSettings{ClientID: values.Google.ClientID, ClientSecret: values.Google.ClientSecret}
	}
	if _, err := remoteserver.Retention(settings.RetentionDays); err != nil {
		return settings, false, err
	}
	if err := values.GitHub.Validate(settings.InsecureCookies); err != nil {
		return settings, false, err
	}
	if github := values.GitHub; github.Enabled() {
		internal, err := values.InternalAddress(settings.Listen)
		if err != nil {
			return settings, false, err
		}
		settings.GitHub = &remoteserver.GitHubSettings{AppID: github.AppID, ClientID: github.ClientID, ClientSecret: github.ClientSecret,
			PrivateKeyFile: github.PrivateKeyFile, WebhookSecret: github.WebhookSecret, TokenKey: github.TokenKey, PublicURL: github.PublicURL,
			InternalListen: internal, FetcherSecret: github.FetcherSecret}
	}
	return settings, false, nil
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "user" {
		if err := manageUser(os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			if err == flag.ErrHelp {
				return
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	settings, showVersion, err := parseSettings(os.Args[1:], os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if showVersion {
		fmt.Println(version.String())
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := remoteserver.Run(ctx, settings, func(address string) { fmt.Printf("diffx-server listening on %s\n", address) }); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Offline admin recovery shares the server's exclusive database lock. Only the
// admin has a password; the setup link creates it. Passwords are read from an
// explicitly selected environment variable or stdin; they are never command
// arguments or printed output.
func manageUser(arguments []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "reset-password" {
		return fmt.Errorf("usage: diffx-server user reset-password --username ADMIN --password-env VAR|--password-stdin [--state PATH]")
	}
	operation := arguments[0]
	flags := flag.NewFlagSet("diffx-server user "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	username := flags.String("username", "", "admin username")
	state := flags.String("state", "", "SQLite database path; server must be stopped")
	configPath := flags.String("config-file", "", "optional server JSON config path")
	passwordEnv := flags.String("password-env", "", "read password from this environment variable")
	passwordStdin := flags.Bool("password-stdin", false, "read password from stdin")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *username == "" || (*passwordEnv != "") == *passwordStdin {
		return fmt.Errorf("provide username and exactly one password source")
	}
	values, err := config.LoadServerFile(*configPath)
	if err != nil {
		return err
	}
	if *state == "" {
		*state = values.State
	}
	if *state == "memory" || *state == ":memory:" {
		return fmt.Errorf("user administration requires a persistent state database")
	}
	var password string
	if *passwordStdin {
		raw, err := io.ReadAll(io.LimitReader(stdin, 1025))
		if err != nil {
			return err
		}
		if len(raw) > 1024 {
			return fmt.Errorf("password input too long")
		}
		password = strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	} else {
		password = os.Getenv(*passwordEnv)
	}
	store, err := reviewstore.Open(*state)
	if err != nil {
		return err
	}
	defer store.Close()
	err = identityservice.New(store).ResetPassword(context.Background(), *username, password)
	if errors.Is(err, identityservice.ErrNotFound) {
		return fmt.Errorf("no admin account named %s", *username)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "User %s: %s complete\n", *username, operation)
	return err
}
