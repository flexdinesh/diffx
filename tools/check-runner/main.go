// check-runner validates immutable sources in explicitly managed Dagger sessions.
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
	"strings"
	"syscall"
	"time"

	"dagger.io/dagger"
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
	case "check", "pr", "status", "engine-setup":
	case "group":
		for _, group := range groups(true) {
			if group.Name == result.Group {
				return result, nil
			}
		}
		return result, fmt.Errorf("unknown check group %q", result.Group)
	default:
		return result, errors.New("usage: check-runner check|pr|status|engine-setup|group NAME [--force]")
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
	if inv.Mode == "engine-setup" {
		return setupEngine(ctx)
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
	release, err := admit(ctx)
	if err != nil {
		return err
	}
	defer release()
	env, err := engineEnvironment(ctx)
	if err != nil {
		return err
	}
	for _, name := range []string{"DAGGER_SESSION_PORT", "DAGGER_SESSION_TOKEN", "DAGGER_ENGINE", "_EXPERIMENTAL_DAGGER_CLI_BIN", "_EXPERIMENTAL_DAGGER_RUNNER_HOST"} {
		os.Unsetenv(name)
	}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	cli, err := exec.LookPath("dagger")
	if err != nil {
		return err
	}
	if err := os.Setenv("_EXPERIMENTAL_DAGGER_CLI_BIN", cli); err != nil {
		return err
	}
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
	defer func() {
		for group, status := range statuses {
			if status == "running" {
				statuses[group] = "interrupted"
			}
		}
		report, err := json.MarshalIndent(struct {
			Commit, Tree, Source, Engine, Platform string
			Inputs, Results                        map[string]string
		}{snapshot.Commit, snapshot.Tree, snapshot.Identity, engineImage, "linux/amd64", keys, statuses}, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join(artifacts, "results.json"), report, 0o644)
		}
		_ = os.WriteFile(filepath.Join(artifacts, "complete"), []byte(snapshot.Commit+"\n"), 0o644)
		_ = pruneArtifacts(filepath.Dir(artifacts))
	}()
	client, err := dagger.Connect(ctx, dagger.WithRunnerHost("docker-container://"+engineName), dagger.WithLogOutput(io.MultiWriter(os.Stderr, log)))
	if err != nil {
		return err
	}
	defer client.Close()
	pipeline := newPipeline(client, snapshot.Root)
	for _, group := range pending {
		statuses[group.Name] = "running"
		fmt.Fprintln(os.Stderr, "check-runner: executing", group.Name)
		started := time.Now()
		if err := pipeline.execute(ctx, group, id, artifacts); err != nil {
			if ctx.Err() == nil {
				statuses[group.Name] = "failed"
			}
			return fmt.Errorf("%s: %w (artifacts: %s)", group.Name, err, artifacts)
		}
		record := validationRecord{Group: group.Name, Fingerprint: keys[group.Name], Commit: snapshot.Commit, Artifacts: artifacts, CompletedAt: time.Now().UTC()}
		if err := saveValidationRecord(records, record); err != nil {
			return err
		}
		statuses[group.Name] = "passed"
		fmt.Fprintf(os.Stderr, "check-runner: ok %s (%s)\n", group.Name, time.Since(started).Round(time.Second))
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
	files += hex.EncodeToString(miseDigest[:]) + engineImage
	policy, err := json.Marshal(struct {
		Group                         checkGroup
		Files, Platform, Base, Dagger string
	}{group, files, "linux/amd64", runnerImage, daggerVersion})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(policy)
	return hex.EncodeToString(hash[:]), nil
}
