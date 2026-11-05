package desktop_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/test/desktop/driver"
)

const fixtureOld = "first context\nold alpha\nold beta\nfourth context\nfifth context\nlast context\n"
const fixtureNew = "first context\nnew alpha\nnew beta\nfourth context\nfifth context\nlast context\n"

type lockedBuffer struct {
	sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *lockedBuffer) String() string { b.Lock(); defer b.Unlock(); return b.Buffer.String() }

type process struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output lockedBuffer
}

func start(t *testing.T, path string, env []string, args ...string) *process {
	t.Helper()
	p := &process{cmd: exec.Command(path, args...), done: make(chan struct{})}
	p.cmd.Env, p.cmd.Stdout, p.cmd.Stderr = env, &p.output, &p.output
	// Kill only this test's process group on failure, including its sidecar.
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})
	return p
}
func (p *process) running() error {
	select {
	case <-p.done:
		return fmt.Errorf("process exited: %v\n%s", p.err, p.output.String())
	default:
		return nil
	}
}

type fixture struct {
	root, repo, sidecar, binary, proxy string
	env                                []string
}

func setup(t *testing.T) fixture {
	t.Helper()
	if os.Getenv("DIFFX_DESKTOP_ACCEPTANCE") != "1" {
		t.Skip("real window acceptance requires DIFFX_DESKTOP_ACCEPTANCE=1 in desktop Dagger or an actual macOS desktop session")
	}
	if runtime.GOOS != "linux" {
		t.Skip("this acceptance suite drives Linux GTK/X11; establish macOS runtime acceptance separately on a Mac")
	}
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			t.Fatal("run under xvfb-run and dbus-run-session")
		}
		if os.Getenv("MYGO_GPU") != "0" {
			t.Fatal("Linux desktop window acceptance requires CPU rendering: MYGO_GPU=0")
		}
	}
	clipboardTool := "xclip"
	if runtime.GOOS == "darwin" {
		clipboardTool = "pbpaste"
	}
	for _, tool := range []string{"git", clipboardTool, "xdotool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	f := fixture{binary: os.Getenv("DIFFX_DESKTOP_BINARY"), sidecar: os.Getenv("DIFFX_DESKTOP_SIDECAR"), proxy: os.Getenv("DIFFX_DESKTOP_DRIVER")}
	for name, path := range map[string]string{"DIFFX_DESKTOP_BINARY": f.binary, "DIFFX_DESKTOP_SIDECAR": f.sidecar, "DIFFX_DESKTOP_DRIVER": f.proxy} {
		info, err := os.Stat(path)
		if err != nil || !filepath.IsAbs(path) || info == nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("%s must name an absolute executable: %q (%v)", name, path, err)
		}
	}
	// Short paths avoid Unix socket truncation, independent of go test's tempdir.
	var err error
	f.root, err = os.MkdirTemp("/tmp", "dxn-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(f.root) })
	f.repo = filepath.Join(f.root, "review-fixture")
	for _, dir := range []string{f.repo, filepath.Join(f.root, "home"), filepath.Join(f.root, "config"), filepath.Join(f.root, "runtime"), filepath.Join(f.root, "xdg")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Exclude inherited personal Diffx/Git state and accidental automation opts.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "DIFFX_") || strings.HasPrefix(key, "MYGO_") || strings.HasPrefix(key, "GIT_") || slices.Contains([]string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "LANG", "LANGUAGE", "LC_ALL"}, key) {
			continue
		}
		f.env = append(f.env, entry)
	}
	f.env = append(f.env,
		"HOME="+filepath.Join(f.root, "home"),
		"XDG_CONFIG_HOME="+filepath.Join(f.root, "config"),
		"XDG_STATE_HOME="+filepath.Join(f.root, "state"),
		"XDG_CACHE_HOME="+filepath.Join(f.root, "cache"),
		"XDG_RUNTIME_DIR="+filepath.Join(f.root, "xdg"),
		"DIFFX_RUNTIME_DIR="+filepath.Join(f.root, "runtime"),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8",
	)
	if runtime.GOOS == "linux" {
		f.env = append(f.env, "MYGO_GPU=0", "GDK_BACKEND=x11")
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", f.repo}, args...)...)
		cmd.Env = f.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--initial-branch=main")
	git("config", "user.name", "Desktop Acceptance")
	git("config", "user.email", "desktop@example.invalid")
	write(t, filepath.Join(f.repo, "review.txt"), []byte(fixtureOld))
	git("add", "review.txt")
	git("commit", "-m", "Initial review fixture")
	write(t, filepath.Join(f.repo, "review.txt"), []byte(fixtureNew))
	return f
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type status struct{ State, InstanceID, Socket, ViewerURL string }

func (f fixture) status(ctx context.Context) (status, error) {
	cmd := exec.CommandContext(ctx, f.sidecar, "status", "--json")
	cmd.Env = f.env
	out, err := cmd.Output()
	var s status
	decodeErr := json.Unmarshal(out, &s)
	if s.State == "stopped" && decodeErr == nil {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("status: %w: %s", err, out)
	}
	return s, decodeErr
}

type window struct {
	t      *testing.T
	ctx    context.Context
	f      fixture
	p      *process
	c      *driver.Client
	id     int
	origin string
}

func open(t *testing.T, f fixture, input *os.File, args ...string) *window {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	socket := filepath.Join(f.root, fmt.Sprintf("automation-%d.sock", time.Now().UnixNano()))
	env := append(slices.Clone(f.env), "MYGO_AUTOMATION="+socket, "DIFFX_DESKTOP_SIDECAR="+f.sidecar)
	for _, entry := range f.env {
		if strings.HasPrefix(entry, "DIFFX_DESKTOP_GATE=") {
			env = append(slices.Clone(f.env), "MYGO_AUTOMATION="+socket, "DIFFX_DESKTOP_SIDECAR="+f.proxy, "DIFFX_DESKTOP_REAL_SIDECAR="+f.sidecar)
		}
	}
	// A pipe reaches the app exactly as shell stdin does, without shell wrappers.
	p := &process{cmd: exec.Command(f.binary, args...), done: make(chan struct{})}
	p.cmd.Env, p.cmd.Stdout, p.cmd.Stderr, p.cmd.Stdin = env, &p.output, &p.output, input
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	must(t, p.cmd.Start())
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})
	w := &window{t: t, ctx: ctx, f: f, p: p}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("desktop output:\n%s", p.output.String())
			directory := filepath.Join("..", "..", "apps", "desktop", "test-results")
			if err := os.MkdirAll(directory, 0700); err == nil {
				screenContext, screenCancel := context.WithTimeout(context.Background(), 3*time.Second)
				capture := exec.CommandContext(screenContext, "scrot", "--overwrite", filepath.Join(directory, t.Name()+"-screen.png"))
				capture.Env = f.env
				if out, err := capture.CombinedOutput(); err != nil {
					t.Logf("desktop screen capture: %v %s", err, out)
				}
				screenCancel()
			}
			diagnosticContext, diagnosticCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer diagnosticCancel()
			client, err := driver.Dial(diagnosticContext, socket)
			if err != nil {
				t.Logf("desktop diagnostics unavailable: %v", err)
				return
			}
			defer client.Close()
			var state json.RawMessage
			if err := client.Eval(diagnosticContext, w.id, `(async()=>({text:document.body.innerText,url:location.href,launch:await window.mygo.call('Desktop.Status')}))()`, &state); err == nil {
				t.Logf("desktop page and launcher: %s", state)
			}
			var capture struct{ PNG string }
			if err := client.Call(diagnosticContext, "capture", map[string]any{"window": w.id}, &capture); err == nil {
				if data, err := base64.StdEncoding.DecodeString(capture.PNG); err == nil {
					directory := filepath.Join("..", "..", "apps", "desktop", "test-results")
					if err := os.MkdirAll(directory, 0700); err == nil {
						if err := os.WriteFile(filepath.Join(directory, t.Name()+".png"), data, 0600); err != nil {
							t.Logf("write desktop screenshot: %v", err)
						}
					}
				}
			}
		}
	})
	must(t, driver.Until(ctx, "MyGo page automation socket", func(ctx context.Context) error {
		if err := p.running(); err != nil {
			return err
		}
		c, err := driver.Dial(ctx, socket)
		if err == nil {
			w.c = c
		}
		return err
	}))
	t.Cleanup(func() { _ = w.c.Close() })
	must(t, driver.Until(ctx, "visible MyGo page window", func(ctx context.Context) error {
		windows, err := w.c.Windows(ctx)
		if err != nil {
			return err
		}
		if len(windows) != 1 {
			return fmt.Errorf("windows: %+v", windows)
		}
		info := windows[0]
		if info.Kind != "page" {
			return fmt.Errorf("expected webview page, got %+v", info)
		}
		if !info.Visible || !strings.HasPrefix(info.URL, "http://127.0.0.1:") {
			return fmt.Errorf("page not ready: %+v", info)
		}
		w.id = info.ID
		u, err := url.Parse(info.URL)
		if err != nil {
			return err
		}
		w.origin = u.Scheme + "://" + u.Host
		return nil
	}))
	return w
}

