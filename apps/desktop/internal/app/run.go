// Package app composes MyGo's page window with the desktop host and lease owner.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/flexdinesh/diffx/apps/desktop/internal/assets"
	"github.com/flexdinesh/diffx/apps/desktop/internal/host"
	"github.com/flexdinesh/diffx/apps/desktop/internal/launcher"
	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// Desktop exposes launch operations only. The shared React app uses REST/SSE
// through the host; it cannot execute processes or choose arbitrary sidecars.
type Desktop struct {
	origin       string
	launch       *launcher.Launcher
	window       *mygo.Window
	dialog       sync.Mutex
	bootstrap    sync.Once
	accept       func() error
	bootstrapErr error
}

func sameOrigin(raw, origin string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && u.Opaque == "" && u.Scheme+"://"+u.Host == origin
}

func (d *Desktop) authorize(ctx context.Context) error {
	page := mygo.CallerPage(ctx)
	if page == nil || mygo.CallerWindow(ctx) != d.window || !sameOrigin(page.URL(), d.origin) {
		return errors.New("desktop bridge origin denied")
	}
	return ctx.Err()
}

// Bootstrap is called only by the mounted React entry. Acceptance means the
// runtime, assets and bridge are usable, before any sidecar consumes stdin.
func (d *Desktop) Bootstrap(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	d.bootstrap.Do(func() { d.bootstrapErr = d.accept() })
	return d.launch.Status(), d.bootstrapErr
}
func (d *Desktop) Confirm(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	err := d.launch.Confirm()
	return d.launch.Status(), err
}
func (d *Desktop) Cancel(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	err := d.launch.Cancel()
	return d.launch.Status(), err
}

func (d *Desktop) Status(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	return d.launch.Status(), nil
}
func (d *Desktop) Retry(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	err := d.launch.Retry()
	return d.launch.Status(), err
}
func (d *Desktop) Replace(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	err := d.launch.Replace()
	return d.launch.Status(), err
}
func (d *Desktop) Folder(ctx context.Context) (launcher.Status, error) {
	if err := d.authorize(ctx); err != nil {
		return launcher.Status{}, err
	}
	if !d.dialog.TryLock() {
		return d.launch.Status(), errors.New("folder dialog already open")
	}
	defer d.dialog.Unlock()
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.window, Title: "Choose checkout or workspace", Directory: true})
	if err == nil && len(paths) > 0 {
		// A page disposed while the native picker was open cannot change scope.
		if err = d.authorize(ctx); err == nil {
			err = d.launch.Folder(paths[0])
		}
	}
	return d.launch.Status(), err
}

