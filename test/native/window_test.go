package native_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	"github.com/flexdinesh/diffx/test/native/driver"
)

const fixtureOld = "first context\nold alpha\nold beta\nfourth context\nfifth context\nlast context\n"
const fixtureNew = "first context\nnew alpha\nnew beta\nfourth context\nfifth context\nlast context\n"

// MyGo's stored-patch buffer excludes the terminating newline. EOF markers are
// separate presentation metadata, not an extra blank line in this value.
var fixtureEditor = strings.TrimSuffix(fixtureNew, "\n")

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
	preferences                        string
	env                                []string
}

func setup(t *testing.T) fixture {
	t.Helper()
	if os.Getenv("DIFFX_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("real window acceptance requires DIFFX_NATIVE_ACCEPTANCE=1 in native Dagger or an actual macOS desktop session")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Fatal("native window acceptance supports Linux GTK/Xvfb and actual macOS desktop sessions")
	}
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			t.Fatal("run under xvfb-run and dbus-run-session")
		}
		if os.Getenv("MYGO_GPU") != "0" {
			t.Fatal("Linux native window acceptance requires CPU rendering: MYGO_GPU=0")
		}
	}
	clipboardTool := "xclip"
	if runtime.GOOS == "darwin" {
		clipboardTool = "pbpaste"
	}
	for _, tool := range []string{"git", clipboardTool} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	f := fixture{binary: os.Getenv("DIFFX_NATIVE_BINARY"), sidecar: os.Getenv("DIFFX_NATIVE_SIDECAR"), proxy: os.Getenv("DIFFX_NATIVE_DRIVER")}
	for name, path := range map[string]string{"DIFFX_NATIVE_BINARY": f.binary, "DIFFX_NATIVE_SIDECAR": f.sidecar, "DIFFX_NATIVE_DRIVER": f.proxy} {
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
	f.preferences = filepath.Join(f.root, "config", "diffx-native", "preferences.json")
	if runtime.GOOS == "darwin" {
		f.preferences = filepath.Join(f.root, "home", "Library", "Application Support", "diffx-native", "preferences.json")
	}
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
	git("config", "user.name", "Native Acceptance")
	git("config", "user.email", "native@example.invalid")
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
	t    *testing.T
	ctx  context.Context
	f    fixture
	p    *process
	c    *driver.Client
	id   int
	tree driver.Tree
}

