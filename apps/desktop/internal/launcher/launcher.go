// Package launcher owns desktop launch intent, cancellation and sidecar leases.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/packages/connection"
	"golang.org/x/sys/unix"

	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type Status struct {
	Phase         string `json:"phase"`
	Generation    uint64 `json:"generation"`
	Error         string `json:"error"`
	Owned         bool   `json:"owned"`
	CanReplace    bool   `json:"canReplace"`
	ConfirmSwitch bool   `json:"confirmSwitch"`
	Destination   string `json:"destination"`
}

type Viewer interface {
	SetConnection(connection.Target) error
	Health(context.Context) error
	ClearViewer()
}
type Lease struct {
	Ready localprotocol.Readiness
	Done  <-chan error
	Close func() error
}
type Options struct {
	Args        []string
	Stdin       *os.File
	Stderr      io.Writer
	SidecarPath string
	Directory   string
	Environment []string
	Start       func(context.Context, session.Options) (*Lease, error)
}

type Launcher struct {
	transition sync.Mutex
	handoff    chan struct{}
	pending    *pendingIntent
	ready      localprotocol.Readiness
	mu         sync.Mutex
	status     Status
	closed     bool
	viewer     Viewer
	options    Options
	args       []string
	useStdin   bool
	capture    string
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func New(viewer Viewer, options Options) *Launcher {
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Start == nil {
		options.Start = func(ctx context.Context, o session.Options) (*Lease, error) {
			s, err := session.Start(ctx, o)
			if err != nil {
				return nil, err
			}
			return &Lease{Ready: s.Ready, Done: s.Done(), Close: s.Close}, nil
		}
	}
	args, desktop := Arguments(options.Args)
	if desktop {
		args = append([]string{"--attach"}, args...)
	}
	return &Launcher{handoff: make(chan struct{}, 1), viewer: viewer, options: options, args: args, useStdin: !desktop && !(len(args) > 0 && args[0] == "connect"), status: Status{Phase: "starting"}}
}

func Arguments(args []string) ([]string, bool) {
	var out []string
	desktop := false
	for i, arg := range args {
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg == "--desktop-launch" {
			desktop = true
		} else {
			out = append(out, arg)
		}
	}
	return out, desktop
}

func (l *Launcher) Status() Status { l.mu.Lock(); defer l.mu.Unlock(); return l.status }
func (l *Launcher) Start() error   { return l.begin(nil, false, false) }
func (l *Launcher) Retry() error   { return l.begin(nil, false, true) }

func (l *Launcher) Folder(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("folder must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("choose a directory")
	}
	l.mu.Lock()
	directory := l.options.Directory
	environment := slices.Clone(l.options.Environment)
	l.mu.Unlock()
	_, err = l.offer([]string{path}, nil, directory, environment)
	return err
}

type pendingIntent struct {
	args        []string
	input       *os.File
	directory   string
	environment []string
	decision    chan bool
}

// Request serializes native launches. Cancellation of a confirmation leaves the
// current lease, proxy, requests and review generation untouched.
func (l *Launcher) Request(ctx context.Context, intent localprotocol.Intent, input *os.File) localprotocol.Acceptance {
	select {
	case <-ctx.Done():
		return localprotocol.Acceptance{Error: "desktop launch cancelled"}
	case l.handoff <- struct{}{}:
	}
	defer func() { <-l.handoff }()
	if ctx.Err() != nil {
		return localprotocol.Acceptance{Error: "desktop launch cancelled"}
	}
	args, desktop := Arguments(intent.Args)
	if desktop {
		return localprotocol.Acceptance{Accepted: true}
	}
	pending, err := l.offer(args, input, intent.Directory, intent.Environment)
	if err != nil {
		return localprotocol.Acceptance{Error: "desktop launch unavailable"}
	}
	if pending == nil {
		return localprotocol.Acceptance{Accepted: true}
	}
	select {
	case <-ctx.Done():
		l.mu.Lock()
		if l.pending == pending {
			l.pending = nil
			l.status.ConfirmSwitch = false
			l.status.Destination = ""
			if pending.input != nil {
				pending.input.Close()
			}
		}
		l.mu.Unlock()
		return localprotocol.Acceptance{Error: "desktop launch cancelled"}
	case <-pending.decision:
		// Both explicit acceptance and cancellation are handled by the GUI. Neither
		// may trigger a second browser client in the invoking CLI.
		return localprotocol.Acceptance{Accepted: true}
	}
}

func (l *Launcher) offer(args []string, input *os.File, directory string, environment []string) (*pendingIntent, error) {
	l.mu.Lock()
	if l.closed || l.pending != nil {
		l.mu.Unlock()
		return nil, errors.New("desktop launch already pending")
	}
	if slices.Equal(environment, l.options.Environment) && l.sameTarget(args, directory, input) {
		l.mu.Unlock()
		return nil, nil
	}
	var duplicate *os.File
	if input != nil {
		fd, err := unix.Dup(int(input.Fd()))
		if err != nil {
			l.mu.Unlock()
			return nil, err
		}
		unix.CloseOnExec(fd)
		duplicate = os.NewFile(uintptr(fd), "desktop-intent-stdin")
	}
	pending := &pendingIntent{args: slices.Clone(args), input: duplicate, directory: directory, environment: slices.Clone(environment), decision: make(chan bool, 1)}
	// Never interrupt a starting or open session without an explicit decision.
	confirm := l.cancel != nil && (l.status.Phase == "starting" || l.status.Phase == "ready" || l.status.Phase == "reconnecting")
	l.pending = pending
	l.status.ConfirmSwitch = confirm
	if confirm {
		l.status.Destination = destination(args, directory, input != nil)
	}
	l.mu.Unlock()
	if !confirm {
		return pending, l.Confirm()
	}
	return pending, nil
}

// Destination is display-only. Never project invitation payloads, credentials
// or arbitrary command options into the bridge.
func destination(args []string, directory string, capture bool) string {
	if len(args) > 0 && args[0] == "connect" {
		for _, arg := range args[1:] {
			if invitation, err := connection.Parse(arg); err == nil {
				return invitation.Address
			}
			if parsed, err := url.Parse(arg); err == nil && parsed.Scheme == "ssh" && parsed.Host != "" {
				return "SSH " + parsed.Host
			}
		}
		return "Remote connection"
	}
	if capture {
		return "Piped diff"
	}
	selected := directory
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--path=") {
			selected = strings.TrimPrefix(arg, "--path=")
			break
		}
		if arg == "--path" && i+1 < len(args) {
			selected = args[i+1]
			break
		}
		if arg == "--" && i+1 < len(args) {
			selected = args[i+1]
			break
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && !slices.Contains([]string{"--attach", "--replace", "--reset-state", "--debug", "--no-browser", "--scan-worktrees"}, arg) {
				i++
			}
			continue
		}
		selected = arg
		break
	}
	if !filepath.IsAbs(selected) {
		selected = filepath.Join(directory, selected)
	}
	return filepath.Clean(selected)
}