// Run must execute on the process's main goroutine. Sidecar startup begins only
// after the window exists; blocking stdin and failures remain visible in React.
func Run(args []string, stdin *os.File, stderr io.Writer) error {
	directory, err := os.Getwd()
	if err != nil {
		return err
	}
	sidecar := os.Getenv("DIFFX_DESKTOP_SIDECAR")
	if sidecar == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		sidecar, err = session.ResolveSidecar(executable)
		if err != nil {
			return err
		}
	}
	if stdin != nil {
		info, err := stdin.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice != 0 {
			stdin = nil
		}
	}
	initial := localprotocol.Intent{Version: localprotocol.Version, Args: args, Directory: directory, Sidecar: sidecar, Stdin: stdin != nil, Environment: localprotocol.InvocationEnvironment(os.Environ())}
	var ack *os.File
	if os.Getenv(localprotocol.HandoffEnvironment) == "1" {
		_ = os.Unsetenv(localprotocol.HandoffEnvironment)
		request := os.NewFile(3, "desktop-startup")
		stdin = os.NewFile(4, "desktop-startup-stdin")
		ack = os.NewFile(5, "desktop-startup-ack")
		defer ack.Close()
		err = json.NewDecoder(io.LimitReader(request, 64*1024)).Decode(&initial)
		request.Close()
		if err != nil || initial.Version != localprotocol.Version || !filepath.IsAbs(initial.Directory) || !matchingSidecar(initial.Sidecar, sidecar) {
			return errors.New("invalid desktop startup contract")
		}
		if !initial.Stdin {
			stdin.Close()
			stdin = nil
		}
	}
	initialized := make(chan struct{})
	var d *Desktop
	inbox, err := localprotocol.Listen(func(ctx context.Context, intent localprotocol.Intent, input *os.File) localprotocol.Acceptance {
		if !matchingSidecar(intent.Sidecar, sidecar) {
			return localprotocol.Acceptance{Error: "desktop installation mismatch"}
		}
		timeout := time.NewTimer(20 * time.Second)
		defer timeout.Stop()
		select {
		case <-timeout.C:
			return localprotocol.Acceptance{Error: "desktop bootstrap unavailable"}
		case <-ctx.Done():
			return localprotocol.Acceptance{}
		case <-initialized:
		}
		d.window.Restore()
		d.window.Show()
		d.window.Focus()
		return d.launch.Request(ctx, intent, input)
	})
	if errors.Is(err, localprotocol.ErrRunning) {
		reply, sendErr := localprotocol.Send(context.Background(), initial, stdin)
		if ack != nil {
			_ = json.NewEncoder(ack).Encode(reply)
		}
		if sendErr != nil {
			return errors.New("desktop handoff unavailable")
		}
		if !reply.Accepted {
			return errors.New("desktop handoff declined")
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer inbox.Close()
	if err := checkRuntime(); err != nil {
		return err
	}
	mygo.App.SetName("Diffx Desktop")
	mygo.App.SetVersion(buildLabel())
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	h, err := host.Start(assets.FS(), filepath.Join(dataDir, "desktop-origin.json"))
	if err != nil {
		return err
	}
	defer h.Close()
	launch := launcher.New(h, launcher.Options{Args: initial.Args, Stdin: stdin, Stderr: stderr, Directory: initial.Directory, SidecarPath: sidecar, Environment: initial.Environment})
	defer launch.Close()
	d = &Desktop{origin: h.Origin, launch: launch}
	d.accept = func() error {
		if ack != nil {
			if err := json.NewEncoder(ack).Encode(localprotocol.Acceptance{Accepted: true}); err != nil {
				return err
			}
			ack.Close()
		}
		err := launch.Start()
		close(initialized)
		return err
	}
	mygo.Bind(d)
	bindDesktopTest(d)
	mygo.App.WhenReady(func() {
		d.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "Diffx Desktop", Width: 1280, Height: 800, MinWidth: 640, MinHeight: 480,
			URL:  h.Origin,
			Page: mygo.PageOptions{TrustedOrigins: []string{h.Origin}, DevTools: mygo.DevToolsDisabled},
		})
		d.window.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
			if !sameOrigin(e.URL, h.Origin) {
				e.PreventDefault()
				if e.UserInitiated {
					openExternal(e.URL)
				}
			}
		})
		d.window.Page().SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions { openExternal(req.URL); return nil })
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Diffx", Submenu: []*mygo.MenuItem{{Role: mygo.RoleQuit}}},
			{Label: "File", Submenu: []*mygo.MenuItem{{Label: "Choose folder", Accelerator: "CmdOrCtrl+O", Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
				// Route through the page so scope disposal and errors use the same UI.
				d.window.Page().Eval(`window.dispatchEvent(new Event("diffx:choose-folder"))`)
			}}, {Role: mygo.RoleClose}}},
			{Role: mygo.RoleEditMenu},
		}))
	})
	offClosed := mygo.App.OnWindowAllClosed(mygo.App.Quit)
	defer offClosed()
	offQuit := mygo.App.OnQuit(launch.Close)
	defer offQuit()
	return mygo.App.Run()
}

func openExternal(raw string) {
	u, err := url.Parse(raw)
	if err == nil && u.User == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto") {
		go mygo.Shell.OpenExternal(raw)
	}
}

func matchingSidecar(left, right string) bool {
	l, err := os.Stat(left)
	if err != nil {
		return false
	}
	r, err := os.Stat(right)
	return err == nil && os.SameFile(l, r)
}