func open(t *testing.T, f fixture) *window {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 65*time.Second)
	t.Cleanup(cancel)
	gate, err := net.Listen("unix", filepath.Join(f.root, "gate.sock"))
	must(t, err)
	t.Cleanup(func() { _ = gate.Close() })
	env := append(slices.Clone(f.env), "MYGO_AUTOMATION="+filepath.Join(f.root, "automation.sock"), "DIFFX_NATIVE_SIDECAR="+f.proxy, "DIFFX_NATIVE_REAL_SIDECAR="+f.sidecar, "DIFFX_NATIVE_GATE="+gate.Addr().String())
	w := &window{t: t, ctx: ctx, f: f, p: start(t, f.binary, env, "--port=0", f.repo)}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("native stdout/stderr:\n%s\nlast accessibility tree:\n%s", w.p.output.String(), treeJSON(w.tree))
		}
	})
	must(t, driver.Until(ctx, "automation socket (check nativeautomation build tag; mygo_noinspector disables v0.7.4 automation)", func(ctx context.Context) error {
		if err := w.p.running(); err != nil {
			return err
		}
		c, err := driver.Dial(ctx, filepath.Join(f.root, "automation.sock"))
		if err == nil {
			w.c = c
		}
		return err
	}))
	t.Cleanup(func() { _ = w.c.Close() })
	must(t, driver.Until(ctx, "visible native content window", func(ctx context.Context) error {
		windows, err := w.c.Windows(ctx)
		if err != nil {
			return err
		}
		if len(windows) != 1 {
			return fmt.Errorf("windows: %+v", windows)
		}
		info := windows[0]
		if info.Kind != "content" || info.URL != "" {
			t.Fatalf("native process opened a page: %+v", info)
		}
		if !info.Visible || info.ContentBounds.Width < 960 || info.ContentBounds.Height < 600 {
			return fmt.Errorf("window geometry/visibility: %+v", info)
		}
		w.id = info.ID
		return nil
	}))
	w.find("text", "Opening local review")
	w.assertNoWebviews()
	// The process must display loading before its gated CLI can serve anything.
	if s, err := f.status(ctx); err != nil {
		t.Fatal(err)
	} else if s.State != "stopped" && s.State != "running" {
		t.Fatalf("unexpected status during handshake: %+v", s)
	}
	accepted := make(chan error, 1)
	go func() {
		conn, err := gate.Accept()
		if err == nil {
			_, err = conn.Write([]byte{1})
			_ = conn.Close()
		}
		accepted <- err
	}()
	select {
	case err := <-accepted:
		must(t, err)
	case <-ctx.Done():
		t.Fatal("sidecar never reached handshake gate")
	}
	w.find("button", "Leave review comment on file review.txt")
	w.find("textField", "New text")
	w.assertNoWebviews()
	return w
}
func treeJSON(tree driver.Tree) string {
	data, _ := json.MarshalIndent(tree, "", "  ")
	return string(data)
}
func (w *window) refresh(ctx context.Context) error {
	if err := w.p.running(); err != nil {
		return err
	}
	tree, err := w.c.Tree(ctx, w.id)
	if err == nil {
		w.tree = tree
	}
	return err
}
func (w *window) find(role, label string) driver.Node {
	w.t.Helper()
	var found driver.Node
	must(w.t, driver.Until(w.ctx, role+" "+label, func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		var matches []driver.Node
		for _, n := range w.tree.Nodes {
			if n.Role == role && n.Label == label {
				matches = append(matches, n)
			}
		}
		// Saved text can appear in both the sidebar and its file annotation.
		// Interactive targets remain unique so automation cannot act ambiguously.
		if len(matches) == 0 || len(matches) > 1 && role != "text" {
			return fmt.Errorf("found %d matching nodes; tree: %s", len(matches), treeJSON(w.tree))
		}
		found = matches[0]
		return nil
	}))
	return found
}
func (w *window) press(role, label string) {
	w.t.Helper()
	n := w.find(role, label)
	must(w.t, w.c.Act(w.ctx, w.id, n, "press", ""))
}
func (w *window) key(key string) {
	w.t.Helper()
	must(w.t, w.c.Input(w.ctx, w.id, driver.Event{Type: "key", Key: key}))
}
func (w *window) assertNoWebviews() {
	w.t.Helper()
	must(w.t, w.refresh(w.ctx))
	windows, err := w.c.Windows(w.ctx)
	must(w.t, err)
	for _, info := range windows {
		if info.Kind != "content" || info.URL != "" {
			w.t.Fatalf("native process opened a page: %+v", info)
		}
	}
	for _, n := range w.tree.Nodes {
		if n.WebView {
			w.t.Fatalf("native tree embeds a webview: %+v", n)
		}
	}
}
func (w *window) quit() {
	w.t.Helper()
	// menu sends its reply after Quit closes the app's listener; allow EOF only
	// here, then demand a successful process exit and the lifecycle assertion.
	label := "Quit"
	if runtime.GOOS == "darwin" {
		label = "Quit Diffx Native"
	}
	err := w.c.Call(w.ctx, "menu", map[string]any{"window": w.id, "path": []string{"Diffx", label}}, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		w.t.Logf("quit response: %v", err)
	}
	select {
	case <-w.p.done:
		if w.p.err != nil {
			w.t.Fatalf("native quit: %v\n%s", w.p.err, w.p.output.String())
		}
	case <-w.ctx.Done():
		w.t.Fatal("native did not quit")
	}
}

