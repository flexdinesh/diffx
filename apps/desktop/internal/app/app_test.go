package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/flexdinesh/diffx/apps/desktop/internal/diffview"
	"github.com/flexdinesh/diffx/apps/desktop/internal/preferences"
	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type fakeController struct {
	state                                     workspace.State
	operations                                uint64
	created                                   []goclient.CreateComment
	createOwners                              []string
	updated                                   []goclient.UpdateComment
	updateIDs, updateOwners, deleted, carried []string
	marks                                     []struct {
		file     string
		reviewed bool
	}
	scopes                                   []goclient.DiffMode
	opened, latest, commits, searched        []string
	filters                                  []workspace.Filters
	syncs, retries, metrics, closed, started int
	loads, contentLoads                      []string
	exportOptions                            []workspace.ExportOptions
	exportText                               string
	exportError                              error
	startContext                             context.Context
}

func (f *fakeController) Start(ctx context.Context, _ localprotocol.Input) {
	f.started++
	f.startContext = ctx
}
func (f *fakeController) RestoreSeen(seen map[string]string) { f.state.Seen = maps.Clone(seen) }
func (f *fakeController) RestoreScopes(scopes map[string]goclient.DiffMode) {
	f.state.SavedScopes = maps.Clone(scopes)
}
func (f *fakeController) Close()                           { f.closed++ }
func (f *fakeController) Snapshot() workspace.State        { return f.state }
func (f *fakeController) OpenContext(id string)            { f.opened = append(f.opened, id) }
func (f *fakeController) OpenLatest(id string)             { f.latest = append(f.latest, id) }
func (f *fakeController) SetScope(scope goclient.DiffMode) { f.scopes = append(f.scopes, scope) }
func (f *fakeController) OpenCommit(id string)             { f.commits = append(f.commits, id) }
func (f *fakeController) Sync()                            { f.syncs++ }
func (f *fakeController) Retry()                           { f.retries++ }
func (f *fakeController) Search(q string)                  { f.searched = append(f.searched, q) }
func (f *fakeController) SearchNext()                      {}
func (f *fakeController) SetFilters(filters workspace.Filters) {
	f.filters = append(f.filters, filters)
}
func (f *fakeController) LoadPatch(id string)    { f.loads = append(f.loads, id) }
func (f *fakeController) LoadContents(id string) { f.contentLoads = append(f.contentLoads, id) }
func (f *fakeController) operation(kind string) uint64 {
	f.operations++
	f.state.Results[f.operations] = workspace.MutationResult{ID: f.operations, ContextID: contextID(f.state), Scope: f.state.Scope, Kind: kind}
	return f.operations
}
func (f *fakeController) CreateComment(body goclient.CreateComment) uint64 {
	return f.CreateCommentFor(contextID(f.state), body)
}
func (f *fakeController) CreateCommentFor(owner string, body goclient.CreateComment) uint64 {
	f.created = append(f.created, body)
	f.createOwners = append(f.createOwners, owner)
	return f.operation("create-comment")
}
func (f *fakeController) UpdateComment(id string, body goclient.UpdateComment) uint64 {
	return f.UpdateCommentFor(contextID(f.state), id, body)
}
func (f *fakeController) UpdateCommentFor(owner, id string, body goclient.UpdateComment) uint64 {
	f.updated = append(f.updated, body)
	f.updateIDs = append(f.updateIDs, id)
	f.updateOwners = append(f.updateOwners, owner)
	return f.operation("update-comment")
}
func (f *fakeController) DeleteComment(id string) uint64 {
	f.deleted = append(f.deleted, id)
	return f.operation("delete-comment")
}
func (f *fakeController) DeleteComments(status string) uint64 {
	f.deleted = append(f.deleted, "status:"+status)
	return f.operation("delete-comments")
}
func (f *fakeController) CarryComment(id string) uint64 {
	f.carried = append(f.carried, id)
	return f.operation("carry-comment")
}
func (f *fakeController) MarkReviewed(id string, reviewed bool) uint64 {
	f.marks = append(f.marks, struct {
		file     string
		reviewed bool
	}{id, reviewed})
	return f.operation("mark-reviewed")
}
func (f *fakeController) ClearReviewed() uint64  { return f.operation("clear-reviewed") }
func (f *fakeController) ForgetResult(id uint64) { delete(f.state.Results, id) }
func (f *fakeController) ExportComments(done func(string, error)) {
	f.ExportCommentsWithOptions(workspace.ExportOptions{}, done)
}
func (f *fakeController) ExportCommentsWithOptions(options workspace.ExportOptions, done func(string, error)) {
	f.exportOptions = append(f.exportOptions, options)
	done(f.exportText, f.exportError)
}
func (f *fakeController) LoadMetrics() { f.metrics++ }

