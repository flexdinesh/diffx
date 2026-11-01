package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Host checks get their own source and dependencies. They never build into the
// user's checkout or contaminate the source subsequently imported by Dagger.
func executeHost(ctx context.Context, source string, group checkGroup, artifacts string) error {
	snapshot, cleanup, err := captureSnapshot(ctx, source, false)
	if err != nil {
		return err
	}
	defer cleanup()
	directory := filepath.Join(artifacts, group.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "diffx-host-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || key == "TMPDIR" || key == "HUSKY" || key == "GOFLAGS" || key == "GOMAXPROCS" {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment, "HUSKY=0", "GOFLAGS=-p=2", "GOMAXPROCS=2", "TMPDIR="+temporary)
	commands := [][]string{
		{"mise", "trust", "--all"},
		{"mise", "exec", "--", "pnpm", "install", "--frozen-lockfile"},
	}
	commands = append(commands, group.Commands...)
	for index, command := range commands {
		path := filepath.Join(directory, fmt.Sprintf("%02d.log", index))
		log, err := os.Create(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "check-runner: host %s (log: %s)\n", strings.Join(command, " "), path)
		runErr := runCommand(ctx, snapshot.Root, environment, command, log, log)
		if err := errors.Join(runErr, log.Close()); err != nil {
			return fmt.Errorf("%s: %w (log: %s)", command[0], err, path)
		}
	}
	return nil
}