// request uses the viewer transport when base is its loopback URL, and the
// owner-only socket for native reads. Both address the same real sidecar.
func request(ctx context.Context, s status, viewer bool, method, path string, body any, result any) ([]byte, error) {
	base := "http://localhost"
	transport := &http.Transport{}
	if viewer {
		base = s.ViewerURL
	} else {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", s.Socket)
		}
	}
	defer transport.CloseIdleConnections()
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, &encoded)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, data)
	}
	if result != nil {
		err = json.Unmarshal(data, result)
	}
	return data, err
}

func TestNativeWindowOwnedReview(t *testing.T) {
	f := setup(t)
	w := open(t, f)
	s, err := f.status(w.ctx)
	must(t, err)
	if s.State != "running" || s.InstanceID == "" || s.Socket == "" || s.ViewerURL == "" {
		t.Fatalf("readiness: %+v", s)
	}
	var page goclient.ContextPage
	_, err = request(w.ctx, s, false, "GET", "/api/v2/contexts?latest=true", nil, &page)
	must(t, err)
	if len(page.Contexts) != 1 {
		t.Fatalf("fixture contexts: %+v", page.Contexts)
	}
	contextID := page.Contexts[0].Id
	base := "/api/v2/contexts/" + contextID
	var diff goclient.RepositoryDiff
	_, err = request(w.ctx, s, false, "GET", base+"/diffs/current?scope=uncommitted", nil, &diff)
	must(t, err)
	if len(diff.Files) != 1 || diff.Files[0].Path != "review.txt" {
		t.Fatalf("fixture diff: %+v", diff)
	}
	file := diff.Files[0]
	editor := w.find("textField", "New text")
	if editor.Value != fixtureEditor || !slices.Contains(editor.States, "readOnly") {
		t.Fatalf("native editor contents/read-only contract: %+v", editor)
	}
	w.geometry()
	// A real pointer selects a stored line through MyGo's native gutter. Derive
	// position from measured editor/header geometry and configured line height.
	var headerBottom float64
	for _, n := range w.tree.Nodes {
		if strings.HasPrefix(n.Label, "@@") {
			headerBottom = max(headerBottom, n.Bounds.Y+n.Bounds.Height)
		}
	}
	if headerBottom == 0 {
		headerBottom = editor.Bounds.Y
	}
	const lineHeight = 13 * 1.5
	must(t, w.c.Input(w.ctx, w.id, driver.Event{Type: "click", X: editor.Bounds.X + 30, Y: headerBottom + 1.5*lineHeight}))
	w.find("button", "Comment on lines 2–2")
	w.key("Alt+Shift+Down")
	w.press("button", "Comment on lines 2–3")
	input := w.find("textField", "Comment body")
	must(t, w.c.Act(w.ctx, w.id, input, "focus", ""))
	must(t, driver.Until(w.ctx, "comment keyboard focus", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		if w.tree.Focus != input.ID {
			return fmt.Errorf("focus=%s, want %s", w.tree.Focus, input.ID)
		}
		return nil
	}))
	const nativeBody = "Native range review: preserve alpha and beta."
	must(t, w.c.Input(w.ctx, w.id, driver.Event{Type: "text", Text: nativeBody}))
	must(t, driver.Until(w.ctx, "typed native comment", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		for _, n := range w.tree.Nodes {
			if n.ID == input.ID && n.Value == nativeBody {
				return nil
			}
		}
		return errors.New("comment body has not received text input")
	}))
	w.key("CmdOrCtrl+Enter")
	var nativeComment goclient.ReviewComment
	must(t, driver.Until(w.ctx, "native range comment persisted through viewer REST", func(ctx context.Context) error {
		var result struct {
			Comments []goclient.ReviewComment `json:"comments"`
		}
		if _, err := request(ctx, s, true, "GET", base+"/comments", nil, &result); err != nil {
			return err
		}
		for _, comment := range result.Comments {
			if comment.Body == nativeBody {
				nativeComment = comment
				return nil
			}
		}
		return fmt.Errorf("comments: %+v", result.Comments)
	}))
	if nativeComment.Start != 2 || nativeComment.End != 3 || nativeComment.Side != "additions" || nativeComment.Scope != "uncommitted" || nativeComment.Code != "+ new alpha\n+ new beta" || nativeComment.Placement == nil || nativeComment.Placement.Applicability != "anchored" {
		t.Fatalf("native range changed at server boundary: %+v", nativeComment)
	}
	w.find("text", nativeBody)
	// A web client writes through the public viewer REST transport. The native
	// mutable-state refresh must display it without Retry or Git collection.
	const uncollected = fixtureNew + "uncollected working tree edit\n"
	write(t, filepath.Join(f.repo, "review.txt"), []byte(uncollected))
	target := goclient.CreateCommentTargetFile
	webBody := goclient.CreateComment{Body: "Viewer transport file review", DiffId: diff.Id, VersionId: diff.VersionId, Scope: "uncommitted", FileId: file.Id, FileVersion: file.Fingerprint, Side: "additions", Target: &target}
	var webComment goclient.ReviewComment
	_, err = request(w.ctx, s, true, "POST", base+"/comments", webBody, &webComment)
	must(t, err)
	w.find("text", webBody.Body)
	if editor := w.find("textField", "New text"); editor.Value != fixtureEditor {
		t.Fatalf("mutable-state refresh changed the captured review: %q", editor.Value)
	}
	_, err = request(w.ctx, s, true, "GET", "/api/v2/contexts", nil, &page)
	must(t, err)
	if len(page.Contexts) != 1 || page.Contexts[0].Id != contextID {
		t.Fatalf("mutable-state refresh collected Git: %+v", page.Contexts)
	}
	w.key("CmdOrCtrl+Shift+C")
	w.find("text", "Review copied")
	exported, err := request(w.ctx, s, true, "GET", base+"/comments/export?scope=uncommitted", nil, nil)
	must(t, err)
	clip := exec.CommandContext(w.ctx, "xclip", "-selection", "clipboard", "-o")
	if runtime.GOOS == "darwin" {
		clip = exec.CommandContext(w.ctx, "pbpaste")
	}
	clip.Env = f.env
	clipboard, err := clip.Output()
	must(t, err)
	if !bytes.Equal(clipboard, exported) || !bytes.Contains(clipboard, []byte(nativeBody)) || !bytes.Contains(clipboard, []byte(webBody.Body)) {
		t.Fatalf("clipboard differs from canonical export\nclipboard: %s\nserver: %s", clipboard, exported)
	}
	w.press("button", "Toggle reviewed review.txt")
	must(t, driver.Until(w.ctx, "native reviewed mark visible to viewer", func(ctx context.Context) error {
		var result struct {
			Marks []goclient.ReviewMark `json:"marks"`
		}
		_, err := request(ctx, s, true, "GET", base+"/review-marks?scope=uncommitted", nil, &result)
		if err != nil {
			return err
		}
		if len(result.Marks) != 1 {
			return fmt.Errorf("marks: %+v", result.Marks)
		}
		return nil
	}))
	_, err = request(w.ctx, s, true, "DELETE", base+"/review-marks/"+file.Id+"?scope=uncommitted", nil, nil)
	must(t, err)
	must(t, driver.Until(w.ctx, "viewer mark deletion reaches native automatically", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		for _, n := range w.tree.Nodes {
			if n.Role == "button" && n.Label == "Toggle reviewed review.txt" && !slices.Contains(n.States, "selected") {
				return nil
			}
		}
		return errors.New("native file remains reviewed")
	}))
	w.theme()
	w.assertNoWebviews()
	// Native read-only review operations never mutate the fixture checkout.
	contents, err := os.ReadFile(filepath.Join(f.repo, "review.txt"))
	must(t, err)
	if string(contents) != uncollected {
		t.Fatal("native review mutated Git working content")
	}
	w.quit()
	must(t, driver.Until(w.ctx, "owned server stopped on native quit", func(ctx context.Context) error {
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "stopped" {
			return fmt.Errorf("still running: %+v", s)
		}
		return nil
	}))
}

