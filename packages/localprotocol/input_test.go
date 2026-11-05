package localprotocol

import (
	"context"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInheritedPipeCaptureIsPollableAndCancellationReleasesIt(t *testing.T) {
	fds := make([]int, 2)
	if err := unix.Pipe(fds); err != nil {
		t.Fatal(err)
	}
	input := os.NewFile(uintptr(fds[0]), "inherited-blocking-stdin")
	defer input.Close()
	writer := os.NewFile(uintptr(fds[1]), "writer")
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	readable, cleanup, err := CaptureInput(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, err := readable.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var pollable bool
	if err := raw.Control(func(fd uintptr) {
		flags, e := unix.FcntlInt(fd, unix.F_GETFL, 0)
		pollable = e == nil && flags&unix.O_NONBLOCK != 0
	}); err != nil || !pollable {
		t.Fatal("capture descriptor is not pollable", err)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, readable); done <- err }()
	// More than pipe capacity proves the reader has started while leaving the
	// write end open, reproducing an unfinished producer without sleeps.
	if _, err := writer.Write(make([]byte, 128<<10)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("open producer reported EOF")
		}
	case <-time.After(time.Second):
		t.Fatal("capture read survived cancellation")
	}
}

func TestInvocationEnvironmentReplacesAndClearsOldConfiguration(t *testing.T) {
	base := []string{"DIFFX_LOCAL_STATE=/old.db", "DIFFX_LOCAL_PORT=7981", "GIT_CONFIG_GLOBAL=/old.git", "PATH=/old/bin", "DISPLAY=:1", "DIFFX_SERVER_DATABASE_URL=private"}
	invocation := InvocationEnvironment([]string{"DIFFX_LOCAL_STATE=/new.db", "PATH=/new/bin", "DIFFX_SERVER_DATABASE_URL=other"})
	got := SessionEnvironment(base, invocation)
	if !slices.Contains(got, "DIFFX_LOCAL_STATE=/new.db") || !slices.Contains(got, "PATH=/new/bin") || !slices.Contains(got, "DISPLAY=:1") || slices.Contains(got, "DIFFX_LOCAL_PORT=7981") || slices.Contains(got, "GIT_CONFIG_GLOBAL=/old.git") || slices.Contains(invocation, "DIFFX_SERVER_DATABASE_URL=other") {
		t.Fatalf("invocation settings lost or stale: %v", got)
	}
}
