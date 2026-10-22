package main

import "github.com/mattn/go-isatty"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/submission"
)

type syncProgress struct {
	writer            io.Writer
	enabled, terminal bool
	previous          string
}

func (progress *syncProgress) show(message string) {
	if !progress.enabled || message == progress.previous {
		return
	}
	progress.previous = message
	if progress.terminal {
		fmt.Fprintf(progress.writer, "\r\x1b[2K%s", message)
	} else {
		fmt.Fprintln(progress.writer, message)
	}
}
func (progress *syncProgress) close() {
	if progress.enabled && progress.terminal {
		fmt.Fprintln(progress.writer)
	}
}

func runSync(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) (failure error) {
	values, err := parseOptionsMode(arguments, stderr, true)
	if errors.Is(err, errHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if values.hostSet || values.portSet || values.stateSet || values.webDirSet || values.retentionDaysSet || values.replace || values.fixture != "" {
		return errors.New("sync is remote-only and does not accept local server options")
	}
	if err := values.singleCheckout(); err != nil {
		return err
	}
	values, err = resolvedCollectorSettings(values)
	if err != nil {
		return err
	}
	if values.server == "" || values.token == "" {
		return errors.New("sync requires remote mode; run diffx config set remote.url URL and diffx config set remote.token -")
	}
	defer func() {
		if failure != nil {
			failure = fmt.Errorf("%s", strings.ReplaceAll(failure.Error(), values.token, "[redacted]"))
		}
	}()
	progress := &syncProgress{writer: stderr, enabled: values.debug}
	if file, ok := stderr.(*os.File); ok {
		progress.terminal = isatty.IsTerminal(file.Fd())
	}
	defer progress.close()
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	var failures []error
	var fresh *ingestion.Request
	if !values.retry {
		progress.show("Collect · reading selected checkout")
		values.progress = func(stage, detail string) { progress.show(stage + " · " + detail) }
		collected, collectErr := collectSubmission(ctx, values)
		if collectErr != nil {
			failures = append(failures, collectErr)
		} else {
			fresh = &collected
		}
	}
	result, err := submission.Sync(ctx, submission.SyncOptions{
		StateDir: stateDir, Server: values.server, Token: values.token,
		Fresh: fresh, Fingerprint: syncFingerprint, Progress: progress.show,
	})
	if err != nil {
		return err
	}
	failures = append(failures, result.Failures...)
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	if result.Empty {
		return errors.New("no pending submissions")
	}
	progress.show("Ingest · complete")
	if values.print {
		fmt.Fprintln(stdout, strings.TrimRight(values.server, "/"))
	} else if result.Unchanged > 0 {
		fmt.Fprintf(stdout, "Synced %d observations (%d unchanged)\n", result.Uploaded, result.Unchanged)
	} else {
		fmt.Fprintf(stdout, "Synced %d observations\n", result.Uploaded)
	}
	return nil
}

// Session metadata must reach the server even when checkout contents match.
func syncFingerprint(request ingestion.Request) string {
	fingerprint := collector.Fingerprint(request)
	if fingerprint == "" {
		return ""
	}
	data, err := json.Marshal([]interface{}{fingerprint, request.Metadata.Agent, request.Metadata.RunID, request.Metadata.AgentSession})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
