// Package desktoplaunch hands native launch intents to the matching desktop.
package desktoplaunch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/flexdinesh/diffx/internal/version"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// Open returns true only once the app accepts the request. Runtime/bootstrap
// failure before that point permits the caller's foreground browser fallback.
func Open(ctx context.Context, arguments []string, stdin *os.File, stderr io.Writer) (bool, error) {
	if stdin != nil {
		info, err := stdin.Stat()
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			stdin = nil
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return false, err
	}
	desktop, err := resolve(executable)
	if err != nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	probe := exec.CommandContext(probeCtx, desktop, "--desktop-contract")
	probe.WaitDelay = time.Second
	data, err := probe.Output()
	cancel()
	if err != nil {
		return false, errors.New("desktop companion unavailable")
	}
	var contract localprotocol.DesktopContract
	// Both executables receive the same explicit package label (or "dev").
	// Number may instead infer a source pseudo-version from this Go module;
	// the independent desktop module does not share that inferred version.
	if json.Unmarshal(data, &contract) != nil || contract.ProtocolVersion != localprotocol.Version || contract.Build != version.Version || !sameFile(contract.Sidecar, executable) {
		return false, errors.New("desktop and CLI installation do not match")
	}
	directory, err := os.Getwd()
	if err != nil {
		return false, err
	}
	intent := localprotocol.Intent{Version: localprotocol.Version, Args: arguments, Directory: directory, Sidecar: executable, Stdin: stdin != nil, Environment: localprotocol.InvocationEnvironment(os.Environ())}
	encoded, encodeErr := json.Marshal(intent)
	if encodeErr != nil || len(encoded) > 48*1024 {
		return false, errors.New("desktop launch intent too large")
	}
	reply, err := localprotocol.Send(ctx, intent, stdin)
	if err == nil {
		return accepted(reply)
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
		return false, errors.New("desktop handoff failed")
	}
	requestRead, requestWrite, err := os.Pipe()
	if err != nil {
		return false, err
	}
	defer requestRead.Close()
	defer requestWrite.Close()
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		return false, err
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	input := stdin
	if input == nil {
		input, err = os.Open(os.DevNull)
		if err != nil {
			return false, err
		}
		defer input.Close()
	}
	cmd := exec.Command(desktop)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), localprotocol.HandoffEnvironment+"=1")
	cmd.ExtraFiles = []*os.File{requestRead, input, ackWrite}
	// Do not leave the detached GUI holding the caller's terminal/output pipes.
	// Diagnostics before acceptance are returned through the acknowledgement.
	if err = cmd.Start(); err != nil {
		return false, errors.New("desktop could not start")
	}
	requestRead.Close()
	ackWrite.Close()
	go func() { _ = cmd.Wait() }()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	written := make(chan error, 1)
	go func() { err := json.NewEncoder(requestWrite).Encode(intent); requestWrite.Close(); written <- err }()
	result := make(chan error, 1)
	go func() { result <- json.NewDecoder(io.LimitReader(ackRead, 4096)).Decode(&reply) }()
	for {
		select {
		case err = <-written:
			if err != nil {
				_ = cmd.Process.Kill()
				return false, errors.New("desktop startup request failed")
			}
			written = nil
		case err = <-result:
			if err != nil {
				_ = cmd.Process.Kill()
				return false, errors.New("desktop exited before acceptance")
			}
			return accepted(reply)
		case <-startup.Done():
			_ = cmd.Process.Kill()
			requestWrite.Close()
			ackRead.Close()
			return false, errors.New("desktop did not accept startup")
		}
	}

}

func accepted(reply localprotocol.Acceptance) (bool, error) {
	if reply.Accepted {
		return true, nil
	}
	return false, errors.New("desktop declined launch before acceptance")
}

// Only inspect the active installation beside this CLI, or the explicitly
// selected development pair. Never select an unrelated desktop from PATH.
func resolve(cli string) (string, error) {
	if development := os.Getenv("DIFFX_DESKTOP_SIDECAR"); development != "" && !sameFile(development, cli) {
		return "", errors.New("development sidecar does not match this CLI")
	}
	dir := filepath.Dir(cli)
	for _, candidate := range []string{filepath.Join(dir, "diffx-desktop"), filepath.Join(dir, "..", "MacOS", "diffx-desktop")} {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return filepath.Clean(candidate), nil
		}
	}
	return "", errors.New("matching desktop companion missing")
}

func sameFile(left, right string) bool {
	if strings.TrimSpace(left) == "" {
		return false
	}
	l, err := os.Stat(left)
	if err != nil {
		return false
	}
	r, err := os.Stat(right)
	return err == nil && os.SameFile(l, r)
}
