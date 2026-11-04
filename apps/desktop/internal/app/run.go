// Package app composes the native view with public local-session and REST
// contracts. All presentation changes are dispatched to the native UI thread.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/flexdinesh/diffx/apps/desktop/internal/preferences"
	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// Run must run on the process's main goroutine. The window exists before
// sidecar discovery, stdin capture, server startup, or the first API request.
func Run(args []string, stdin *os.File, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	if handled, err := HandleCommand(args, stderr); handled {
		return err
	}
	prefs, prefErr := preferences.Load()
	a := newApplication(prefs)
	a.stderr = stderr
	a.args, a.desktopLaunch = launchArguments(args)
	a.stdin = stdin
	if prefErr != nil {
		a.preferenceError = prefErr.Error()
	}
	mygo.App.SetName("Diffx Native")
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "Diffx Native", Width: 1280, Height: 800,
			MinWidth: 960, MinHeight: 640, Content: ui.View(a.view),
		})
		a.dispatch = a.window.Update
		a.chooseDirectory = func(done func(string, error)) {
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
					Parent: a.window, Title: "Choose checkout or workspace", Directory: true,
					ButtonLabel: "Open review",
				})
				path := ""
				if len(paths) > 0 {
					path = paths[0]
				}
				a.dispatch(func() { done(path, err) })
			}()
		}
		a.installMenu()
		if a.desktopLaunch {
			a.start(append([]string{"--attach"}, a.args...))
		} else {
			a.start(a.args)
		}
	})
	offClosed := mygo.App.OnWindowAllClosed(mygo.App.Quit)
	defer offClosed()
	offQuit := mygo.App.OnQuit(a.close)
	defer offQuit()
	err := mygo.App.Run()
	a.close()
	return errors.Join(err, a.shutdownError)
}

func launchArguments(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
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

// launch owns every sidecar invocation, including those whose UI delivery was
// queued when quit began. Closing never relies on another UI event being run.
type launch struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	session *localSession
	wg      sync.WaitGroup
}

type localSession struct {
	Ready  localprotocol.Readiness
	Client *goclient.ClientWithResponses
	Close  func() error
	Done   <-chan error
}

func startSidecar(ctx context.Context, options session.Options) (*localSession, error) {
	s, err := session.Start(ctx, options)
	if err != nil {
		return nil, err
	}
	return &localSession{Ready: s.Ready, Client: s.Client, Close: s.Close, Done: s.Done()}, nil
}