func (w *window) eval(script string, result any) {
	w.t.Helper()
	must(w.t, w.c.Eval(w.ctx, w.id, script, result))
}
func (w *window) wait(description, predicate string) {
	w.t.Helper()
	must(w.t, driver.Until(w.ctx, description, func(ctx context.Context) error {
		if err := w.p.running(); err != nil {
			return err
		}
		var ok bool
		if err := w.c.Eval(ctx, w.id, "Promise.resolve("+predicate+").then(Boolean)", &ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("condition false: %s", predicate)
		}
		return nil
	}))
}

// MyGo 0.7.4 eval reads WebKit. Its input RPC supports native content only;
// xdotool sends real X11 input to the actual GTK page, including user gestures.
func (w *window) input(args ...string) {
	w.t.Helper()
	search := exec.CommandContext(w.ctx, "xdotool", "search", "--onlyvisible", "--pid", fmt.Sprint(w.p.cmd.Process.Pid), "--name", "[Dd]iffx")
	search.Env = w.f.env
	out, err := search.Output()
	must(w.t, err)
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		w.t.Fatal("no desktop X11 window")
	}
	focus := exec.CommandContext(w.ctx, "xdotool", "windowfocus", "--sync", ids[len(ids)-1])
	focus.Env = w.f.env
	must(w.t, focus.Run())
	cmd := exec.CommandContext(w.ctx, "xdotool", args...)
	cmd.Env = w.f.env
	if out, err := cmd.CombinedOutput(); err != nil {
		w.t.Fatalf("xdotool %v: %v %s", args, err, out)
	}
}
func (w *window) click(selector string) {
	w.t.Helper()
	encoded, _ := json.Marshal(selector)
	w.wait("click target "+selector, "document.querySelector("+string(encoded)+")")
	var point struct{ X, Y int }
	w.eval(`(() => {const e=document.querySelector(`+string(encoded)+`);e.scrollIntoView({block:"center"});const r=e.getBoundingClientRect();return {X:Math.round(window.screenX+r.x+r.width/2),Y:Math.round(window.screenY+r.y+r.height/2)};})()`, &point)
	// GTK's native menu/title geometry is outside the DOM. Use window-relative
	// coordinates from the actual automation window's measured content bounds.
	var windows []driver.Window
	windows, err := w.c.Windows(w.ctx)
	must(w.t, err)
	for _, info := range windows {
		if info.ID == w.id {
			var local struct{ X, Y int }
			w.eval(`(() => {const r=document.querySelector(`+string(encoded)+`).getBoundingClientRect();return {X:Math.round(r.x+r.width/2),Y:Math.round(r.y+r.height/2)};})()`, &local)
			point.X = int(info.ContentBounds.X) + local.X
			point.Y = int(info.ContentBounds.Y) + local.Y
		}
	}
	w.input("mousemove", "--sync", fmt.Sprint(point.X), fmt.Sprint(point.Y), "click", "1")
}