func wire[T any](t *testing.T, text string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func fixture(t *testing.T) (*application, *fakeController) {
	t.Helper()
	caps := goclient.SessionCapabilities{}
	caps.Diff.Scopes.State = "enabled"
	caps.Diff.Scopes.Values = []goclient.DiffMode{"all", "uncommitted", "committed", "staged", "unstaged"}
	caps.Diff.StagingMetadata.State = "enabled"
	caps.Files.Contents.State = "enabled"
	caps.Review.Comments.State = "enabled"
	current := wire[goclient.Context](t, `{"id":"context-one","name":"diffx","branch":"feature","streamId":"stream-one","repositoryId":"repo-one","root":"/checkout","availability":"available","changedFileCount":2,"latestContextId":"context-one","createdAt":1793836800000}`)
	current.Capabilities = caps
	current.ScopeSummaries = &[]goclient.ScopeSummary{{Scope: "all", ComparisonKind: "combined", FileCount: 2}, {Scope: "uncommitted", ComparisonKind: "uncommitted", FileCount: 2}, {Scope: "committed", ComparisonKind: "committed", FileCount: 1}, {Scope: "staged", ComparisonKind: "staged", FileCount: 1}, {Scope: "unstaged", ComparisonKind: "unstaged", FileCount: 1}}
	files := []goclient.ChangedFile{{Id: "file-one", Path: "src/one.go", Fingerprint: "fingerprint-one", Status: "M", Additions: 1, Deletions: 1}, {Id: "file-two", Path: "src/two.go", Fingerprint: "fingerprint-two", Status: "M", Additions: 1, Deletions: 1}}
	patch := goclient.FilePatch{Patch: "--- a/src/one.go\n+++ b/src/one.go\n@@ -1,3 +1,3 @@\n before\n-old\n+new\n after\n"}
	f := &fakeController{state: workspace.State{Bootstrap: &goclient.Bootstrap0{Input: "checkout", Collect: true}, Contexts: []goclient.Context{current}, Current: &current, Session: &goclient.Session{Capabilities: caps}, Diff: &goclient.RepositoryDiff{Id: "diff-one", VersionId: "version-one", Revision: "revision-one", Files: files}, Scope: "all", Patches: map[string]goclient.FilePatch{"file-one": patch, "file-two": patch}, Contents: map[string]goclient.FileContents{}, Results: map[uint64]workspace.MutationResult{}, Filters: workspace.DefaultFilters(), Seen: map[string]string{"stream-one": "context-one"}, Connected: true}, exportText: "<review canonical=\"server\"/>"}
	a := newApplication(preferences.Defaults())
	a.controller = f
	a.savePreferences = func(preferences.Preferences) error { return nil }
	a.receive(f.state)
	t.Cleanup(a.close)
	return a, f
}

func nativeTester(t *testing.T, a *application, width, height int) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(a.view, width, height)
	tt.Frame()
	tt.Frame()
	return tt
}

func uiEvidence(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if dir := os.Getenv("DIFFX_NATIVE_TEST_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("click %q: %v; visible=%q", label, err, tt.Texts())
	}
}
func finish(t *testing.T, a *application, f *fakeController, id uint64, err string) {
	t.Helper()
	r, ok := f.state.Results[id]
	if !ok {
		t.Fatalf("missing operation %d", id)
	}
	r.Done = true
	r.Error = err
	f.state.Results[id] = r
	a.receive(f.state)
}

