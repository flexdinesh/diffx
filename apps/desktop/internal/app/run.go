// Package app composes MyGo's page window with the desktop host and lease owner.
package app

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"github.com/flexdinesh/diffx/apps/desktop/internal/assets"
	"github.com/flexdinesh/diffx/apps/desktop/internal/host"
	"github.com/flexdinesh/diffx/apps/desktop/internal/launcher"
)

// Desktop exposes launch operations only. The shared React app uses REST/SSE
// through the host; it cannot execute processes or choose arbitrary sidecars.
type Desktop struct {
	origin string
	launch *launcher.Launcher
	window *mygo.Window
	dialog sync.Mutex
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
	if err := checkRuntime(); err != nil {
		return err
	}
	mygo.App.SetName("Diffx Desktop")
	mygo.App.SetVersion(Version)
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	h, err := host.Start(assets.FS(), filepath.Join(dataDir, "desktop-origin.json"))
	if err != nil {
		return err
	}
	defer h.Close()
	launch := launcher.New(h, launcher.Options{Args: args, Stdin: stdin, Stderr: stderr})
	defer launch.Close()
	d := &Desktop{origin: h.Origin, launch: launch}
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
		_ = launch.Start()
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