// chooseFolder drives the real GTK chooser and accepts it only through X11.
// GTK navigation resolves location-entry completion without responding to the
// dialog; observed selection and sensitivity, not elapsed time, gate the click.
func (w *window) chooseFolder(path string) {
	w.t.Helper()
	type pickerState struct {
		Visible       bool
		Title         string
		Directory     string
		Filename      string
		Action        int
		WindowID      uint64
		OpenLabel     string
		OpenResponse  int
		OpenSensitive bool
		OpenMapped    bool
		OpenBounds    struct{ X, Y, Width, Height int }
	}
	var state pickerState
	defer func() {
		if w.t.Failed() {
			w.t.Logf("last native GTK picker observation: %+v", state)
		}
	}()
	observe := func(ctx context.Context) error {
		if err := w.p.running(); err != nil {
			return err
		}
		return w.c.Eval(ctx, w.id, `window.mygo.call('DesktopTest.Picker')`, &state)
	}
	must(w.t, driver.Until(w.ctx, "mapped native folder chooser", func(ctx context.Context) error {
		if err := observe(ctx); err != nil {
			return err
		}
		if !state.Visible || state.WindowID == 0 {
			return fmt.Errorf("native GTK picker: %+v", state)
		}
		return nil
	}))
	x11 := func(args ...string) {
		w.t.Helper()
		cmd := exec.CommandContext(w.ctx, "xdotool", args...)
		cmd.Env = w.f.env
		if out, err := cmd.CombinedOutput(); err != nil {
			w.t.Fatalf("native picker xdotool %v: %v %s", args, err, out)
		}
	}
	x11("windowfocus", "--sync", fmt.Sprint(state.WindowID))
	x11("key", "--clearmodifiers", "ctrl+l")
	x11("type", "--clearmodifiers", path+"/")
	encoded, err := json.Marshal(path)
	must(w.t, err)
	// GTK's public navigation selects this actual directory and clears any
	// inline completion. It leaves the modal dialog open for the real click.
	w.eval(`window.mygo.call('DesktopTest.NavigateFolder', `+string(encoded)+`)`, &state)
	must(w.t, driver.Until(w.ctx, "intended native directory and enabled Open", func(ctx context.Context) error {
		if err := observe(ctx); err != nil {
			return err
		}
		if !state.Visible || state.Action != 2 || state.Directory != path || state.Filename != path ||
			state.WindowID == 0 || state.OpenResponse != -3 || strings.ReplaceAll(state.OpenLabel, "_", "") != "Open" ||
			!state.OpenSensitive || !state.OpenMapped || state.OpenBounds.Width <= 0 || state.OpenBounds.Height <= 0 {
			return fmt.Errorf("native GTK picker: %+v", state)
		}
		return nil
	}))
	w.t.Logf("native GTK picker before real acceptance: %+v", state)
	x11("windowfocus", "--sync", fmt.Sprint(state.WindowID))
	x11("mousemove", "--sync", fmt.Sprint(state.OpenBounds.X+state.OpenBounds.Width/2), fmt.Sprint(state.OpenBounds.Y+state.OpenBounds.Height/2), "click", "1")
	must(w.t, driver.Until(w.ctx, "native picker closed after X11 Open click", func(ctx context.Context) error {
		if err := observe(ctx); err != nil {
			return err
		}
		if state.Visible {
			return fmt.Errorf("native GTK picker: %+v", state)
		}
		return nil
	}))
}

