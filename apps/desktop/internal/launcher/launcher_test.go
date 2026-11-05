package launcher

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/connection"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

func TestSwitchDestinationExcludesNativeSecrets(t *testing.T) {
	secret := strings.Repeat("A", 43)
	invitation := connection.Invitation{Version: connection.Version, Address: "review.example:443", RunID: secret, Pin: secret, Secret: secret}.String()
	if got := destination([]string{"connect", invitation}, "/workspace", false); got != "review.example:443" || strings.Contains(got, secret) {
		t.Fatalf("unsafe destination %q", got)
	}
	if got := destination([]string{"connect", "ssh://user@devbox:2222"}, "/workspace", false); got != "SSH devbox:2222" {
		t.Fatalf("SSH destination %q", got)
	}
	if got := destination([]string{"--base", "HEAD", "checkout"}, "/workspace", false); got != "/workspace/checkout" {
		t.Fatalf("local destination %q", got)
	}
	if got := destination(nil, "/workspace", true); got != "Piped diff" {
		t.Fatalf("capture destination %q", got)
	}
}

func TestCaptureSpoolingRejectsOversizeBeforeStartingSidecar(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "oversize")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := input.Truncate(localprotocol.MaxCaptureBytes + 1); err != nil {
		t.Fatal(err)
	}
	l := New(&viewer{}, Options{Stdin: input})
	defer l.Close()
	if _, err := l.replayStdin(t.Context()); err == nil || !strings.Contains(err.Error(), "input limit") {
		t.Fatalf("oversize input accepted: %v", err)
	}
	if l.capture != "" {
		t.Fatal("rejected capture retained for replay")
	}
}

func TestConfigurationChangeRequiresConfirmationAndReachesSidecar(t *testing.T) {
	started := make(chan []string, 2)
	l := New(&viewer{}, Options{Args: []string{"/checkout"}, Directory: "/cwd", Environment: []string{"DIFFX_LOCAL_STATE=/old.db"}, SidecarPath: "/sidecar", Start: func(_ context.Context, o session.Options) (*Lease, error) {
		started <- slices.Clone(o.Environment)
		return &Lease{Ready: localprotocol.Readiness{Connection: connection.Target{Kind: "unix", Socket: "/socket"}}, Done: make(chan error), Close: func() error { return nil }}, nil
	}})
	defer l.Close()
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, l, "ready")
	<-started
	pending, err := l.offer([]string{"/checkout"}, nil, "/cwd", []string{"DIFFX_LOCAL_STATE=/new.db"})
	if err != nil || pending == nil || !l.Status().ConfirmSwitch {
		t.Fatalf("configuration change did not require confirmation: %v", err)
	}
	if err := l.Confirm(); err != nil {
		t.Fatal(err)
	}
	select {
	case environment := <-started:
		if !slices.Equal(environment, []string{"DIFFX_LOCAL_STATE=/new.db"}) {
			t.Fatal("sidecar inherited stale configuration")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement never started")
	}
}

type viewer struct {
	sync.Mutex
	origin    string
	healthErr error
	probes    int
}

