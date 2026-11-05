package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/packages/localprotocol"
	"golang.org/x/sys/unix"
)

func runDesktopSession(ctx context.Context, arguments []string, stdin *os.File, stdout, stderr io.Writer) error {
	values, err := parseOptionsSession(arguments, stderr, true, true)
	if err != nil {
		return err
	}
	if values.parentFD < 3 {
		return errors.New("--parent-fd requires an inherited readable pipe descriptor >= 3")
	}
	ctx, closeLifetime, err := parentLifetime(ctx, values.parentFD)
	if err != nil {
		return err
	}
	defer closeLifetime()
	if values.serverSet || values.tokenSet || values.branch != "" || values.fixture != "" || values.print || values.retry || values.headless || values.version {
		return errors.New("desktop session accepts local checkout/scanning/capture options")
	}
	values.noBrowser, values.diagnosticWriter = true, stderr
	var settings localserver.Settings
	if !values.attach {
		settings, err = resolvedServerSettings(values)
		if err != nil {
			return err
		}
		settings.Headless = !values.portSet && !values.webDirSet
	}
	var selected localserver.Input
	if !values.attach {
		// A parent may die while capture stdin is still open. A pollable duplicate
		// lets cancellation interrupt that read without consuming the lifetime pipe.
		inputFile, closeInput, err := sessionInput(ctx, stdin)
		if err != nil {
			return err
		}
		input, err := acquireLocalInput(ctx, values, inputFile)
		closeInput()
		if err != nil {
			return sessionResult(ctx, err)
		}
		if input.Kind == "capture" && values.base != "" {
			return errors.New("base is only supported for a checkout path")
		}
		selected, err = prepareLocalSession(ctx, &values, input, stderr)
		if err != nil {
			return sessionResult(ctx, err)
		}
	}
	reset, err := localStateReset(ctx, values, settings, nil, stderr)
	if err != nil {
		return err
	}
	err = localserver.RunSession(ctx, settings, selected, localserver.SessionOptions{Attach: values.attach, Replace: values.replace}, reset, func(ready localprotocol.Readiness) error {
		if err := json.NewEncoder(stdout).Encode(ready); err != nil {
			return fmt.Errorf("write desktop readiness: %w", err)
		}
		return nil
	}, stderr)
	return sessionResult(ctx, withResetHint(err))
}

func sessionResult(ctx context.Context, err error) error {
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, os.ErrClosed)) {
		return nil
	}
	return err
}

// parentLifetime consumes only the inherited lifetime pipe. EOF (including
// parent crash), a read failure or signals cancel the same local ownership path.
func parentLifetime(ctx context.Context, fd int) (context.Context, func(), error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, nil, fmt.Errorf("parent pipe: %w", err)
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFIFO || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return nil, nil, errors.New("--parent-fd must name a readable lifetime pipe")
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}
	unix.CloseOnExec(fd)
	pipe := os.NewFile(uintptr(fd), "desktop-parent")
	if pipe == nil {
		return nil, nil, errors.New("invalid parent pipe")
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, pipe)
		cancel()
	}()
	return ctx, func() {
		cancel()
		_ = pipe.Close()
		<-done
	}, nil
}

func sessionInput(ctx context.Context, stdin *os.File) (*os.File, func(), error) {
	if stdin == nil {
		return nil, func() {}, nil
	}
	info, err := stdin.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return stdin, func() {}, nil
	}
	// Duplicates share file status flags. Leave terminals alone, and restore a
	// pipe's original flags as soon as capture acquisition has finished.
	original, err := stdin.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var fd, flags int
	var copyErr error
	err = original.Control(func(sourceFD uintptr) {
		flags, copyErr = unix.FcntlInt(sourceFD, unix.F_GETFL, 0)
		if copyErr == nil {
			fd, copyErr = unix.Dup(int(sourceFD))
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if copyErr != nil {
		return nil, nil, copyErr
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), "desktop-input")
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	return file, func() {
		stop()
		_ = file.Close()
		_ = original.Control(func(sourceFD uintptr) {
			_ = unix.SetNonblock(int(sourceFD), flags&unix.O_NONBLOCK != 0)
		})
	}, nil
}

func prepareLocalSession(ctx context.Context, values *options, input initialInput, stderr io.Writer) (localserver.Input, error) {
	if values.sourceID == "" {
		id, err := collector.SourceID()
		if err != nil {
			return localserver.Input{}, err
		}
		values.sourceID = id
	}
	metadata := localprotocol.Input{Kind: "capture"}
	if input.Kind == "worktree" {
		metadata.Kind, metadata.Selection = string(input.Selection), string(input.Selection)
		for _, path := range input.Paths {
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				return localserver.Input{}, err
			}
			// Subdirectories of the same checkout collect the same root.
			repository, err := diffsource.OpenRepository(ctx, canonical)
			if err == nil {
				canonical = repository.Root()
			} else if !errors.Is(err, diffsource.ErrNotRepository) {
				return localserver.Input{}, err
			}
			metadata.Paths = append(metadata.Paths, canonical)
		}
		slices.Sort(metadata.Paths)
		metadata.Paths = slices.Compact(metadata.Paths)
	}
	base := values.base
	if base == "" {
		base = "auto"
	}
	depth := 0
	if input.Selection == localserver.DiscoveredCheckouts {
		depth = values.scanDepth
	}
	origin := input.SubmittedFrom
	if origin != "" {
		var err error
		origin, err = filepath.EvalSymlinks(origin)
		if err != nil {
			return localserver.Input{}, err
		}
	}
	identity := struct {
		Input                                                      localprotocol.Input
		Depth                                                      int
		Base, SourceID, Trigger, Agent, RunID, SessionName, Origin string
		Capture                                                    [sha256.Size]byte
	}{metadata, depth, base, values.sourceID, values.trigger, values.agent, values.runID, values.sessionName, origin, sha256.Sum256(input.Raw)}
	raw, err := json.Marshal(identity)
	if err != nil {
		return localserver.Input{}, err
	}
	digest := sha256.Sum256(raw)
	selected := localserver.Input{Session: &localprotocol.Session{ProtocolVersion: localprotocol.Version, ScopeID: hex.EncodeToString(digest[:]), Input: metadata}}
	if input.Kind == "worktree" {
		settings := *values
		selected.Selection = input.Selection
		selected.Collect = func(ctx context.Context, publish func(ingestion.Request) error) ([]localserver.CollectionFailure, error) {
			return collectWorkspaces(ctx, settings, input, stderr, publish)
		}
	} else {
		request, err := collectInitialInput(ctx, *values, input)
		if err != nil {
			return localserver.Input{}, err
		}
		selected.Collection = localserver.Collection{Requests: []ingestion.Request{request}}
	}
	return selected, nil
}
