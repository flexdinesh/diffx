// check-runner validates immutable sources in per-run Docker Compose projects.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "check-runner:", err)
		if ctx.Err() != nil {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

type invocation struct {
	Mode, Group string
	Force       bool
}

func parseInvocation(args []string) (invocation, error) {
	var result invocation
	for _, arg := range args {
		if arg == "--force" {
			result.Force = true
			continue
		}
		if result.Mode == "" {
			result.Mode = arg
			continue
		}
		if result.Mode == "group" && result.Group == "" {
			result.Group = arg
			continue
		}
		return result, fmt.Errorf("unexpected argument %q", arg)
	}
	switch result.Mode {
	case "check", "pr", "status":
	case "group":
		for _, group := range groups(true) {
			if group.Name == result.Group {
				return result, nil
			}
		}
		return result, fmt.Errorf("unknown check group %q", result.Group)
	default:
		return result, errors.New("usage: check-runner check|pr|status|group NAME [--force]")
	}
	return result, nil
}

func run(ctx context.Context, args []string) error {
	inv, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("isolated checks currently require Linux x64")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	committed := inv.Mode == "pr" || inv.Mode == "status"
	snapshot, cleanup, err := captureSnapshot(ctx, root, committed)
	if err != nil {
		return err
	}
	defer cleanup()
	records, err := recordDirectory(ctx, root)
	if err != nil {
		return err
	}
	selected := groups(true)
	if inv.Mode == "group" {
		for _, group := range selected {
			if group.Name == inv.Group {
				selected = []checkGroup{group}
				break
			}
		}
	}
	var pending []checkGroup
	keys := make(map[string]string)
	statuses := make(map[string]string)
	for _, group := range selected {
		key, err := groupFingerprint(snapshot.Root, group)
		if err != nil {
			return err
		}
		keys[group.Name] = key
		if inv.Force {
			path, err := validationRecordPath(records, group.Name, key)
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		_, found, err := loadValidationRecord(records, group.Name, key)
		if err != nil {
			return err
		}
		if found && !inv.Force {
			statuses[group.Name] = "reused"
			fmt.Fprintln(os.Stderr, "check-runner: reused", group.Name)
			continue
		}
		fmt.Fprintln(os.Stderr, "check-runner: pending", group.Name)
		statuses[group.Name] = "pending"
		pending = append(pending, group)
	}
	if inv.Mode == "status" {
		if len(pending) > 0 {
			return errors.New("HEAD has incomplete validation; run mise run check:pr")
		}
		return checkSnapshotCurrent(ctx, root, snapshot, committed)
	}
	if len(pending) == 0 {
		return checkSnapshotCurrent(ctx, root, snapshot, committed)
	}
	slot, release, err := admit(ctx)
	if err != nil {
		return err
	}
	defer release()
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	id := time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(idBytes)
	artifacts := filepath.Join(root, "dist", "check-runs", id)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(artifacts, "execution.log"))
	if err != nil {
		return err
	}
	defer log.Close()
	var mu sync.Mutex
	report := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(io.MultiWriter(os.Stderr, log), "check-runner: "+format+"\n", args...)
	}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for group, status := range statuses {
			if status == "running" || (status == "pending" && ctx.Err() != nil) {
				statuses[group] = "interrupted"
			}
		}
		result, err := json.MarshalIndent(struct {
			Commit, Tree, Source, Platform string
			Inputs, Results                map[string]string
		}{snapshot.Commit, snapshot.Tree, snapshot.Identity, "linux/amd64", keys, statuses}, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join(artifacts, "results.json"), result, 0o644)
		}
		_ = os.WriteFile(filepath.Join(artifacts, "complete"), []byte(snapshot.Commit+"\n"), 0o644)
		_ = pruneArtifacts(filepath.Dir(artifacts))
	}()
	contexts := ""
	if slices.ContainsFunc(pending, func(group checkGroup) bool { return !group.Host }) {
		if contexts, err = prepareContainers(ctx, slot, snapshot.Root); err != nil {
			return err
		}
		defer func() {
			_ = os.RemoveAll(contexts)
			cleanup, cancel := context.WithTimeout(context.Background(), teardownTimeout)
			defer cancel()
			if err := untagSlotImages(cleanup, dockerOutput, slot); err != nil {
				report("cleanup: %v", err)
			}
		}()
	}
	starts := make(map[string]time.Time)
	var recordErrs []error
	outcomes := schedule(ctx, pending, groupConcurrency, scheduleHooks{
		build: func(ctx context.Context, target string) error {
			path := filepath.Join(artifacts, "image-"+target+".log")
			buildLog, err := os.Create(path)
			if err != nil {
				return err
			}
			report("building image %s (log: %s)", target, path)
			err = buildImage(ctx, slot, target, snapshot.Root, contexts, buildLog)
			if err := errors.Join(err, buildLog.Close()); err != nil {
				return fmt.Errorf("%w (log: %s)", err, path)
			}
			return nil
		},
		execute: func(ctx context.Context, group checkGroup) error {
			if group.Host {
				return executeHost(ctx, snapshot.Root, group, artifacts)
			}
			return runGroup(ctx, dockerOutput, slot, group, id, snapshot.Root, artifacts)
		},
		started: func(group checkGroup) {
			mu.Lock()
			statuses[group.Name] = "running"
			starts[group.Name] = time.Now()
			mu.Unlock()
			report("executing %s", group.Name)
		},
		finished: func(group checkGroup, err error) {
			mu.Lock()
			elapsed := time.Since(starts[group.Name]).Round(time.Second)
			mu.Unlock()
			if err == nil {
				record := validationRecord{Group: group.Name, Fingerprint: keys[group.Name], Commit: snapshot.Commit, Artifacts: artifacts, CompletedAt: time.Now().UTC()}
				err = saveValidationRecord(records, record)
				if err != nil {
					mu.Lock()
					recordErrs = append(recordErrs, err)
					mu.Unlock()
				}
			}
			status := "passed"
			switch {
			case err != nil && ctx.Err() != nil:
				status = "interrupted"
			case err != nil:
				status = "failed"
			}
			mu.Lock()
			statuses[group.Name] = status
			mu.Unlock()
			if err != nil {
				report("%s %s: %v", status, group.Name, err)
				return
			}
			report("ok %s (%s)", group.Name, elapsed)
		},
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	var failures []error
	for _, group := range pending {
		if err := outcomes[group.Name]; err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", group.Name, err))
		}
	}
	if err := errors.Join(append(failures, recordErrs...)...); err != nil {
		return fmt.Errorf("%w\nartifacts: %s", err, artifacts)
	}
	if err := checkSnapshotCurrent(ctx, root, snapshot, committed); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(artifacts, "complete"), []byte(snapshot.Commit+"\n"), 0o644); err != nil {
		return err
	}
	return pruneArtifacts(filepath.Dir(artifacts))
}

func groupFingerprint(root string, group checkGroup) (string, error) {
	files, err := fingerprintFiles(root, func(path string) bool { return groupInput(group.Name, path) })
	if err != nil {
		return "", err
	}
	mise, err := exec.LookPath("mise")
	if err != nil {
		return "", err
	}
	binary, err := os.ReadFile(mise)
	if err != nil {
		return "", err
	}
	miseDigest := sha256.Sum256(binary)
	// The Dockerfile and compose.yaml, including pinned image digests, are
	// already file inputs of every group through tools/check-runner.
	files += hex.EncodeToString(miseDigest[:])
	if group.Host {
		// Host results cannot be reused against different installed runtimes.
		for _, tool := range []string{"go", "node", "pnpm"} {
			path, err := exec.LookPath(tool)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			digest := sha256.Sum256(data)
			files += hex.EncodeToString(digest[:])
		}
	}
	policy, err := json.Marshal(struct {
		Group           checkGroup
		Files, Platform string
	}{group, files, "linux/amd64"})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(policy)
	return hex.EncodeToString(hash[:]), nil
}