func (v *viewer) SetConnection(target connection.Target) error {
	origin := target.URL
	v.Lock()
	defer v.Unlock()
	v.origin = origin
	return nil
}
func (v *viewer) Health(context.Context) error {
	v.Lock()
	defer v.Unlock()
	v.probes++
	return v.healthErr
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
	if !l.Status().ConfirmSwitch {
		t.Fatal("scope change skipped confirmation")
	}
	select {
	case <-closed:
		t.Fatal("confirmation closed current lease")
	default:
	}
	if err := l.Confirm(); err != nil {
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

func TestCancelledConnectionSwitchPreservesCurrentLeaseAndGeneration(t *testing.T) {
	starts := 0
	closed := make(chan struct{}, 2)
	l := New(&viewer{}, Options{Args: []string{"/checkout"}, Directory: "/cwd", SidecarPath: "/sidecar", Start: func(context.Context, session.Options) (*Lease, error) {
		starts++
		return &Lease{Ready: localprotocol.Readiness{Connection: connection.Target{Kind: "unix", Socket: "/socket"}}, Done: make(chan error), Close: func() error { closed <- struct{}{}; return nil }}, nil
	}})
	defer l.Close()
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	before := waitStatus(t, l, "ready")
	done := make(chan localprotocol.Acceptance, 1)
	go func() {
		done <- l.Request(t.Context(), localprotocol.Intent{Args: []string{"connect", "private invitation"}, Directory: "/other"}, nil)
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !l.Status().ConfirmSwitch {
		select {
		case <-deadline.C:
			t.Fatal("confirmation unavailable")
		case <-ticker.C:
		}
	}
	if err := l.Cancel(); err != nil {
		t.Fatal(err)
	}
	if reply := <-done; !reply.Accepted {
		t.Fatal("cancellation would trigger duplicate browser fallback")
	}
	if after := l.Status(); after != before {
		t.Fatalf("cancel changed session: before=%+v after=%+v", before, after)
	}
	if starts != 1 {
		t.Fatal("cancel started a connection")
	}
	select {
	case <-closed:
		t.Fatal("cancel closed current lease")
	default:
	}
}

func TestSameTargetOnlyFocusesAndDisconnectedSenderCancelsConfirmation(t *testing.T) {
	dir := t.TempDir()
	l := New(&viewer{}, Options{Args: []string{dir}, Directory: dir, SidecarPath: "/sidecar", Start: func(context.Context, session.Options) (*Lease, error) {
		return &Lease{Ready: localprotocol.Readiness{Input: localprotocol.Input{Kind: "checkout", Paths: []string{dir}}}, Done: make(chan error), Close: func() error { return nil }}, nil
	}})
	defer l.Close()
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	before := waitStatus(t, l, "ready")
	if reply := l.Request(t.Context(), localprotocol.Intent{Directory: dir}, nil); !reply.Accepted || l.Status() != before {
		t.Fatal("same canonical target restarted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if reply := l.Request(ctx, localprotocol.Intent{Args: []string{"connect", "other"}}, nil); reply.Accepted {
		t.Fatal("disconnected sender switched session")
	}
	if l.Status() != before {
		t.Fatal("sender cancellation changed current session")
	}
}

func TestRemoteTransientFailurePreservesGenerationAndRetriesAreBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		v := &viewer{}
		l := New(v, Options{Args: []string{"connect", "invitation"}, SidecarPath: "/sidecar", Start: func(context.Context, session.Options) (*Lease, error) {
			return &Lease{Ready: localprotocol.Readiness{Input: localprotocol.Input{Kind: "remote"}, Connection: connection.Target{Kind: "tcp", URL: "http://127.0.0.1:7981"}}, Done: make(chan error), Close: func() error { return nil }}, nil
		}})
		defer l.Close()
		if err := l.Start(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		before := l.Status()
		v.Lock()
		v.healthErr = errors.New("offline")
		v.Unlock()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if got := l.Status(); got.Phase != "reconnecting" || got.Generation != before.Generation {
			t.Fatalf("transient failure disposed workspace: %+v", got)
		}
		v.Lock()
		v.healthErr = nil
		v.Unlock()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if got := l.Status(); got.Phase != "ready" || got.Generation != before.Generation {
			t.Fatalf("recovery replaced workspace: %+v", got)
		}
		v.Lock()
		v.healthErr = errors.New("offline")
		v.Unlock()
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if got := l.Status(); got.Phase != "failed" || got.CanReplace {
			t.Fatalf("exhausted retry did not offer explicit retry: %+v", got)
		}
		v.Lock()
		probes := v.probes
		v.Unlock()
		if probes > 25 {
			t.Fatalf("unbounded reconnect attempts: %d", probes)
		}
	})
}

func TestRemoteForwardExitRecreatesOnlyConnectionWithoutChangingWorkspace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finished := make(chan error, 1)
		starts, closed := 0, 0
		l := New(&viewer{}, Options{Args: []string{"connect", "ssh://host"}, SidecarPath: "/sidecar", Start: func(_ context.Context, options session.Options) (*Lease, error) {
			if !slices.Equal(options.Args, []string{"connect", "ssh://host"}) {
				t.Error("reconnect replayed a different command")
			}
			starts++
			done := make(chan error, 1)
			if starts == 1 {
				done = finished
			}
			return &Lease{Ready: localprotocol.Readiness{Input: localprotocol.Input{Kind: "remote"}, Connection: connection.Target{Kind: "tcp", URL: "http://127.0.0.1:7981"}}, Done: done, Close: func() error { closed++; return nil }}, nil
		}})
		defer l.Close()
		if err := l.Start(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		before := l.Status()
		finished <- errors.New("ssh exited")
		synctest.Wait()
		if after := l.Status(); after.Phase != "ready" || after.Generation != before.Generation || starts != 2 || closed != 1 {
			t.Fatalf("forward recovery: %+v starts=%d closed=%d", after, starts, closed)
		}
	})
}
