package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/version"
)

func parseSettings(arguments []string, stderr io.Writer) (remoteserver.Settings, bool, error) {
	var settings remoteserver.Settings
	flags := flag.NewFlagSet("diffx-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&settings.Listen, "listen", net.JoinHostPort("127.0.0.1", strconv.Itoa(config.DefaultServerPort)), "HTTP listen address; terminate TLS at the deployment boundary")
	flags.StringVar(&settings.PublicURL, "public-url", "", "browser-facing https origin used for Google sign-in and Web Push")
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
	if err := values.ValidateDatabase(); err != nil {
		return settings, false, err
	}
	settings.DatabaseURL = values.DatabaseURL
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
	values.PublicURL, values.InsecureCookies = settings.PublicURL, settings.InsecureCookies
	if err := values.ValidateIdentity(); err != nil {
		return settings, false, err
	}
	settings.Members = identityservice.MemberPolicy{Signup: values.Signup, Password: values.PasswordSignIn}
	if values.Google.Enabled() {
		settings.Google = &remoteserver.GoogleSettings{ClientID: values.Google.ClientID, ClientSecret: values.Google.ClientSecret}
	}
	if err := values.ValidateAuthRateLimit(); err != nil {
		return settings, false, err
	}
	if values.AuthRateLimit {
		settings.ClientIPHeader = values.ClientIPHeader
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