func TestNativeCommentSaveKeepsDraftUntilMatchingSuccess(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Leave review comment on file src/one.go")
	click(t, tt, "Comment body")
	tt.Type("Keep cancellation ownership here.")
	click(t, tt, "Save comment")
	if len(f.created) != 1 {
		t.Fatalf("requests=%v", f.created)
	}
	req := f.created[0]
	if req.Body != "Keep cancellation ownership here." || req.FileId != "file-one" || req.FileVersion != "fingerprint-one" || req.DiffId != "diff-one" || req.VersionId != "version-one" || req.Scope != "all" || req.Start != 0 || req.End != 0 || req.Target == nil || *req.Target != "file" {
		t.Fatalf("wrong captured request: %+v", req)
	}
	if len(a.drafts) != 1 {
		t.Fatal("draft cleared before response")
	}
	uiEvidence(t, tt, "comment-draft")
	finish(t, a, f, 1, "server unavailable")
	tt.Frame()
	if len(a.drafts) != 1 || !tt.HasText("server unavailable") {
		t.Fatal("failed draft or inline error lost")
	}
	click(t, tt, "Save comment")
	finish(t, a, f, 2, "")
	tt.Frame()
	if len(a.drafts) != 0 {
		t.Fatal("successful draft retained")
	}
}

func TestDraftSaveAfterNavigationRetainsOriginalOwner(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Leave review comment on file src/one.go")
	click(t, tt, "Comment body")
	tt.Type("Original snapshot concern")
	other := *f.state.Current
	other.Id = "context-two"
	f.state.Current = &other
	f.state.Scope = "staged"
	a.receive(f.state)
	tt.Frame()
	click(t, tt, "Comments")
	click(t, tt, "Save comment")
	if len(f.created) != 1 || f.createOwners[0] != "context-one" || f.created[0].Scope != "all" || f.created[0].VersionId != "version-one" {
		t.Fatalf("draft retargeted: owners=%v request=%+v", f.createOwners, f.created)
	}
}

func TestNativeCommentResolveCarryDeleteAndExport(t *testing.T) {
	a, f := fixture(t)
	comment := goclient.ReviewComment{Id: "comment-one", Body: "Preserve cancellation", Path: "src/one.go", Status: "open", Scope: "all", Code: "old", Start: 2, End: 2, Side: "deletions", Placement: &goclient.CommentPlacement{Applicability: "outdated", Side: "deletions", Start: 2, End: 2}}
	f.state.Comments = []goclient.ReviewComment{comment}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Comments")
	click(t, tt, "Keep open")
	if !slices.Equal(f.carried, []string{"comment-one"}) {
		t.Fatalf("carry=%v", f.carried)
	}
	click(t, tt, "Resolve")
	if len(f.updated) != 1 || f.updated[0].Status == nil || *f.updated[0].Status != "resolved" {
		t.Fatalf("update=%v", f.updated)
	}
	click(t, tt, "Copy review")
	if tt.Clipboard() != f.exportText {
		t.Fatalf("canonical export clipboard=%q", tt.Clipboard())
	}
	click(t, tt, "Delete")
	if len(f.deleted) != 0 {
		t.Fatal("delete happened before confirmation")
	}
	click(t, tt, "Confirm")
	if !slices.Equal(f.deleted, []string{"comment-one"}) {
		t.Fatalf("delete=%v", f.deleted)
	}
}

func TestNativeReviewExportShortcutUsesCanonicalClipboard(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	tt.SetClipboard("previous clipboard")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if tt.Clipboard() != f.exportText || len(f.exportOptions) != 1 || a.feedback != "Review copied" {
		t.Fatalf("export shortcut: clipboard=%q requests=%v feedback=%q", tt.Clipboard(), f.exportOptions, a.feedback)
	}
}

func TestReviewedShortcutWaitsForSuccessThenAdvances(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	tt.Key(ui.Alt, ui.KeyM)
	tt.Key(ui.Alt, ui.KeyM)
	if len(f.marks) != 1 || a.selectedFile != "file-one" {
		t.Fatalf("duplicate write or early advance: %+v %q", f.marks, a.selectedFile)
	}
	finish(t, a, f, 1, "write failed")
	tt.Frame()
	if a.selectedFile != "file-one" {
		t.Fatal("failed write advanced")
	}
	a.actionError = ""
	tt.Frame()
	tt.Key(ui.Alt, ui.KeyM)
	f.state.Marks = []goclient.ReviewMark{{DiffId: "diff-one", VersionId: "version-one", Scope: "all", FileId: "file-one", FileVersion: "fingerprint-one"}}
	finish(t, a, f, 2, "")
	tt.Frame()
	if a.selectedFile != "file-two" {
		t.Fatalf("mark success did not advance: %q", a.selectedFile)
	}
	tt.Key(ui.Alt, ui.KeyM)
	f.state.Marks = append(f.state.Marks, goclient.ReviewMark{DiffId: "diff-one", VersionId: "version-one", Scope: "all", FileId: "file-two", FileVersion: "fingerprint-two"})
	finish(t, a, f, 3, "")
	tt.Frame()
	tt.Key(ui.Alt, ui.KeyM)
	if len(f.marks) != 3 {
		t.Fatal("repeated mark shortcut unreviewed the final file")
	}
	f.state.Marks = f.state.Marks[:1]
	a.receive(f.state)
	tt.Frame()
	click(t, tt, "Hide reviewed")
	if got := a.visibleFiles(); len(got) != 1 || got[0].Id != "file-two" {
		t.Fatalf("hidden reviewed=%v", got)
	}
}