func (l *Launcher) sameTarget(args []string, directory string, input *os.File) bool {
	if input != nil || l.cancel == nil || (l.status.Phase != "ready" && l.status.Phase != "starting" && l.status.Phase != "reconnecting") {
		return false
	}
	if slices.Equal(args, l.args) && (directory == l.options.Directory || len(args) > 0 && args[0] == "connect") {
		return true
	}
	if len(args) == 0 || len(args) == 1 && !strings.HasPrefix(args[0], "-") && args[0] != "connect" {
		path := directory
		if len(args) == 1 {
			path = args[0]
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
		}
		canonical, err := filepath.EvalSymlinks(path)
		return err == nil && l.ready.Input.Kind == "checkout" && len(l.ready.Input.Paths) == 1 && l.ready.Input.Paths[0] == canonical
	}
	if len(args) > 1 && args[0] == "connect" && l.ready.Input.Kind == "remote" {
		invitation, err := connection.Parse(args[1])
		return err == nil && l.ready.Connection.Kind == "tls" && invitation.RunID == l.ready.Connection.RunID && invitation.Pin == l.ready.Connection.Pin
	}
	return false
}

func (l *Launcher) Cancel() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending == nil {
		return errors.New("no pending desktop launch")
	}
	pending := l.pending
	l.pending = nil
	l.status.ConfirmSwitch = false
	l.status.Destination = ""
	if pending.input != nil {
		pending.input.Close()
	}
	pending.decision <- false
	return nil
}

func (l *Launcher) Confirm() error {
	l.mu.Lock()
	pending := l.pending
	if pending == nil {
		l.mu.Unlock()
		return errors.New("no pending desktop launch")
	}
	l.pending = nil
	l.status.ConfirmSwitch = false
	l.status.Destination = ""
	l.mu.Unlock()
	// begin joins the old session before changing native input ownership.
	err := l.beginIntent(pending)
	pending.decision <- err == nil
	return err
}

func (l *Launcher) beginIntent(pending *pendingIntent) error {
	l.transition.Lock()
	defer l.transition.Unlock()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		if pending.input != nil {
			pending.input.Close()
		}
		return errors.New("desktop is closing")
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()
	l.viewer.ClearViewer()
	l.wg.Wait()
	if l.options.Stdin != nil && l.options.Stdin != os.Stdin {
		l.options.Stdin.Close()
	}
	if l.capture != "" {
		_ = os.Remove(l.capture)
		l.capture = ""
	}
	l.mu.Lock()
	l.options.Stdin = pending.input
	l.options.Directory = pending.directory
	l.options.Environment = pending.environment
	l.useStdin = pending.input != nil && !(len(pending.args) > 0 && pending.args[0] == "connect")
	l.args = slices.Clone(pending.args)
	l.mu.Unlock()
	return l.beginLocked(nil, false, false)
}

