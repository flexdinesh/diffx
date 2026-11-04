package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/localserver"
	"github.com/flexdinesh/diffx/packages/localprotocol"
	"golang.org/x/sys/unix"
)

func TestDesktopSessionProcess(t *testing.T) {
	if os.Getenv("DIFFX_DESKTOP_TEST_CHILD") != "1" {
		return
	}
	index := slices.Index(os.Args, "--")
	if index < 0 {
		os.Exit(2)
	}
	os.Args = append([]string{"diffx"}, os.Args[index+1:]...)
	main()
	os.Exit(0)
}

type desktopChild struct {
	command  *exec.Cmd
	lifetime *os.File
	output   *bufio.Reader
	stderr   bytes.Buffer
	done     chan error
	stopped  chan struct{}
}

func desktopEnvironment(t *testing.T) string {
	t.Helper()
	// Keep the Unix socket below the platform path limit.
	runtime, err := os.MkdirTemp("", "dx-session-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	t.Setenv("DIFFX_RUNTIME_DIR", runtime)
	t.Setenv("DIFFX_CONFIG_PATH", filepath.Join(runtime, "config.json"))
	return runtime
}

func startDesktopChild(t *testing.T, stdin io.Reader, args ...string) *desktopChild {
	t.Helper()
	return startDesktopChildEnv(t, stdin, nil, args...)
}

func startDesktopChildEnv(t *testing.T, stdin io.Reader, environment []string, args ...string) *desktopChild {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestDesktopSessionProcess$", "--", localprotocol.Command, "--parent-fd=3"}, args...)...)
	command.Env = append(os.Environ(), "DIFFX_DESKTOP_TEST_CHILD=1")
	command.Env = append(command.Env, environment...)
	command.ExtraFiles = []*os.File{reader}
	command.Stdin = stdin
	child := &desktopChild{command: command, lifetime: writer, done: make(chan error, 1), stopped: make(chan struct{})}
	command.Stderr = &child.stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.output = bufio.NewReader(stdout)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	go func() { defer close(child.stopped); child.done <- command.Wait() }()
	t.Cleanup(func() {
		_ = writer.Close()
		_ = command.Process.Kill()
		<-child.stopped
	})
	return child
}

func (child *desktopChild) ready(t *testing.T) localprotocol.Readiness {
	t.Helper()
	type result struct {
		value localprotocol.Readiness
		err   error
	}
	read := make(chan result, 1)
	go func() {
		line, err := child.output.ReadBytes('\n')
		var ready localprotocol.Readiness
		if err == nil {
			err = json.Unmarshal(line, &ready)
		}
		read <- result{ready, err}
	}()
	select {
	case result := <-read:
		if result.err != nil {
			t.Fatalf("readiness: %v", result.err)
		}
		if result.value.ProtocolVersion != localprotocol.Version || result.value.ScopeID == "" || result.value.InstanceID == "" || result.value.Socket == "" || result.value.ViewerURL == "" {
			t.Fatalf("incomplete readiness: %+v", result.value)
		}
		return result.value
	case <-time.After(10 * time.Second):
		t.Fatal("readiness timed out")
		return localprotocol.Readiness{}
	}
}

func (child *desktopChild) wait(t *testing.T, failure string) {
	t.Helper()
	select {
	case err := <-child.done:
		if failure == "" && err != nil {
			t.Fatalf("child: %v: %s", err, child.stderr.String())
		}
		if failure != "" && (err == nil || !strings.Contains(child.stderr.String(), failure)) {
			t.Fatalf("want %q, got %v: %s", failure, err, child.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not stop")
	}
}

func TestDesktopParentDeathReleasesOwnershipAndCapture(t *testing.T) {
	runtime := desktopEnvironment(t)
	root := cliRepository(t)
	child := startDesktopChild(t, nil, root, "--port=0", "--state=memory")
	ready := child.ready(t)
	if !ready.Owned || ready.Input.Kind != "checkout" {
		t.Fatalf("owned checkout: %+v", ready)
	}
	_ = child.lifetime.Close() // EOF is the kernel signal on parent exit/crash.
	child.wait(t, "")
	if status, err := (&localserver.Client{RuntimeDirectory: runtime}).Status(t.Context()); err != nil || status.State != "stopped" {
		t.Fatalf("owner survived EOF: %+v %v", status, err)
	}
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"
	capture := startDesktopChild(t, strings.NewReader(patch), "--port=0", "--state=memory")
	selected := capture.ready(t)
	if !selected.Owned || selected.Input.Kind != "capture" || selected.Input.ContextID == "" {
		t.Fatalf("capture readiness: %+v", selected)
	}
	client := &localserver.Client{RuntimeDirectory: runtime}
	response, err := client.HTTPClient().Get("http://localhost/api/v2/contexts/" + selected.Input.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("capture unavailable: %d", response.StatusCode)
	}
	_ = capture.lifetime.Close()
	capture.wait(t, "")
}

func TestDesktopAttachmentScopeConflictAndReplacement(t *testing.T) {
	runtime := desktopEnvironment(t)
	root := cliRepository(t)
	owner := startDesktopChild(t, nil, root, "--port=0", "--state=memory")
	first := owner.ready(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	attached := startDesktopChild(t, nil, alias, "--port=0", "--state=memory")
	second := attached.ready(t)
	if second.Owned || second.InstanceID != first.InstanceID || second.ScopeID != first.ScopeID {
		t.Fatalf("same scope did not attach: %+v %+v", first, second)
	}
	_ = attached.lifetime.Close()
	attached.wait(t, "")
	current := startDesktopChildEnv(t, nil, []string{"DIFFX_LOCAL_PORT=invalid"}, "--attach")
	if ready := current.ready(t); ready.Owned || ready.InstanceID != first.InstanceID {
		t.Fatalf("explicit attach: %+v", ready)
	}
	_ = current.lifetime.Close()
	current.wait(t, "")
	client := &localserver.Client{RuntimeDirectory: runtime}
	if status, err := client.Status(t.Context()); err != nil || status.InstanceID != first.InstanceID {
		t.Fatalf("attach quit stopped owner: %+v %v", status, err)
	}
	conflict := startDesktopChild(t, nil, cliRepository(t), "--port=0", "--state=memory")
	conflict.wait(t, "scope conflict")
	configuration := startDesktopChild(t, nil, root, "--port=0", "--state=memory", "--retention-days=8")
	configuration.wait(t, "configuration conflict")
	replace := startDesktopChild(t, nil, cliRepository(t), "--port=0", "--state=memory", "--replace")
	third := replace.ready(t)
	if !third.Owned || third.InstanceID == first.InstanceID {
		t.Fatalf("replacement: %+v", third)
	}
	owner.wait(t, "")
	_ = replace.lifetime.Close()
	replace.wait(t, "")
}

func TestDesktopParentDeathInterruptsCaptureRead(t *testing.T) {
	desktopEnvironment(t)
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	child := startDesktopChild(t, input, "--port=0", "--state=memory")
	_ = child.lifetime.Close()
	child.wait(t, "")
}

func TestDesktopCaptureAttachmentUsesBytesAndOrigin(t *testing.T) {
	desktopEnvironment(t)
	t.Chdir(t.TempDir())
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"
	owner := startDesktopChild(t, strings.NewReader(patch), "--port=0", "--state=memory")
	first := owner.ready(t)
	attached := startDesktopChild(t, strings.NewReader(patch), "--port=0", "--state=memory")
	second := attached.ready(t)
	if second.Owned || second.Input.ContextID != first.Input.ContextID || second.InstanceID != first.InstanceID {
		t.Fatalf("capture did not attach: %+v %+v", first, second)
	}
	_ = attached.lifetime.Close()
	attached.wait(t, "")
	different := startDesktopChild(t, strings.NewReader(strings.ReplaceAll(patch, "+new", "+changed")), "--port=0", "--state=memory")
	different.wait(t, "scope conflict")
	t.Chdir(t.TempDir())
	origin := startDesktopChild(t, strings.NewReader(patch), "--port=0", "--state=memory")
	origin.wait(t, "scope conflict")
	_ = owner.lifetime.Close()
	owner.wait(t, "")
}

type startupNotice chan struct{}

func (notice startupNotice) Write(raw []byte) (int, error) {
	select {
	case notice <- struct{}{}:
	default:
	}
	return len(raw), nil
}

func TestDesktopAttachesToNormalCLIWithoutStoppingIt(t *testing.T) {
	runtime := desktopEnvironment(t)
	root := cliRepository(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	startup := make(startupNotice, 1)
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		done <- runLocal(ctx, []string{root, "--port=0", "--state=memory", "--no-browser"}, nil, startup, io.Discard)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("foreground did not stop")
		}
	})
	select {
	case <-startup:
	case err := <-done:
		t.Fatalf("foreground: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("foreground readiness timeout")
	}
	client := &localserver.Client{RuntimeDirectory: runtime}
	status, err := client.Status(t.Context())
	if err != nil || status.Session == nil {
		t.Fatalf("missing foreground session metadata: %+v %v", status, err)
	}
	attached := startDesktopChild(t, nil, root, "--port=0", "--state=memory")
	ready := attached.ready(t)
	if ready.Owned || ready.InstanceID != status.InstanceID || ready.ScopeID != status.Session.ScopeID {
		t.Fatalf("foreground attachment: %+v", ready)
	}
	_ = attached.lifetime.Close()
	attached.wait(t, "")
	if current, err := client.Status(t.Context()); err != nil || current.InstanceID != status.InstanceID || current.State != "running" {
		t.Fatalf("native quit stopped foreground: %+v %v", current, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("foreground shutdown timeout")
	}
}

type brokenReadiness struct{}

func (brokenReadiness) Write([]byte) (int, error) { return 0, errors.New("readiness broken") }

func TestDesktopReadinessFailureCleansOwnedServer(t *testing.T) {
	runtime := desktopEnvironment(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	// Match exec.ExtraFiles: transfer a raw descriptor with no Go File owner.
	fd, err := unix.Dup(int(reader.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	err = runDesktopSession(t.Context(), []string{cliRepository(t), "--port=0", "--state=memory", "--parent-fd=" + fmt.Sprint(fd)}, nil, brokenReadiness{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "readiness broken") {
		t.Fatalf("readiness failure: %v", err)
	}
	if status, err := (&localserver.Client{RuntimeDirectory: runtime}).Status(t.Context()); err != nil || status.State != "stopped" {
		t.Fatalf("readiness leaked owner: %+v %v", status, err)
	}
}

func TestDesktopOptionsRejectAmbiguousAttachment(t *testing.T) {
	for _, args := range [][]string{{"--attach", "."}, {"--attach", "--replace"}, {"--attach", "--base=HEAD"}, {"--attach", "--scan-worktrees"}, {"--attach", "--reset-state"}, {"--attach", "--state=memory"}} {
		if _, err := parseOptionsSession(args, io.Discard, true, true); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := runDesktopSession(context.Background(), nil, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "parent-fd") {
		t.Fatalf("missing parent: %v", err)
	}
}

func TestDesktopCapturePreservesBorrowedStdinFlags(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	connection, err := reader.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flagErr error
	if err := connection.Control(func(fd uintptr) { flagErr = unix.SetNonblock(int(fd), false) }); err != nil || flagErr != nil {
		t.Fatalf("prepare inherited blocking stdin: %v %v", err, flagErr)
	}
	_, cleanup, err := sessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	var flags int
	if err := connection.Control(func(fd uintptr) { flags, flagErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil || flagErr != nil {
		t.Fatalf("borrowed stdin closed: %v %v", err, flagErr)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Fatal("capture changed inherited stdin flags")
	}
	file := testInputFile(t, []byte("capture"))
	selected, cleanup, err := sessionInput(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if selected != file {
		t.Fatal("ordinary file stdin should remain untouched")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("borrowed file stdin closed: %v", err)
	}
}