func (w *window) quit() {
	w.t.Helper()
	label := "Quit"
	if runtime.GOOS == "darwin" {
		label = "Quit Diffx Desktop"
	}
	err := w.c.Call(w.ctx, "menu", map[string]any{"window": w.id, "path": []string{"Diffx", label}}, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		w.t.Logf("quit reply: %v", err)
	}
	select {
	case <-w.p.done:
		if w.p.err != nil {
			w.t.Fatalf("quit: %v %s", w.p.err, w.p.output.String())
		}
	case <-w.ctx.Done():
		w.t.Fatal("desktop did not quit")
	}
}

func viewerRequest(ctx context.Context, s status, method, route string, body any, result any) ([]byte, error) {
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.ViewerURL, "/")+route, &encoded)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, route, response.Status, data)
	}
	if result != nil {
		err = json.Unmarshal(data, result)
	}
	return data, err
}

func (w *window) ready() {
	w.wait("shared review rendered in WebKit", `document.querySelector('#viewer diffs-container')`)
	w.wait("Pierre shadow DOM has measured highlighted lines", `Array.from(document.querySelectorAll('diffs-container')).some(e=>e.shadowRoot && e.shadowRoot.querySelector('[data-line]') && e.getBoundingClientRect().height>0)`)
	var actual bool
	w.eval(`Array.from(document.querySelectorAll('diffs-container')).some(e=>e.shadowRoot?.textContent.includes('new alpha'))`, &actual)
	if !actual {
		w.t.Fatal("WebKit rendered no captured additions")
	}
	w.wait("Pierre worker asset loaded by WebKit", `performance.getEntriesByType('resource').some(e=>/\/assets\/.*worker.*\.js/.test(e.name))`)
	w.eval(`CSS.supports('color','color-mix(in srgb, black, white)') && CSS.supports('selector(:has(*))') && typeof CSSStyleSheet.prototype.replaceSync==='function'`, &actual)
	if !actual {
		w.t.Fatal("WebKit lacks shared Safari16.4 target features")
	}
	w.eval(`!document.querySelector('link[rel="manifest"]')`, &actual)
	if !actual {
		w.t.Fatal("desktop activated browser PWA")
	}
}

