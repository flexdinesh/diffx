package main

import "github.com/mattn/go-isatty"

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

func runLocal(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	values, err := parseOptionsMode(arguments, stderr, true)
	if errors.Is(err, errHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if values.serverSet || values.tokenSet || values.branch != "" || values.fixture != "" || values.print || values.retry {
		return errors.New("local mode accepts a checkout path or piped diff; use sync for remote collection or dev --fixture for fixtures")
	}
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
	started := time.Now()
	label := "Piped diff"
	var selected localserver.Input
	if input.Kind == "worktree" {
		label = strings.Join(input.Paths, ", ")
		collect := func(ctx context.Context, publish func(ingestion.Request) error) ([]localserver.CollectionFailure, error) {
			return collectWorkspaces(ctx, values, input, stderr, publish)
		}
		selected = localserver.Input{Collect: collect, Selection: input.Selection}
	} else {
		request, err := collectInitialInput(ctx, values, input)
		if err != nil {
			return err
		}
		selected = localserver.Input{Collection: localserver.Collection{Requests: []ingestion.Request{request}}}
	}
	reset, err := localStateReset(ctx, values, settings, stdin, stderr)
	if err != nil {
		return err
	}
	return withResetHint(localserver.RunForeground(ctx, settings, selected, func(status localserver.Status) (bool, error) {
		if values.replace {
			return true, nil
		}
		if stdin == nil || !isatty.IsTerminal(stdin.Fd()) {
			return false, fmt.Errorf("diffx already running (PID %d); use --replace to restart here", status.PID)
		}
		return askYes(ctx, stdin, stderr, fmt.Sprintf("diffx is running (PID %d).\nStop it and open %s? [y/N] ", status.PID, label))
	}, reset, func(status localserver.Status, published localserver.Published) {
		if input.Kind == "worktree" {
			address := status.BrowserURL + "/"
			writeStartup(stdout, loadedInput{mode: "git", directory: label, collecting: true, processed: time.Since(started), mcpURL: status.BrowserURL + "/mcp"}, address)
			openBrowser(values, address, stderr)
			return
		}
		submitted, ok := published.Newest()
		if !ok {
			return
		}
		address := status.BrowserURL + "/contexts/" + url.PathEscape(submitted.Context.ID)
		loaded := publicationInput(submitted, time.Since(started))
		loaded.mode, loaded.directory = "pipe", ""
		for _, failure := range published.Failures {
			fmt.Fprintf(stderr, "  diffx: %s: %s\n", failure.Path, failure.Detail)
		}
		loaded.contextID, loaded.mcpURL = submitted.Context.ID, status.BrowserURL+"/mcp"
		writeStartup(stdout, loaded, address)
		openBrowser(values, address, stderr)
	}))
}

// localStateReset composes the local reset decision: --reset-state consents up
// front; otherwise older state is confirmed only on an interactive terminal.
func localStateReset(ctx context.Context, values options, settings localserver.Settings, stdin *os.File, stderr io.Writer) (localserver.StateReset, error) {
	if values.resetState && settings.State == "memory" {
		return localserver.StateReset{}, errors.New("reset-state requires persistent state; remove --state memory")
	}
	reset := localserver.StateReset{Force: values.resetState, Saved: func(backup string) {
		fmt.Fprintf(stderr, "diffx: started fresh; old state saved at %s\n", backup)
	}}
	if stdin != nil && isatty.IsTerminal(stdin.Fd()) {
		reset.Confirm = func(failure *reviewstore.StateError) (bool, error) {
			return askYes(ctx, stdin, stderr, fmt.Sprintf("diffx: saved state uses schema %d; this version needs schema %d.\n  State: %s\n  Saved reviews, comments and reviewed marks will not carry over.\n  Old state moves to a backup beside it.\nBack up old state and start fresh? [y/N] ", failure.Version, failure.Supported(), failure.Path))
		}
	}
	return reset, nil
}

// withResetHint adds the local recovery to refused older state.
func withResetHint(err error) error {
	var failure *reviewstore.StateError
	if errors.As(err, &failure) && failure.Older() {
		return fmt.Errorf("%w\n  Or rerun with --reset-state to back it up beside the database and start fresh.", err)
	}
	return err
}

// askYes asks on the terminal; anything but y or yes declines.
func askYes(ctx context.Context, stdin *os.File, stderr io.Writer, question string) (bool, error) {
	fmt.Fprint(stderr, question)
	type response struct {
		answer string
		err    error
	}
	answers := make(chan response, 1)
	go func() {
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		answers <- response{answer, err}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case reply := <-answers:
		if reply.err != nil && !errors.Is(reply.err, io.EOF) {
			return false, reply.err
		}
		return strings.EqualFold(strings.TrimSpace(reply.answer), "y") || strings.EqualFold(strings.TrimSpace(reply.answer), "yes"), nil
	}
}

// collectWorkspaces collects the input's selected checkout or every checkout
// discovered under its paths. Discovery diagnostics go to stderr; failures stay
// in the collection.
func collectWorkspaces(ctx context.Context, values options, input initialInput, stderr io.Writer, publish func(ingestion.Request) error) ([]localserver.CollectionFailure, error) {
	settings, err := collectionOptions(values)
	if err != nil {
		return nil, err
	}
	workspaces := collector.Workspaces{Paths: input.Paths, Depth: values.scanDepth, LinkedWorktrees: input.Selection == localserver.DiscoveredCheckouts}
	batch, err := collector.CollectWorkspaces(ctx, workspaces, settings, publish)
	for _, diagnostic := range batch.Diagnostics {
		fmt.Fprintf(stderr, "  diffx: %s\n", diagnostic)
	}
	if err != nil {
		return nil, err
	}
	failures := make([]localserver.CollectionFailure, 0, len(batch.Failures))
	for _, failure := range batch.Failures {
		failures = append(failures, localserver.CollectionFailure{Path: failure.Path, Detail: failure.Detail})
	}
	return failures, nil
}

func acquireLocalInput(ctx context.Context, values options, stdin *os.File) (initialInput, error) {
	piped := false
	if stdin != nil {
		var err error
		piped, err = redirected(stdin)
		if err != nil {
			return initialInput{}, err
		}
	}
	if piped && values.repositorySet && values.directory != "-" {
		return initialInput{}, errors.New("checkout path cannot be combined with piped stdin")
	}
	if piped || values.directory == "-" {
		return acquireInput(values, stdin)
	}
	selected := values.paths
	if len(selected) == 0 {
		selected = []string{values.directory}
	}
	paths := make([]string, 0, len(selected))
	for _, path := range selected {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return initialInput{}, err
		}
		// Reject typos before serving; collection runs only after startup.
		if info, err := os.Stat(absolute); err != nil || !info.IsDir() {
			return initialInput{}, fmt.Errorf("checkout path %s is not a readable directory", absolute)
		}
		paths = append(paths, absolute)
	}
	selection, err := checkoutSelection(ctx, values, paths)
	if err != nil {
		return initialInput{}, err
	}
	return initialInput{Kind: "worktree", Paths: paths, Selection: selection}, nil
}

// checkoutSelection scopes a bare or positional checkout path to that checkout
// unless --scan-worktrees adds its repository's other worktrees. --path, and a
// directory outside a repository, discover every checkout and worktree below.
func checkoutSelection(ctx context.Context, values options, paths []string) (localserver.Selection, error) {
	if values.pathSet || len(paths) != 1 {
		return localserver.DiscoveredCheckouts, nil
	}
	if _, err := diffsource.OpenRepository(ctx, paths[0]); errors.Is(err, diffsource.ErrNotRepository) {
		return localserver.DiscoveredCheckouts, nil
	} else if err != nil {
		return "", err
	}
	if values.scanDepthSet {
		return "", errors.New("--scan-depth bounds directory search; a checkout path has none. Use --scan-worktrees for its worktrees")
	}
	if values.scanWorktrees {
		return localserver.DiscoveredCheckouts, nil
	}
	return localserver.SelectedCheckout, nil
}