func resolveSidecar() (string, error) {
	if override := os.Getenv("DIFFX_NATIVE_SIDECAR"); override != "" {
		if !filepath.IsAbs(override) {
			return "", errors.New("DIFFX_NATIVE_SIDECAR must be an absolute executable path")
		}
		return override, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return session.ResolveSidecar(executable)
}

func (l *launch) close() error {
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	s := l.session
	l.mu.Unlock()
	var err error
	if s != nil {
		err = s.Close()
	}
	l.wg.Wait()
	return err
}

func (a *application) start(args []string) {
	if a.starting || a.closed {
		return
	}
	if a.controller != nil {
		pendingBeforeClose := maps.Clone(a.pending)
		a.controller.Close()
		// Close joins admitted writes. Consume their final outcomes before a new
		// controller can reuse operation IDs or discard the queued notification.
		final := a.controller.Snapshot()
		a.receive(final)
		for id, pending := range pendingBeforeClose {
			if d := a.drafts[pending.draftKey]; d != nil {
				result := final.Results[id]
				if !result.Done || result.Error != "" {
					d.Saving = 0
					d.Error = "Session ended before save was confirmed. Check the original review before retrying."
					if result.Error != "" {
						d.Error += " " + result.Error
					}
				}
			}
		}
		clear(a.pending)
		a.controller = nil
	}
	if a.launch != nil {
		if err := a.launch.close(); err != nil {
			fmt.Fprintln(a.stderr, "Previous local session:", err)
		}
	}
	ctx, cancel := context.WithCancel(a.ctx)
	l := &launch{cancel: cancel}
	a.launch, a.starting, a.startupError = l, true, ""
	a.lastArgs = slices.Clone(args)
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		sidecar, err := a.resolveSidecar()
		var input *os.File
		if err == nil && !a.desktopLaunch && !a.ignoreStdin {
			input, err = a.replayStdin(ctx)
			if input != nil && input != a.stdin {
				defer input.Close()
			}
		}
		var s *localSession
		if err == nil {
			s, err = a.startSidecar(ctx, session.Options{Args: slices.Clone(args), Stdin: input, Stderr: a.stderr, SidecarPath: sidecar})
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintln(a.stderr, err)
			a.dispatch(func() {
				if a.closed || a.launch != l {
					return
				}
				a.starting, a.startupError = false, err.Error()
				if a.desktopLaunch && slices.Contains(args, "--attach") {
					a.needsFolder = true
				}
			})
			return
		}
		l.mu.Lock()
		l.session = s
		l.mu.Unlock()
		if ctx.Err() != nil {
			_ = s.Close()
			return
		}
		a.dispatch(func() {
			if a.closed || a.launch != l || ctx.Err() != nil {
				return
			}
			a.starting, a.needsFolder, a.startupError = false, false, ""
			a.ready = &s.Ready
			var c controller
			c = a.newController(s.Client, func() {
				a.dispatch(func() {
					if !a.closed && a.launch == l && a.controller == c {
						a.receive(c.Snapshot())
					}
				})
			})
			a.controller = c
			seen := a.prefs.Seen
			if !a.prefs.SeenInitialized && len(seen) == 0 {
				seen = nil
			}
			c.RestoreSeen(seen)
			scopes := make(map[string]goclient.DiffMode, len(a.prefs.Scopes))
			for stream, scope := range a.prefs.Scopes {
				scopes[stream] = goclient.DiffMode(scope)
			}
			c.RestoreScopes(scopes)
			c.Start(ctx, s.Ready.Input)
		})
		select {
		case <-ctx.Done():
		case err := <-s.Done:
			if ctx.Err() == nil {
				if err == nil {
					err = errors.New("local session ended")
				}
				a.dispatch(func() {
					if !a.closed && a.launch == l {
						a.startupError = err.Error()
					}
				})
			}
		}
		_ = s.Close()
	}()
}

func (a *application) replayStdin(ctx context.Context) (*os.File, error) {
	if a.stdin == nil {
		return nil, nil
	}
	stat, err := a.stdin.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return a.stdin, nil
	}
	if a.capturePath == "" {
		f, err := os.CreateTemp("", "diffx-native-stdin-*")
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { _ = a.stdin.Close() })
		_, copyErr := io.Copy(f, a.stdin)
		stop()
		err = errors.Join(copyErr, f.Close())
		if err != nil {
			_ = os.Remove(f.Name())
			return nil, fmt.Errorf("read piped diff: %w", err)
		}
		a.capturePath = f.Name()
	}
	return os.Open(a.capturePath)
}

func (a *application) close() {
	if a.closed {
		return
	}
	a.closed = true
	a.cancel()
	if a.controller != nil {
		a.controller.Close()
	}
	if a.launch != nil {
		a.shutdownError = a.launch.close()
		if a.shutdownError != nil {
			fmt.Fprintln(a.stderr, "Close local session:", a.shutdownError)
		}
	}
	if a.capturePath != "" {
		_ = os.Remove(a.capturePath)
	}
}

func (a *application) chooseFolder() {
	if a.chooseDirectory == nil || a.starting {
		return
	}
	a.chooseDirectory(func(path string, err error) {
		if a.closed {
			return
		}
		if err != nil {
			a.startupError = err.Error()
		} else if path != "" {
			a.ignoreStdin = true
			a.args = []string{path}
			a.start(a.args)
		}
	})
}
