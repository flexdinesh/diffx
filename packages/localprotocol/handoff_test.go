package localprotocol

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func isolatedInbox(t *testing.T) {
	t.Helper()
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	dir, err := RuntimeDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
}

func TestHandoffCarriesNativeIntentAndStdinWithoutConsumingOnCancel(t *testing.T) {
	isolatedInbox(t)
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err = input.WriteString("private patch"); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	intent := Intent{Version: Version, Args: []string{"connect", "private invitation"}, Directory: t.TempDir(), Sidecar: "/matching/diffx"}
	inbox, err := Listen(func(_ context.Context, got Intent, stdin *os.File) Acceptance {
		if !slices.Equal(got.Args, intent.Args) || got.Directory != intent.Directory || got.Sidecar != intent.Sidecar || stdin == nil {
			t.Error("native intent lost")
		}
		// Explicit cancellation is handled without reading the inherited input.
		return Acceptance{Accepted: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	if second, err := Listen(nil); !errors.Is(err, ErrRunning) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("singleton lock: %v", err)
	}
	dir, err := RuntimeDirectory()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "launch.sock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket authority: %v %v", info, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	reply, err := Send(ctx, intent, input)
	if err != nil || !reply.Accepted {
		t.Fatalf("handoff: %+v %v", reply, err)
	}
	data, err := io.ReadAll(input)
	if err != nil || string(data) != "private patch" {
		t.Fatalf("cancel consumed stdin: %q %v", data, err)
	}
}

func TestHandoffCancellationDisposesPendingRequestAndSingletonCanRestart(t *testing.T) {
	isolatedInbox(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	inbox, err := Listen(func(ctx context.Context, _ Intent, _ *os.File) Acceptance {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return Acceptance{}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := Send(ctx, Intent{Version: Version, Directory: "/cwd", Sidecar: "/cli"}, nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled sender accepted")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("pending native request survived disconnect")
	}
	inbox.Close()
	restarted, err := Listen(func(context.Context, Intent, *os.File) Acceptance { return Acceptance{Accepted: true} })
	if err != nil {
		t.Fatal(err)
	}
	restarted.Close()
}

func TestRuntimeRejectsSymlinkAndSharedDirectory(t *testing.T) {
	isolatedInbox(t)
	dir, err := RuntimeDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = RuntimeDirectory(); err == nil {
		t.Fatal("shared runtime directory admitted")
	}
	if err = os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err = RuntimeDirectory(); err == nil {
		t.Fatal("symlink runtime directory admitted")
	}
}