func TestCapturedComparisonAndCommitControlsAreReadOnly(t *testing.T) {
	a, f := fixture(t)
	f.state.Commits = []goclient.CommitSummary{{Oid: "111111111", Subject: "First", Body: "First message"}, {Oid: "222222222", Subject: "Second"}}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Diff comparison")
	if err := tt.ChooseMenuItem("Staged"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.scopes, []goclient.DiffMode{"staged"}) {
		t.Fatalf("scope action=%v", f.scopes)
	}
	f.state.CommitID = "111111111"
	a.receive(f.state)
	tt.Frame()
	tt.Key(ui.Alt, ui.KeyM)
	if len(f.marks) != 0 {
		t.Fatal("commit allowed reviewed mutation")
	}
	if _, found := tt.Find("Leave review comment on file src/one.go"); found {
		t.Fatal("commit exposes comment action")
	}
	click(t, tt, "Newer commit")
	if !slices.Equal(f.commits, []string{"222222222"}) {
		t.Fatalf("commit stepping=%v", f.commits)
	}
	click(t, tt, "Open in Branch changes")
	if f.scopes[len(f.scopes)-1] != "committed" {
		t.Fatal("commit did not return to captured branch comparison")
	}
}

func TestSyncAndNewerNoticeDoNotReplaceOpenSnapshot(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Sync checkout")
	if f.syncs != 1 {
		t.Fatal("sync action missing")
	}
	f.state.NewerContextID = "context-new"
	a.receive(f.state)
	tt.Frame()
	if contextID(a.state) != "context-one" || len(f.latest) != 0 {
		t.Fatal("arrival silently changed review")
	}
	click(t, tt, "View latest")
	if !slices.Equal(f.latest, []string{"stream-one"}) {
		t.Fatalf("latest action=%v", f.latest)
	}
}

func TestNativeSettingsPersistAndMetricsLoadOnlyOnDemand(t *testing.T) {
	a, f := fixture(t)
	var saved []preferences.Preferences
	a.savePreferences = func(p preferences.Preferences) error { saved = append(saved, p); return nil }
	tt := nativeTester(t, a, 1280, 800)
	if f.metrics != 0 {
		t.Fatal("metrics loaded without request")
	}
	click(t, tt, "Settings")
	click(t, tt, "diffx Dark")
	if a.prefs.Theme != "dark" || len(saved) == 0 || saved[len(saved)-1].Theme != "dark" {
		t.Fatal("theme not applied and persisted")
	}
	click(t, tt, "Close settings")
	click(t, tt, "Server connected")
	if f.metrics != 1 {
		t.Fatal("status did not request server metrics")
	}
}

func TestNativeSizesScalesAndFileFilter(t *testing.T) {
	for _, size := range [][2]int{{960, 640}, {1280, 800}} {
		for _, scale := range []float32{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%dx%d-%g", size[0], size[1], scale), func(t *testing.T) {
				a, _ := fixture(t)
				tt := nativeTester(t, a, size[0], size[1])
				tt.SetScale(scale)
				tt.Frame()
				uiEvidence(t, tt, fmt.Sprintf("workspace-%dx%d-scale-%g", size[0], size[1], scale))
				if size[0] < 1012 {
					click(t, tt, "Toggle files")
				}
				click(t, tt, "Filter files")
				tt.Type("two.go")
				if got := a.visibleFiles(); len(got) != 1 || got[0].Id != "file-two" {
					t.Fatalf("file filtering=%v", got)
				}
				if bounds, ok := tt.Find("Filter files"); !ok || bounds.W < 80 {
					t.Fatalf("unusable file filter: %+v %v", bounds, ok)
				}
			})
		}
	}
}

