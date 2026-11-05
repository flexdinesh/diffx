// Package launcher owns desktop launch intent, cancellation and sidecar leases.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type Status struct {
	Phase      string `json:"phase"`
	Generation uint64 `json:"generation"`
	Error      string `json:"error"`
	Owned      bool   `json:"owned"`
	CanReplace bool   `json:"canReplace"`
}

type Viewer interface {
	SetViewer(string) error
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
	Start       func(context.Context, session.Options) (*Lease, error)
}

type Launcher struct {
	transition sync.Mutex
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
	return &Launcher{viewer: viewer, options: options, args: args, useStdin: !desktop, status: Status{Phase: "starting"}}
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
	return l.begin([]string{path}, false, false)
}

// Only this distinct operation adds replacement authority. Retry and folder
// selection cannot replace another scope, including after a failed replacement.
func (l *Launcher) Replace() error { return l.begin(nil, true, true) }

func (l *Launcher) begin(args []string, replace, retry bool) error {
	l.transition.Lock()
	defer l.transition.Unlock()
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
		lease, err = l.options.Start(ctx, session.Options{Args: args, Stdin: input, Stderr: l.options.Stderr, SidecarPath: path})
	}
	if err != nil {
		if ctx.Err() == nil {
			l.mu.Lock()
			l.status.Phase, l.status.Error = "failed", err.Error()
			l.status.CanReplace = !slices.Contains(args, "--attach")
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
	if err = l.viewer.SetViewer(lease.Ready.ViewerURL); err != nil {
		l.mu.Lock()
		l.status.Phase, l.status.Error = "failed", err.Error()
		l.mu.Unlock()
		return
	}
	defer l.viewer.ClearViewer()
	l.mu.Lock()
	l.status.Phase, l.status.Owned = "ready", lease.Ready.Owned
	l.mu.Unlock()
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
		file, err := os.CreateTemp("", "diffx-desktop-stdin-*")
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { _ = input.Close() })
		_, copyErr := io.Copy(file, input)
		stop()
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
}
