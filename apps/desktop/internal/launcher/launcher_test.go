package launcher

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type viewer struct {
	sync.Mutex
	origin string
}

func (v *viewer) SetViewer(origin string) error {
	v.Lock()
	defer v.Unlock()
	v.origin = origin
	return nil
}
func (v *viewer) ClearViewer() { v.Lock(); defer v.Unlock(); v.origin = "" }

func waitStatus(t *testing.T, l *Launcher, phase string) Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		status := l.Status()
		if status.Phase == phase {
			return status
		}
		select {
		case <-ctx.Done():
			t.Fatalf("want %s: %+v", phase, status)
		case <-ticker.C:
		}
	}
}

func TestReplacementIsExplicitAndConsumedOnce(t *testing.T) {
	attempts := make(chan []string, 8)
	l := New(&viewer{}, Options{Args: []string{"/checkout"}, SidecarPath: "/sidecar", Start: func(_ context.Context, o session.Options) (*Lease, error) {
		attempts <- slices.Clone(o.Args)
		return nil, errors.New("another scope owns the database")
	}})
	defer l.Close()
	if err := l.Replace(); err == nil {
		t.Fatal("replacement admitted without failed launch")
	}
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if args := <-attempts; slices.Contains(args, "--replace") {
		t.Fatal("initial launch replaced automatically")
	}
	if !waitStatus(t, l, "failed").CanReplace {
		t.Fatal("explicit replacement unavailable after scope failure")
	}
	if err := l.Retry(); err != nil {
		t.Fatal(err)
	}
	if args := <-attempts; slices.Contains(args, "--replace") {
		t.Fatal("Retry replaced automatically")
	}
	waitStatus(t, l, "failed")
	if err := l.Replace(); err != nil {
		t.Fatal(err)
	}
	if args := <-attempts; !slices.Contains(args, "--replace") {
		t.Fatal("explicit authority not forwarded")
	}
	waitStatus(t, l, "failed")
	if err := l.Retry(); err != nil {
		t.Fatal(err)
	}
	if args := <-attempts; slices.Contains(args, "--replace") {
		t.Fatal("Retry reused replacement authority")
	}
}

func TestScopeChangeJoinsLeaseBeforePublishingNewViewer(t *testing.T) {
	closed := make(chan string, 2)
	starts := make(chan string, 2)
	l := New(&viewer{}, Options{Args: []string{"--desktop-launch"}, SidecarPath: "/sidecar", Start: func(ctx context.Context, o session.Options) (*Lease, error) {
		name := o.Args[0]
		starts <- name
		return &Lease{Ready: localprotocol.Readiness{ViewerURL: "http://127.0.0.1:1", Owned: false}, Done: make(chan error), Close: func() error { closed <- name; return nil }}, nil
	}})
	defer l.Close()
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	<-starts
	status := waitStatus(t, l, "ready")
	if status.Owned {
		t.Fatal("attachment transferred ownership")
	}
	dir := t.TempDir()
	if err := l.Folder(dir); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-closed:
		if name != "--attach" {
			t.Fatal(name)
		}
	default:
		t.Fatal("scope changed before old lease joined")
	}
	if name := <-starts; name != dir {
		t.Fatalf("folder invocation %q", name)
	}
	if next := waitStatus(t, l, "ready"); next.Generation <= status.Generation {
		t.Fatal("workspace generation did not change")
	}
	l.Close()
	select {
	case <-closed:
	default:
		t.Fatal("close did not join current lease")
	}
}

func TestCloseCancelsStartupAndReapsLateLease(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	l := New(&viewer{}, Options{SidecarPath: "/sidecar", Start: func(ctx context.Context, _ session.Options) (*Lease, error) {
		close(started)
		<-ctx.Done()
		return &Lease{Close: func() error { close(closed); return nil }}, nil
	}})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	<-started
	l.Close()
	select {
	case <-closed:
	default:
		t.Fatal("late sidecar lease leaked")
	}
	if err := l.Start(); err == nil {
		t.Fatal("closing desktop restarted")
	}
}

func TestPipedCaptureReplaysOnRetryAndIsRemoved(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err = input.WriteString("captured patch"); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	reads := make(chan string, 2)
	l := New(&viewer{}, Options{Stdin: input, SidecarPath: "/sidecar", Start: func(_ context.Context, o session.Options) (*Lease, error) {
		data, err := io.ReadAll(o.Stdin)
		if err != nil {
			return nil, err
		}
		reads <- string(data)
		return nil, errors.New("temporary failure")
	}})
	defer l.Close()
	if err = l.Start(); err != nil {
		t.Fatal(err)
	}
	if got := <-reads; got != "captured patch" {
		t.Fatal(got)
	}
	waitStatus(t, l, "failed")
	if err = l.Retry(); err != nil {
		t.Fatal(err)
	}
	if got := <-reads; got != "captured patch" {
		t.Fatal("stdin lost on Retry", got)
	}
	waitStatus(t, l, "failed")
	capture := l.capture
	l.Close()
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capture survived close", err)
	}
}

func TestDesktopLaunchDoesNotDiscoverLauncherDirectory(t *testing.T) {
	args, desktop := Arguments([]string{"--desktop-launch", "--port=0"})
	if !desktop || !slices.Equal(args, []string{"--port=0"}) {
		t.Fatalf("%v %v", args, desktop)
	}
	args, desktop = Arguments([]string{"--", "--desktop-launch"})
	if desktop || !slices.Equal(args, []string{"--", "--desktop-launch"}) {
		t.Fatal("path interpreted as flag")
	}
	l := New(&viewer{}, Options{SidecarPath: "/sidecar"})
	defer l.Close()
	if err := l.Folder(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("accepted missing folder")
	}
}

func TestReplacementConsumptionPreservesLiteralPaths(t *testing.T) {
	args := consumeReplacement([]string{"--replace=true", "--port=0", "--", "--replace"})
	if !slices.Equal(args, []string{"--port=0", "--", "--replace"}) {
		t.Fatalf("literal path consumed as authority: %v", args)
	}
}