func TestSidecarOverrideRequiresAbsolutePath(t *testing.T) {
	t.Setenv("DIFFX_NATIVE_SIDECAR", "diffx")
	if _, err := resolveSidecar(); err == nil {
		t.Fatal("relative override accepted")
	}
	path := filepath.Join(t.TempDir(), "private-diffx")
	t.Setenv("DIFFX_NATIVE_SIDECAR", path)
	got, err := resolveSidecar()
	if err != nil || got != path {
		t.Fatalf("override=%q %v", got, err)
	}
}

func TestLaunchArgumentAndStdinReplay(t *testing.T) {
	args, desktop := launchArguments([]string{"--scan-worktrees", "--desktop-launch", "/checkout", "--base", "main"})
	if !desktop || !slices.Equal(args, []string{"--scan-worktrees", "/checkout", "--base", "main"}) {
		t.Fatalf("forwarded args=%v desktop=%v", args, desktop)
	}
	a := newApplication(preferences.Defaults())
	t.Cleanup(a.close)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	a.stdin = read
	t.Cleanup(func() { _ = read.Close() })
	if _, err := io.WriteString(write, "diff bytes\n"); err != nil {
		t.Fatal(err)
	}
	_ = write.Close()
	for range 2 {
		input, err := a.replayStdin(a.ctx)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(input)
		_ = input.Close()
		if err != nil || string(body) != "diff bytes\n" {
			t.Fatalf("replayed=%q %v", body, err)
		}
	}
	if stat, err := os.Stat(a.capturePath); err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("capture permissions=%v %v", stat, err)
	}
}

func TestStartupFailureRetryAndQuitJoinWithoutUIDelivery(t *testing.T) {
	a := newApplication(preferences.Defaults())
	a.savePreferences = func(preferences.Preferences) error { return nil }
	a.resolveSidecar = func() (string, error) { return "/private/diffx", nil }
	deliveries := make(chan func(), 8)
	a.dispatch = func(fn func()) { deliveries <- fn }
	var mu sync.Mutex
	var calls [][]string
	var closed int
	a.startSidecar = func(ctx context.Context, options session.Options) (*localSession, error) {
		mu.Lock()
		calls = append(calls, slices.Clone(options.Args))
		n := len(calls)
		mu.Unlock()
		if n == 1 {
			return nil, errors.New("scope conflict; explicit replacement required")
		}
		return &localSession{Ready: localprotocol.Readiness{Owned: true}, Close: func() error { mu.Lock(); closed++; mu.Unlock(); return nil }, Done: make(chan error)}, nil
	}
	a.start([]string{"--scan-worktrees", "/checkout"})
	select {
	case fn := <-deliveries:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("startup failure did not dispatch")
	}
	tt := nativeTester(t, a, 960, 640)
	click(t, tt, "Retry")
	select {
	case <-deliveries:
	case <-time.After(3 * time.Second):
		t.Fatal("successful startup did not dispatch")
	}
	// Do not run its queued UI delivery. Quit must still find and join it.
	a.close()
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || !slices.Equal(calls[0], calls[1]) || closed == 0 {
		t.Fatalf("retry/quit calls=%v closed=%d", calls, closed)
	}
}