// Only this distinct operation adds replacement authority. Retry and folder
// selection cannot replace another scope, including after a failed replacement.
func (l *Launcher) Replace() error { return l.begin(nil, true, true) }

func (l *Launcher) begin(args []string, replace, retry bool) error {
	l.transition.Lock()
	defer l.transition.Unlock()
	return l.beginLocked(args, replace, retry)
}

func (l *Launcher) beginLocked(args []string, replace, retry bool) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("desktop is closing")
	}
	if replace && !l.status.CanReplace {
		l.mu.Unlock()
		return errors.New("replacement requires a failed launch")
	}
	if retry && l.status.Phase != "failed" && l.status.Phase != "folder" {
		l.mu.Unlock()
		return errors.New("no failed launch to retry")
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()
	l.viewer.ClearViewer()
	l.wg.Wait()
	l.viewer.ClearViewer()
	l.mu.Lock()
	if args != nil {
		l.args, l.useStdin = slices.Clone(args), false
	}
	if replace {
		l.args = slices.DeleteFunc(l.args, func(arg string) bool { return arg == "--attach" })
		l.args = append([]string{"--replace"}, l.args...)
	}
	launchArgs := slices.Clone(l.args)
	// Replacement authority is consumed by one attempt, including CLI flags.
	l.args = consumeReplacement(l.args)
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.status = Status{Phase: "starting", Generation: l.status.Generation + 1}
	l.mu.Unlock()
	l.wg.Add(1)
	go func() { defer l.wg.Done(); l.run(ctx, launchArgs) }()
	return nil
}

func consumeReplacement(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
		if arg != "--replace" && arg != "-replace" && !strings.HasPrefix(arg, "--replace=") && !strings.HasPrefix(arg, "-replace=") {
			out = append(out, arg)
		}
	}
	return out
}

func (l *Launcher) run(ctx context.Context, args []string) {
	path := l.options.SidecarPath
	var err error
	if path == "" {
		path = os.Getenv("DIFFX_DESKTOP_SIDECAR")
		if path == "" {
			var executable string
			executable, err = os.Executable()
			if err == nil {
				path, err = session.ResolveSidecar(executable)
			}
		}
	}
	var input *os.File
	if err == nil && l.useStdin {
		input, err = l.replayStdin(ctx)
	}
	if input != nil && input != l.options.Stdin {
		defer input.Close()
	}
	var lease *Lease
	if err == nil {
		lease, err = l.options.Start(ctx, session.Options{Args: args, Stdin: input, Stderr: l.options.Stderr, SidecarPath: path, Directory: l.options.Directory, Environment: l.options.Environment})
	}
	if err != nil {
		if ctx.Err() == nil {
			l.mu.Lock()
			l.status.Phase, l.status.Error = "failed", err.Error()
			l.status.CanReplace = !slices.Contains(args, "--attach") && !(len(args) > 0 && args[0] == "connect")
			if slices.Contains(args, "--attach") {
				l.status.Phase = "folder"
			}
			l.mu.Unlock()
		}
		return
	}
	defer func() {
		if err := lease.Close(); err != nil {
			fmt.Fprintln(l.options.Stderr, "Close local session:", err)
		}
	}()
	if ctx.Err() != nil {
		return
	}
	target := lease.Ready.Connection
	if target.Kind == "" {
		target = connection.Target{Kind: "unix", Socket: lease.Ready.Socket}
	}
	if err = l.viewer.SetConnection(target); err != nil {
		l.mu.Lock()
		l.status.Phase, l.status.Error = "failed", err.Error()
		l.mu.Unlock()
		return
	}
	defer l.viewer.ClearViewer()
	l.mu.Lock()
	l.status.Phase, l.status.Owned = "ready", lease.Ready.Owned
	l.ready = lease.Ready
	l.mu.Unlock()
	if lease.Ready.Input.Kind == "remote" {
		lease = l.monitor(ctx, lease, args)
		return
	}
	select {
	case <-ctx.Done():
	case err := <-lease.Done:
		l.viewer.ClearViewer()
		if ctx.Err() == nil {
			if err == nil {
				err = errors.New("local session ended")
			}
			l.mu.Lock()
			l.status.Phase, l.status.Error, l.status.CanReplace = "failed", err.Error(), false
			l.mu.Unlock()
		}
	}
}

