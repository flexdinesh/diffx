package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// testDatabaseEnv is read by internal/testsupport/pgtest and test/conformance.
const testDatabaseEnv = "DIFFX_TEST_DATABASE_URL"

// postgresImage matches internal/testsupport/pgtest.Image.
const postgresImage = "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5"

// ownerLabel marks containers started for a Git common directory. The check
// lock admits one run per common directory, so any labeled container found at
// start was left by a killed run.
const ownerLabel = "diffx.check-runner"

// startDatabase starts a server whose role may create databases and returns
// its URL and a stop function that must be called on every exit path.
type startDatabase func(ctx context.Context, common string) (string, func() error, error)

// startDockerPostgres runs one disposable Postgres for every check step, so
// test processes clone databases from it instead of each starting a container.
func startDockerPostgres(ctx context.Context, common string) (string, func() error, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("server tests need Docker for the shared test Postgres (or set %s): %w", testDatabaseEnv, err)
	}
	label := ownerLabel + "=" + common
	if err := removeContainers(ctx, label); err != nil {
		return "", nil, fmt.Errorf("server tests need Docker for the shared test Postgres (or set %s): %w", testDatabaseEnv, err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", nil, err
	}
	name := "diffx-check-" + hex.EncodeToString(suffix)
	stop := func() error {
		// Cleanup must outlive the cancelled run and a repeated terminal
		// interrupt, so it runs in its own process group with a fresh deadline.
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return runCommand(stopCtx, "", os.Environ(), []string{"docker", "rm", "--force", "--volumes", name})
	}
	url, err := runPostgres(ctx, name, label)
	if err != nil {
		return "", nil, errors.Join(err, stop())
	}
	return url, stop, nil
}

func runPostgres(ctx context.Context, name, label string) (string, error) {
	// Docker picks a free loopback port. Data lives in tmpfs; durability
	// settings are off because every database is disposable. Connections cover
	// the test processes that each had their own server before.
	if _, err := docker(ctx, "run", "--detach", "--rm", "--name", name, "--label", label,
		"--publish", "127.0.0.1::5432", "--tmpfs", "/var/lib/postgresql",
		"--env", "POSTGRES_USER=diffx", "--env", "POSTGRES_PASSWORD=diffx", "--env", "POSTGRES_DB=postgres",
		postgresImage,
		"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off", "-c", "max_connections=300",
	); err != nil {
		return "", fmt.Errorf("start shared test Postgres (server tests need Docker, or set %s): %w", testDatabaseEnv, err)
	}
	published, err := docker(ctx, "port", name, "5432/tcp")
	if err != nil {
		return "", err
	}
	address, _, _ := strings.Cut(published, "\n")
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("published Postgres port %q: %w", published, err)
	}
	// The image's init server listens only on its socket; TCP readiness means
	// the final server accepts connections.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := docker(ctx, "exec", name, "pg_isready", "--quiet", "--host=127.0.0.1", "--username=diffx", "--dbname=postgres"); err == nil {
			break
		} else if time.Now().After(deadline) {
			return "", fmt.Errorf("shared test Postgres not ready: %w", err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "postgres://diffx:diffx@" + net.JoinHostPort("127.0.0.1", port) + "/postgres?sslmode=disable", nil
}

// removeContainers removes containers a killed run could not stop.
func removeContainers(ctx context.Context, label string) error {
	ids, err := docker(ctx, "ps", "--all", "--quiet", "--filter", "label="+label)
	if err != nil || ids == "" {
		return err
	}
	_, err = docker(ctx, append([]string{"rm", "--force", "--volumes"}, strings.Fields(ids)...)...)
	return err
}

func docker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			err = fmt.Errorf("docker %s: %w: %s", args[0], err, message)
		} else {
			err = fmt.Errorf("docker %s: %w", args[0], err)
		}
	}
	return strings.TrimSpace(string(output)), err
}