func TestChooseFolderUsesChosenPathWithoutLauncherCWD(t *testing.T) {
	a := newApplication(preferences.Defaults())
	a.desktopLaunch = true
	a.resolveSidecar = func() (string, error) { return "/private/diffx", nil }
	deliveries := make(chan func(), 4)
	a.dispatch = func(fn func()) { deliveries <- fn }
	options := make(chan session.Options, 1)
	a.startSidecar = func(_ context.Context, o session.Options) (*localSession, error) {
		options <- o
		return nil, errors.New("test end")
	}
	a.chooseDirectory = func(done func(string, error)) { done("/chosen/workspace", nil) }
	tt := nativeTester(t, a, 960, 640)
	click(t, tt, "Choose folder")
	select {
	case o := <-options:
		if !slices.Equal(o.Args, []string{"/chosen/workspace"}) || o.Stdin != nil {
			t.Fatalf("launcher input=%+v", o)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("folder did not start session")
	}
	a.close()
}

func TestNativeLineCommentRetainsStoredSideAndCoordinates(t *testing.T) {
	a, f := fixture(t)
	f.state.Patches["file-one"] = goclient.FilePatch{Patch: "--- a/src/one.go\n+++ b/src/one.go\n@@ -40,3 +70,3 @@\n before\n-old\n+new\n after\n"}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	r, found := tt.Find("New text")
	if !found {
		t.Fatal("new-side native editor absent")
	}
	// The native editor's hunk header precedes its first displayed code row.
	tt.ClickAt(r.X+8, r.Y+38)
	v := a.fileViews["file-one"].view
	selected, ok := v.Selection()
	if !ok || selected.Side != editor.DiffNew || selected.Start < 70 || selected.End > 72 {
		t.Fatalf("native gutter selection=%+v %v bounds=%v", selected, ok, r)
	}
	click(t, tt, fmt.Sprintf("Comment on lines %d–%d", selected.Start, selected.End))
	click(t, tt, "Comment body")
	tt.Type("Native stored-coordinate comment")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(f.created) != 1 || f.created[0].Start != selected.Start || f.created[0].End != selected.End || f.created[0].Side != "additions" || f.created[0].Target != nil {
		t.Fatalf("line request=%+v selection=%+v", f.created, selected)
	}
	uiEvidence(t, tt, "line-comment")
}

func TestNativeSearchFiltersHistoryAndUnavailableRows(t *testing.T) {
	a, f := fixture(t)
	old := *f.state.Current
	old.Id = "old-context"
	old.Name = "archive"
	old.Stale = new(bool)
	*old.Stale = true
	unavailable := old
	unavailable.Id = "unavailable-context"
	unavailable.Availability = "unavailable"
	f.state.History = []goclient.Context{*f.state.Current, old, unavailable}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Search reviews")
	if !slices.Equal(f.searched, []string{""}) {
		t.Fatalf("search admission=%v", f.searched)
	}
	click(t, tt, "Freshness All")
	if len(f.filters) != 1 || f.filters[0].Freshness != "all" {
		t.Fatalf("freshness filters=%+v", f.filters)
	}
	f.state.Filters = f.filters[0]
	a.receive(f.state)
	tt.Frame()
	click(t, tt, "All snapshots")
	f.state.Filters = f.filters[len(f.filters)-1]
	a.receive(f.state)
	tt.Frame()
	click(t, tt, "Open stored review unavailable-context")
	if len(f.opened) != 0 {
		t.Fatal("unavailable review opened")
	}
	click(t, tt, "Open stored review old-context")
	if !slices.Equal(f.opened, []string{"old-context"}) || a.picker {
		t.Fatalf("historical selection=%v picker=%v", f.opened, a.picker)
	}
}

func TestNativeFileScrollSelectionAndResizablePane(t *testing.T) {
	a, f := fixture(t)
	var patch strings.Builder
	patch.WriteString("--- a/src/one.go\n+++ b/src/one.go\n@@ -1,80 +1,80 @@\n")
	for i := 1; i <= 80; i++ {
		if i == 40 {
			patch.WriteString("-old\n+new\n")
		} else {
			fmt.Fprintf(&patch, " line %d\n", i)
		}
	}
	for _, file := range f.state.Diff.Files {
		f.state.Patches[file.Id] = goclient.FilePatch{Patch: strings.ReplaceAll(patch.String(), "src/one.go", file.Path)}
	}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	divider, found := tt.Find("Divider")
	if !found {
		t.Fatal("native pane divider absent")
	}
	before := a.prefs.FilePaneWidth
	tt.Press(divider.X+divider.W/2, divider.Y+80)
	tt.Move(divider.X+65, divider.Y+80)
	tt.Release(divider.X+65, divider.Y+80)
	if a.prefs.FilePaneWidth <= before+30 {
		t.Fatalf("resize did not update saved width: %g -> %g", before, a.prefs.FilePaneWidth)
	}
	list, found := tt.Find("File diffs")
	if !found {
		t.Fatal("file viewport absent")
	}
	tt.Scroll(list.X+list.W/2, list.Y+120, 0, 100)
	if a.selectedFile != "file-one" {
		t.Fatal("selection advanced before first file left viewport")
	}
	tt.Scroll(list.X+list.W/2, list.Y+120, 0, 1800)
	if a.selectedFile != "file-two" {
		t.Fatalf("scroll did not select second file: %s", a.selectedFile)
	}
	tt.Key(ui.Alt, ui.KeyK)
	if a.selectedFile != "file-one" {
		t.Fatalf("keyboard did not return to first file: %s", a.selectedFile)
	}
}

func TestRendererEvictionRetainsOnlyLightweightDraftAndSelection(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Leave review comment on file src/one.go")
	click(t, tt, "Comment body")
	tt.Type("Unsaved draft survives eviction")
	a.lineSelections["file-one"] = diffview.ServerRange{Side: editor.DiffNew, Start: 2, End: 2}
	if a.fileViews["file-one"] == nil {
		t.Fatal("file was never rendered")
	}
	f.state.Patches = maps.Clone(f.state.Patches)
	delete(f.state.Patches, "file-one")
	a.receive(f.state)
	if a.fileViews["file-one"] != nil || len(a.drafts) != 1 || a.lineSelections["file-one"].Start != 2 {
		t.Fatal("eviction retained source or lost lightweight state")
	}
}

func TestNativeCLIInformationalCommandsNeedNoWindowOrSidecar(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "--version"} {
		var output strings.Builder
		handled, err := HandleCommand([]string{arg}, &output)
		if !handled || err != nil || output.Len() == 0 {
			t.Fatalf("command %s: %v %v %q", arg, handled, err, output.String())
		}
		if err := Run([]string{arg}, nil, &output); err != nil {
			t.Fatalf("Run informational command: %v", err)
		}
	}
	args, desktop := launchArguments([]string{"--", "--desktop-launch"})
	if desktop || !slices.Equal(args, []string{"--", "--desktop-launch"}) {
		t.Fatal("native argument parsing ignored -- delimiter")
	}
}