func (w *window) geometry() {
	w.t.Helper()
	editor := w.find("textField", "New text")
	if editor.Bounds.Width < 200 || editor.Bounds.Height <= 0 {
		w.t.Fatalf("editor geometry: %+v", editor.Bounds)
	}
	w.press("button", "Toggle files")
	must(w.t, driver.Until(w.ctx, "native diff expands when file pane closes", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		for _, n := range w.tree.Nodes {
			if n.Role == "textField" && n.Label == "New text" && n.Bounds.Width > editor.Bounds.Width+100 {
				return nil
			}
		}
		return errors.New("diff width did not expand by >100 DIP")
	}))
	w.press("button", "Toggle files")
	must(w.t, driver.Until(w.ctx, "file pane geometry restored", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		for _, n := range w.tree.Nodes {
			if n.Role == "textField" && n.Label == "New text" && abs(n.Bounds.Width-editor.Bounds.Width) < 2 {
				return nil
			}
		}
		return errors.New("diff width did not return to original geometry")
	}))
}
func abs(n float64) float64 {
	if n < 0 {
		return -n
	}
	return n
}
func (w *window) theme() {
	w.t.Helper()
	w.key("CmdOrCtrl+,")
	w.press("radio", "diffx Dark")
	must(w.t, driver.Until(w.ctx, "dark theme persisted", func(context.Context) error {
		data, err := os.ReadFile(w.f.preferences)
		if err != nil {
			return err
		}
		var prefs struct {
			Theme string `json:"theme"`
		}
		if err = json.Unmarshal(data, &prefs); err != nil {
			return err
		}
		if prefs.Theme != "dark" {
			return fmt.Errorf("theme=%q", prefs.Theme)
		}
		return nil
	}))
	must(w.t, driver.Until(w.ctx, "dark native radio checked", func(ctx context.Context) error {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		for _, n := range w.tree.Nodes {
			if n.Role == "radio" && n.Label == "diffx Dark" && slices.Contains(n.States, "checked") {
				return nil
			}
		}
		return errors.New("Dark radio not checked")
	}))
	w.press("button", "Close settings")
	w.geometry()
}

