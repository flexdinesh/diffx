package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flexdinesh/diffx/internal/browser"
	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/localserver"
	buildversion "github.com/flexdinesh/diffx/internal/version"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

func run(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	if len(arguments) > 0 {
		switch arguments[0] {
		case localprotocol.Command:
			return runDesktopSession(ctx, arguments[1:], stdin, stdout, stderr)
		case "diagnostics":
			return runDiagnostics(arguments[1:], stdout)
		case "config":
			return runConfigInput(arguments[1:], stdin, stdout)
		case "sync":
			return runSync(ctx, arguments[1:], stdin, stdout, stderr)
		case "remote":
			return runRemote(ctx, arguments[1:], stdout)
		case "status":
			return runStatus(ctx, arguments[1:], stdout, stderr)
		case "mcp":
			return runMCPProcess(ctx, arguments[1:], stdin, stdout, stderr)
		case "connect":
			return runConnect(ctx, arguments[1:], stdout, stderr)
		case "hook":
			return runHook(arguments[1:], stdin, stdout)
		case "collector":
			return runCollector(arguments[1:], stdout)
		case "__hook-worker":
			return runHookWorker(ctx, arguments[1:])
		case "dev":
			return runFixture(ctx, arguments[1:], stdin, stdout, stderr)
		case "--version":
			fmt.Fprintln(stdout, buildversion.String())
			return nil
		case "review", "service", "serve", "capture", "__daemon":
			return fmt.Errorf("%s removed; use diffx PATH, sync, or dev --fixture", arguments[0])
		}
	}
	return runLocal(ctx, arguments, stdin, stdout, stderr)
}
func runFixture(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	values, err := parseOptionsMode(arguments, stderr, true)
	if errors.Is(err, errHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if values.fixture == "" {
		return errors.New("dev requires --fixture")
	}
	if values.headless {
		return errors.New("dev serves its fixture to a viewer; --headless applies only to diffx [PATH]")
	}
	if err := values.singleCheckout(); err != nil {
		return err
	}
	input, err := acquireInput(values, stdin)
	if err != nil {
		return err
	}
	request, err := collectInitialInput(ctx, values, input)
	if err != nil {
		return err
	}

	settings, err := resolvedServerSettings(values)
	if err != nil {
		return err
	}
	if os.Getenv("DIFFX_EXIT_ON_STDIN_CLOSE") == "1" {
		child, cancel := context.WithCancel(ctx)
		defer cancel()
		ctx = child
		go func() { _, _ = io.Copy(io.Discard, stdin); cancel() }()
	}
	// Fixture stdin carries input and the exit-on-close hook, so dev never prompts.
	reset, err := localStateReset(ctx, values, settings, nil, stderr)
	if err != nil {
		return err
	}
	started := time.Now()
	return withResetHint(localserver.Run(ctx, settings, "", &request, reset, func(status localserver.Status, submitted *contextservice.Publication) {
		address := status.ViewerURL + "/contexts/" + url.PathEscape(submitted.Context.ID)
		writeStartup(stdout, publicationInput(*submitted, time.Since(started)), address)
		openBrowser(values, address, stderr)
	}))
}
func resolvedServerSettings(values options) (localserver.Settings, error) {
	saved, err := config.LoadFile(values.configFile)
	if err != nil {
		return localserver.Settings{}, err
	}
	if values.portSet {
		saved.Local.Port = &values.port
	}
	if values.stateSet {
		saved.Local.State = values.state
		if saved.Local.State == "" {
			defaults, err := config.Default()
			if err != nil {
				return localserver.Settings{}, err
			}
			saved.Local.State = defaults.Local.State
		}
	}
	if values.webDirSet {
		saved.Local.WebDir = values.webDir
	}
	if values.retentionDaysSet {
		saved.Local.RetentionDays = values.retentionDays
	}
	settings, err := saved.Settings()
	settings.Headless = values.headless
	return settings, err
}

func openBrowser(values options, url string, stderr io.Writer) {
	if !values.noBrowser {
		if err := browser.Open(url); err != nil {
			fmt.Fprintf(stderr, "  diffx: %v\n", err)
		}
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		var exit exitStatus
		if errors.As(err, &exit) {
			os.Exit(int(exit))
		}
		fmt.Fprintf(os.Stderr, "  diffx: %v\n", err)
		os.Exit(1)
	}
}

// exitStatus ends the process with its code after the command already wrote
// its own output, such as status reporting a stopped instance.
type exitStatus int

func (code exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(code)) }