func TestBackgroundMetadataPreservesInvalidPreferences(t *testing.T) {
	a, f := fixture(t)
	a.preferenceError = "unsupported future preferences version"
	saved := 0
	a.savePreferences = func(preferences.Preferences) error { saved++; return nil }
	f.state.Seen = map[string]string{"stream-one": "future-context"}
	a.receive(f.state)
	if saved != 0 {
		t.Fatal("background metadata overwrote invalid preferences")
	}
	a.persist()
	if saved != 1 || a.preferenceError != "" {
		t.Fatal("explicit preferences recovery did not save")
	}
}

func TestDraftEditorHasOneNativeFocusOwner(t *testing.T) {
	a, f := fixture(t)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Leave review comment on file src/one.go")
	click(t, tt, "Comment body")
	tt.Type("Draft shown once")
	click(t, tt, "Comments")
	count := 0
	for _, label := range tt.Texts() {
		if label == "Comment body" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("draft bound to %d simultaneous native editors", count)
	}
	click(t, tt, "Comment body")
	tt.Type(" and saved with focus")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(f.created) != 1 || f.created[0].Body != "Draft shown once and saved with focus" {
		t.Fatalf("focused save=%+v", f.created)
	}
}

func TestSavedCommentEditRetainsOwnerAcrossScopeChange(t *testing.T) {
	a, f := fixture(t)
	target := goclient.ReviewCommentTargetFile
	f.state.Comments = []goclient.ReviewComment{{Id: "comment-edit", Body: "Original", Path: "src/one.go", Status: "open", Scope: "all", Target: &target, Placement: &goclient.CommentPlacement{Applicability: "anchored"}}}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Comments")
	click(t, tt, "Edit")
	click(t, tt, "Comment body")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Revised")
	current := *f.state.Current
	current.Id = "another-review"
	f.state.Current = &current
	f.state.Scope = "staged"
	f.state.Comments = nil
	a.receive(f.state)
	tt.Frame()
	click(t, tt, "Save comment")
	if len(f.updated) != 1 || f.updateOwners[0] != "context-one" || f.updateIDs[0] != "comment-edit" || f.updated[0].Body == nil || *f.updated[0].Body != "Revised" {
		t.Fatalf("edit retargeted: owners=%v updates=%+v", f.updateOwners, f.updated)
	}
}