func TestNativeWindowAttachedPreservesWebServer(t *testing.T) {
	f := setup(t)
	server := start(t, f.sidecar, f.env, "--no-browser", "--port=0", f.repo)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var before status
	must(t, driver.Until(ctx, "foreground web server ready", func(ctx context.Context) error {
		if err := server.running(); err != nil {
			return err
		}
		s, err := f.status(ctx)
		if err != nil {
			return err
		}
		if s.State != "running" || s.InstanceID == "" {
			return fmt.Errorf("status: %+v", s)
		}
		before = s
		return nil
	}))
	w := open(t, f)
	after, err := f.status(ctx)
	must(t, err)
	if after.InstanceID != before.InstanceID || after.Socket != before.Socket || after.ViewerURL != before.ViewerURL {
		t.Fatalf("native replaced compatible server: before=%+v after=%+v", before, after)
	}
	w.theme()
	w.quit()
	must(t, server.running())
	after, err = f.status(ctx)
	must(t, err)
	if after.State != "running" || after.InstanceID != before.InstanceID {
		t.Fatalf("native quit stopped/replaced attached server: %+v", after)
	}
	var page goclient.ContextPage
	_, err = request(ctx, after, true, "GET", "/api/v2/contexts?latest=true", nil, &page)
	must(t, err)
	if len(page.Contexts) != 1 {
		t.Fatalf("attached viewer lost real review: %+v", page)
	}
	// The owner, not the attaching native client, initiates server shutdown.
	must(t, server.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-server.done:
		if server.err != nil {
			t.Fatalf("foreground shutdown: %v\n%s", server.err, server.output.String())
		}
	case <-ctx.Done():
		t.Fatal("foreground server failed to stop")
	}
}