func TestDesktopWebKitReviewMutationExportKeyboardAndOwnedLease(t *testing.T) {
	f := setup(t)
	w := open(t, f, nil, "--port=0", f.repo)
	w.ready()
	s, err := f.status(w.ctx)
	must(t, err)
	var page goclient.ContextPage
	_, err = viewerRequest(w.ctx, s, "GET", "/api/v2/contexts?latest=true", nil, &page)
	must(t, err)
	if len(page.Contexts) != 1 {
		t.Fatalf("contexts: %+v", page)
	}
	base := "/api/v2/contexts/" + page.Contexts[0].Id
	// The shared application defaults local edits to the uncommitted scope.
	// Mutations, reviewed marks and export must retain that captured scope.
	const scope = "uncommitted"
	w.wait("default local comparison", `document.querySelector('[aria-label="Comparison: Uncommitted"]')`)
	var diff goclient.RepositoryDiff
	_, err = viewerRequest(w.ctx, s, "GET", base+"/diffs/current?scope="+scope, nil, &diff)
	must(t, err)
	if len(diff.Files) != 1 {
		t.Fatalf("files: %+v", diff.Files)
	}
	w.click(`[aria-label="Leave review comment on file review.txt"]`)
	w.wait("comment editor focused", `document.activeElement?.matches('textarea[placeholder="Leave a review comment…"]')`)
	const body = "Desktop WebKit review comment"
	w.input("type", "--clearmodifiers", "--delay", "0", body)
	w.input("key", "--clearmodifiers", "ctrl+Return")
	must(t, driver.Until(w.ctx, "shared UI comment persisted through viewer", func(ctx context.Context) error {
		var result struct{ Comments []goclient.ReviewComment }
		if _, err := viewerRequest(ctx, s, "GET", base+"/comments", nil, &result); err != nil {
			return err
		}
		for _, comment := range result.Comments {
			if comment.Body == body && comment.Target != nil && *comment.Target == "file" && comment.Scope == scope {
				return nil
			}
		}
		return fmt.Errorf("comments: %+v", result.Comments)
	}))
	w.input("key", "--clearmodifiers", "Escape")
	w.input("key", "--clearmodifiers", "alt+m")
	must(t, driver.Until(w.ctx, "keyboard reviewed mutation persisted", func(ctx context.Context) error {
		var result struct{ Marks []goclient.ReviewMark }
		if _, err := viewerRequest(ctx, s, "GET", base+"/review-marks?scope="+scope, nil, &result); err != nil {
			return err
		}
		if len(result.Marks) != 1 {
			return fmt.Errorf("marks: %+v", result.Marks)
		}
		return nil
	}))
	// Use the shared web UI's export action, not the removed native shortcut.
	// Actual X11 gestures exercise the OS clipboard, including the web fallback.
	w.click("#comments-tab")
	w.wait("copy review action", `Array.from(document.querySelectorAll('button')).some(e=>e.textContent.trim()==='Copy review')`)
	w.eval(`Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()==='Copy review').id='desktop-acceptance-copy'`, nil)
	w.click("#desktop-acceptance-copy")
	w.wait("review export feedback", `document.body.textContent.includes('Copied unresolved comments as XML.') || document.querySelector('[aria-label="Comments XML"]')`)
	var manual bool
	w.eval(`Boolean(document.querySelector('[aria-label="Comments XML"]'))`, &manual)
	if manual {
		w.input("key", "--clearmodifiers", "ctrl+c")
	}
	exported, err := viewerRequest(w.ctx, s, "GET", base+"/comments/export?includeResolved=false", nil, nil)
	must(t, err)
	must(t, driver.Until(w.ctx, "system clipboard equals server export", func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o")
		cmd.Env = f.env
		data, err := cmd.Output()
		if err != nil {
			return err
		}
		if !bytes.Equal(data, exported) {
			return fmt.Errorf("clipboard differs: %q", data)
		}
		return nil
	}))
	contents, err := os.ReadFile(filepath.Join(f.repo, "review.txt"))
	must(t, err)
	if string(contents) != fixtureNew {
		t.Fatal("review UI changed working tree")
	}
	w.quit()
	must(t, driver.Until(w.ctx, "owned server stopped", func(ctx context.Context) error {
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "stopped" {
			return fmt.Errorf("status %+v", s)
		}
		return nil
	}))
}