func TestFullContextNativeActionUsesVerifiedStoredPatch(t *testing.T) {
	a, f := fixture(t)
	before := make([]string, 80)
	for i := range before {
		before[i] = fmt.Sprintf("line %d", i+1)
	}
	after := slices.Clone(before)
	after[40] = "changed"
	f.state.Patches["file-one"] = goclient.FilePatch{Patch: "--- a/src/one.go\n+++ b/src/one.go\n@@ -40,3 +40,3 @@\n line 40\n-line 41\n+changed\n line 42\n"}
	a.receive(f.state)
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Load full context src/one.go")
	if !slices.Equal(f.contentLoads, []string{"file-one"}) {
		t.Fatalf("content owner=%v", f.contentLoads)
	}
	f.state.Contents["file-one"] = goclient.FileContents{Before: strings.Join(before, "\n") + "\n", After: strings.Join(after, "\n") + "\n"}
	a.receive(f.state)
	tt.Frame()
	v := a.fileViews["file-one"].view
	raw := v.Presentation().Raw()
	click(t, tt, "Expand before")
	if v.Presentation().Raw() != raw || v.Presentation().Patch().Hunks[0].OldStart != 1 {
		t.Fatal("native expansion replaced stored identity or failed to include verified context")
	}
	if aAdded, aDeleted := v.Stats(); aAdded != 1 || aDeleted != 1 {
		t.Fatalf("expansion changed stored edit runs: +%d -%d", aAdded, aDeleted)
	}
}

func TestExplicitReplacementPreservesCLIInputArguments(t *testing.T) {
	a := newApplication(preferences.Defaults())
	a.resolveSidecar = func() (string, error) { return "/private/diffx", nil }
	a.lastArgs = []string{"--scan-worktrees", "/checkout"}
	a.startupError = "scope conflict"
	deliveries := make(chan func(), 2)
	a.dispatch = func(fn func()) { deliveries <- fn }
	calls := make(chan []string, 1)
	a.startSidecar = func(_ context.Context, options session.Options) (*localSession, error) {
		calls <- options.Args
		return nil, errors.New("finished")
	}
	tt := nativeTester(t, a, 960, 640)
	click(t, tt, "Replace running session")
	select {
	case <-calls:
		t.Fatal("replacement started before explicit confirmation")
	default:
	}
	click(t, tt, "Confirm")
	select {
	case args := <-calls:
		if !slices.Equal(args, []string{"--scan-worktrees", "/checkout", "--replace"}) {
			t.Fatalf("replacement args=%v", args)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not start")
	}
	a.close()
}

func TestReviewedProjectionCarriesAndResetUncollapsesOnlyAfterSuccess(t *testing.T) {
	a, f := fixture(t)
	f.state.Marks = []goclient.ReviewMark{{DiffId: "prior-diff", VersionId: "prior-version", Scope: "all", FileId: "file-one", FileVersion: "fingerprint-one"}}
	a.receive(f.state)
	a.fileCollapsed["file-one"] = true
	if !a.reviewed(f.state.Diff.Files[0]) {
		t.Fatal("unchanged patch lost carried reviewed mark")
	}
	tt := nativeTester(t, a, 1280, 800)
	click(t, tt, "Reset reviewed files")
	click(t, tt, "Confirm")
	if !a.fileCollapsed["file-one"] {
		t.Fatal("reset changed presentation before acknowledgement")
	}
	f.state.Marks = nil
	finish(t, a, f, 1, "")
	tt.Frame()
	if len(a.fileCollapsed) != 0 {
		t.Fatal("successful reviewed reset left files collapsed")
	}
}

func TestFileNavigationSharesDirectoryFirstOrder(t *testing.T) {
	a, f := fixture(t)
	f.state.Diff.Files = []goclient.ChangedFile{{Id: "z", Path: "z.go"}, {Id: "az", Path: "a/z.go"}, {Id: "a", Path: "a.go"}, {Id: "nested", Path: "a/b/k.go"}, {Id: "b", Path: "b.go"}}
	a.receive(f.state)
	paths := []string{}
	for _, file := range a.visibleFiles() {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"a/b/k.go", "a/z.go", "a.go", "b.go", "z.go"}) {
		t.Fatalf("tree/list/diff navigation order=%v", paths)
	}
	a.fileFilter = "a/"
	paths = nil
	for _, file := range a.visibleFiles() {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"a/b/k.go", "a/z.go"}) {
		t.Fatalf("path filter=%v", paths)
	}
}