// A transient remote failure preserves the mounted workspace. Probe only reads,
// cap retry delay at 15 seconds and stop after two minutes. Explicit Retry
// recreates the native session, including an SSH forward if needed.
func (l *Launcher) monitor(ctx context.Context, lease *Lease, args []string) *Lease {
	delay := 3 * time.Second
	var lost time.Time
	alive := true
	for {
		if !lost.IsZero() {
			delay = min(delay, max(time.Duration(0), time.Until(lost.Add(2*time.Minute))))
		}
		timer := time.NewTimer(delay)
		done := lease.Done
		if !alive {
			done = nil
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return lease
		case <-done:
			timer.Stop()
			alive = false
			if lost.IsZero() {
				lost = time.Now()
				delay = time.Second
			}
			l.mu.Lock()
			l.status.Phase = "reconnecting"
			l.status.Error = "Connection lost. Reconnecting…"
			l.mu.Unlock()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return lease
		}
		if !lost.IsZero() && time.Since(lost) >= 2*time.Minute {
			l.mu.Lock()
			l.status.Phase = "failed"
			l.status.Error = "Connection unavailable. Retry to reconnect."
			l.mu.Unlock()
			return lease
		}
		var err error
		if alive {
			budget := 10 * time.Second
			if !lost.IsZero() {
				budget = min(budget, time.Until(lost.Add(2*time.Minute)))
			}
			probe, cancel := context.WithTimeout(ctx, budget)
			err = l.viewer.Health(probe)
			cancel()
		} else {
			// Recreate only the native connection/SSH forward. Remote leases never
			// own a server and this command performs no application mutation replay.
			attempt, cancel := context.WithCancel(ctx)
			timeout := time.AfterFunc(min(30*time.Second, 2*time.Minute-time.Since(lost)), cancel)
			candidate, startErr := l.options.Start(attempt, session.Options{Args: args, Directory: l.options.Directory, Stderr: l.options.Stderr, SidecarPath: l.options.SidecarPath, Environment: l.options.Environment})
			timeout.Stop()
			if startErr == nil && attempt.Err() != nil {
				_ = candidate.Close()
				startErr = attempt.Err()
			}
			if startErr != nil {
				cancel()
			} else {
				closeLease := candidate.Close
				candidate.Close = func() error { cancel(); return closeLease() }
			}
			err = startErr
			if err == nil {
				if ctx.Err() != nil {
					_ = candidate.Close()
					return lease
				}
				err = l.viewer.SetConnection(candidate.Ready.Connection)
				if err != nil {
					_ = candidate.Close()
				} else {
					_ = lease.Close()
					lease = candidate
					alive = true
					l.mu.Lock()
					l.ready = candidate.Ready
					l.mu.Unlock()
				}
			}
		}
		if ctx.Err() != nil {
			return lease
		}
		l.mu.Lock()
		if err == nil {
			lost = time.Time{}
			delay = 3 * time.Second
			l.status.Phase = "ready"
			l.status.Error = ""
		} else {
			if lost.IsZero() {
				lost = time.Now()
				delay = time.Second
			}
			if time.Since(lost) >= 2*time.Minute {
				l.status.Phase = "failed"
				l.status.Error = "Connection unavailable. Retry to reconnect."
				l.mu.Unlock()
				return lease
			}
			l.status.Phase = "reconnecting"
			l.status.Error = "Connection lost. Reconnecting…"
			delay = min(delay*2, 15*time.Second)
		}
		l.mu.Unlock()

	}
}

func (l *Launcher) replayStdin(ctx context.Context) (*os.File, error) {
	input := l.options.Stdin
	if input == nil {
		return nil, nil
	}
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return input, nil
	}
	if l.capture == "" {
		readable, closeInput, err := localprotocol.CaptureInput(ctx, input)
		if err != nil {
			return nil, err
		}
		defer closeInput()
		file, err := os.CreateTemp("", "diffx-desktop-stdin-*")
		if err != nil {
			return nil, err
		}
		n, copyErr := io.Copy(file, io.LimitReader(readable, localprotocol.MaxCaptureBytes+1))
		if n > localprotocol.MaxCaptureBytes {
			copyErr = errors.New("piped diff exceeds 16 MiB input limit")
		}
		if err := errors.Join(copyErr, file.Close()); err != nil {
			os.Remove(file.Name())
			return nil, fmt.Errorf("read piped diff: %w", err)
		}
		l.capture = file.Name()
	}
	return os.Open(l.capture)
}

func (l *Launcher) Close() {
	l.transition.Lock()
	defer l.transition.Unlock()
	l.mu.Lock()
	l.closed = true
	if l.pending != nil {
		if l.pending.input != nil {
			l.pending.input.Close()
		}
		l.pending.decision <- false
		l.pending = nil
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()
	l.viewer.ClearViewer()
	l.wg.Wait()
	if l.capture != "" {
		_ = os.Remove(l.capture)
		l.capture = ""
	}
	if l.options.Stdin != nil && l.options.Stdin != os.Stdin {
		l.options.Stdin.Close()
	}
}