func TestDesktopShowsSharedStartupBeforeSidecarReadiness(t *testing.T) {
	f := setup(t)
	gate, err := net.Listen("unix", filepath.Join(f.root, "gate.sock"))
	must(t, err)
	defer gate.Close()
	f.env = append(f.env, "DIFFX_DESKTOP_GATE="+gate.Addr().String())
	w := open(t, f, nil, "--port=0", f.repo)
	w.wait("shared loading UI before sidecar readiness", `document.body.textContent.includes('Opening local review') && !document.querySelector('#viewer')`)
	if listener, ok := gate.(*net.UnixListener); ok {
		must(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
	}
	conn, err := gate.Accept()
	must(t, err)
	_, err = conn.Write([]byte{1})
	conn.Close()
	must(t, err)
	w.ready()
	w.quit()
}

func TestDesktopAttachAndRecoveryNeverShutsDownAttachedServer(t *testing.T) {
	f := setup(t)
	owner := start(t, f.sidecar, f.env, "--no-browser", "--port=0", f.repo)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var before status
	must(t, driver.Until(ctx, "owner viewer ready", func(ctx context.Context) error {
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "running" {
			return fmt.Errorf("%+v", s)
		}
		before = s
		return nil
	}))
	w := open(t, f, nil, "--desktop-launch")
	w.ready()
	var launch struct{ Owned bool }
	w.eval(`window.mygo.call('Desktop.Status')`, &launch)
	if launch.Owned {
		t.Fatal("attachment transferred ownership")
	}
	after, err := f.status(ctx)
	must(t, err)
	if before.InstanceID != after.InstanceID {
		t.Fatal("attachment replaced server")
	}
	w.quit()
	must(t, owner.running())
	after, err = f.status(ctx)
	must(t, err)
	if after.State != "running" || after.InstanceID != before.InstanceID {
		t.Fatal("attached quit stopped server")
	}
	// A subsequent attached window surfaces owner loss and unmounts its review.
	w = open(t, f, nil, "--desktop-launch")
	w.ready()
	must(t, owner.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-owner.done:
	case <-ctx.Done():
		t.Fatal("owner shutdown timed out")
	}
	w.wait("session loss disposes shared review", `!document.querySelector('#viewer') && document.body.textContent.includes('Could not open local review')`)
	w.eval(`window.mygo.call('Desktop.Retry')`, nil)
	w.wait("attach retry offers folder choice", `document.body.textContent.includes('Choose a checkout or workspace')`)
	w.quit()
}

func TestDesktopPipedInput(t *testing.T) {
	f := setup(t)
	input, err := os.CreateTemp(f.root, "stdin-")
	must(t, err)
	defer input.Close()
	_, err = input.WriteString("diff --git a/review.txt b/review.txt\n--- a/review.txt\n+++ b/review.txt\n@@ -1 +1 @@\n-old alpha\n+new alpha\n")
	must(t, err)
	_, err = input.Seek(0, 0)
	must(t, err)
	w := open(t, f, input, "--port=0")
	w.ready()
	w.wait("stdin shown as Piped", `document.body.textContent.includes('Piped')`)
	w.quit()
}

func TestDesktopPreferencesSurviveActualWebKitRestart(t *testing.T) {
	f := setup(t)
	w := open(t, f, nil, "--port=0", f.repo)
	w.ready()
	origin := w.origin
	w.click("#settings")
	w.wait("theme choices", `Array.from(document.querySelectorAll('[role="radio"]')).some(e=>e.textContent.includes('diffx Dark'))`)
	w.eval(`Array.from(document.querySelectorAll('[role="radio"]')).find(e=>e.textContent.includes('diffx Dark')).id='desktop-acceptance-dark'`, nil)
	w.click("#desktop-acceptance-dark")
	w.wait("appearance preference saved", `localStorage.getItem('theme-v2')==='dark' && document.documentElement.dataset.theme==='dark'`)
	w.input("key", "--clearmodifiers", "Escape")
	w.eval(`Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()==='Unified').id='desktop-acceptance-layout'`, nil)
	w.click("#desktop-acceptance-layout")
	w.wait("display preference saved", `localStorage.getItem('layout')==='unified'`)
	w.quit()
	w = open(t, f, nil, "--port=0", f.repo)
	w.ready()
	if w.origin != origin {
		t.Fatalf("restart lost preferences origin: %s -> %s", origin, w.origin)
	}
	w.wait("appearance and display restored after process restart", `localStorage.getItem('theme-v2')==='dark' && localStorage.getItem('layout')==='unified' && document.documentElement.dataset.theme==='dark' && Array.from(document.querySelectorAll('button')).some(e=>e.textContent.trim()==='Unified' && e.getAttribute('aria-pressed')==='true')`)
	w.quit()
}

func TestDesktopExplicitReplacementAndFolderChoice(t *testing.T) {
	f := setup(t)
	owner := start(t, f.sidecar, f.env, "--no-browser", "--port=0", f.repo)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var before status
	must(t, driver.Until(ctx, "owner ready", func(ctx context.Context) error {
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "running" {
			return fmt.Errorf("%+v", s)
		}
		before = s
		return nil
	}))
	other := filepath.Join(f.root, "another-workspace")
	must(t, os.Mkdir(other, 0700))
	w := open(t, f, nil, other, "--port=0")
	must(t, owner.running())
	w.wait("scope conflict offers explicit replacement", `document.body.textContent.includes('Replace running session')`)
	after, err := f.status(ctx)
	must(t, err)
	if after.InstanceID != before.InstanceID {
		t.Fatal("scope conflict replaced automatically")
	}
	w.eval(`Array.from(document.querySelectorAll('button')).find(e=>e.textContent.includes('Replace running session')).click()`, nil)
	w.wait("replacement confirmation", `document.body.textContent.includes('Replace running local session?')`)
	after, err = f.status(ctx)
	must(t, err)
	if after.InstanceID != before.InstanceID {
		t.Fatal("opening confirmation replaced server")
	}
	w.eval(`Array.from(document.querySelectorAll('button')).find(e=>e.textContent==='Replace session').click()`, nil)
	w.wait("replacement ready", `window.mygo && !document.body.textContent.includes('Replace running local session?')`)
	must(t, driver.Until(ctx, "new instance after explicit confirmation", func(ctx context.Context) error {
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "running" || s.InstanceID == before.InstanceID {
			return fmt.Errorf("%+v", s)
		}
		return nil
	}))
	// Changing an owned scope closes its own lease first. The native picker
	// supplies the new path; saved server review data remains intact.
	var beforeFolder struct{ Generation uint64 }
	w.eval(`window.mygo.call('Desktop.Status')`, &beforeFolder)
	w.eval(`window.dispatchEvent(new Event('diffx:choose-folder'))`, nil)
	w.chooseFolder(f.repo)
	w.wait("folder selection starts and owns the new scope", fmt.Sprintf(`(async()=>{const s=await window.mygo.call('Desktop.Status');return s.generation>%d && s.phase==='ready' && s.owned;})()`, beforeFolder.Generation))
	w.ready()
	var launch struct{ Owned bool }
	w.eval(`window.mygo.call('Desktop.Status')`, &launch)
	if !launch.Owned {
		t.Fatal("folder choice lost owned-server lifetime")
	}
	w.quit()
}
