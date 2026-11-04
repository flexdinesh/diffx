package app

import (
	"context"
	"io"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/flexdinesh/diffx/apps/desktop/internal/diffview"
	"github.com/flexdinesh/diffx/apps/desktop/internal/preferences"
	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// The controller owns asynchronous requests and server semantics. This seam
// permits ui.Tester to exercise the same native controls without a display.
type controller interface {
	Start(context.Context, localprotocol.Input)
	RestoreSeen(map[string]string)
	RestoreScopes(map[string]goclient.DiffMode)
	Close()
	Snapshot() workspace.State
	OpenContext(string)
	OpenLatest(string)
	SetScope(goclient.DiffMode)
	OpenCommit(string)
	Sync()
	Retry()
	Search(string)
	SearchNext()
	SetFilters(workspace.Filters)
	LoadPatch(string)
	LoadContents(string)
	CreateComment(goclient.CreateComment) uint64
	CreateCommentFor(string, goclient.CreateComment) uint64
	UpdateComment(string, goclient.UpdateComment) uint64
	UpdateCommentFor(string, string, goclient.UpdateComment) uint64
	DeleteComment(string) uint64
	DeleteComments(string) uint64
	CarryComment(string) uint64
	MarkReviewed(string, bool) uint64
	ClearReviewed() uint64
	ForgetResult(uint64)
	ExportComments(func(string, error))
	ExportCommentsWithOptions(workspace.ExportOptions, func(string, error))
	LoadMetrics()
}

type draft struct {
	ContextID string
	Body      string
	Request   goclient.CreateComment
	EditingID string
	Saving    uint64
	Error     string
	Focus     ui.Handle
}

type pendingAction struct {
	draftKey    string
	body        string
	advanceFile string
	advance     bool
	reviewed    bool
}

type fileView struct {
	raw      string
	view     *diffview.ReadOnlyView
	error    string
	contents *goclient.FileContents
}

type reviewOwner struct {
	ContextID string
	Scope     goclient.DiffMode
	CommitID  string
}

type application struct {
	ctx                                                                          context.Context
	cancel                                                                       context.CancelFunc
	window                                                                       *mygo.Window
	dispatch                                                                     func(func())
	startSidecar                                                                 func(context.Context, session.Options) (*localSession, error)
	resolveSidecar                                                               func() (string, error)
	newController                                                                func(*goclient.ClientWithResponses, func()) controller
	chooseDirectory                                                              func(func(string, error))
	controller                                                                   controller
	state                                                                        workspace.State
	ready                                                                        *localprotocol.Readiness
	launch                                                                       *launch
	args, lastArgs                                                               []string
	stdin                                                                        *os.File
	stderr                                                                       io.Writer
	capturePath                                                                  string
	desktopLaunch, starting, needsFolder, closed                                 bool
	startupError, preferenceError, actionError, feedback                         string
	shutdownError                                                                error
	prefs                                                                        preferences.Preferences
	savePreferences                                                              func(preferences.Preferences) error
	selectedFile                                                                 string
	snapshotKey                                                                  reviewOwner
	fileFilter, reviewQuery, repositoryQuery, branchQuery, commitQuery           string
	commentFilter                                                                string
	sidebarTab                                                                   string
	viewportWidth                                                                float32
	picker, repoPicker, branchPicker, commitPicker, settings, metrics, shortcuts bool
	fileDrawer, projectDrawer                                                    bool
	showProjects                                                                 bool
	commitMessage, comparisonDetails                                             bool
	confirmTitle, confirmDetail                                                  string
	confirmAction                                                                func()
	drafts                                                                       map[string]*draft
	activeDraft                                                                  string
	pending                                                                      map[uint64]pendingAction
	fileViews                                                                    map[string]*fileView
	lineSelections                                                               map[string]diffview.ServerRange
	fileCollapsed, folderCollapsed, repositoryCollapsed                          map[string]bool
	commentExpanded                                                              map[string]bool
	diffList                                                                     ui.ListState
	fileScroll, projectScroll, commentScroll, settingsScroll, pickerScroll       ui.ScrollState
	fileFocus, searchFocus                                                       ui.Handle
	pickerProject                                                                string
	revealFile                                                                   string
	ignoreStdin                                                                  bool
	jumpFile                                                                     string
	lastVisibleFirst                                                             int
	lastWidths                                                                   [2]float32
	clipboard                                                                    ui.Services
}

func newApplication(prefs preferences.Preferences) *application {
	ctx, cancel := context.WithCancel(context.Background())
	return &application{
		ctx: ctx, cancel: cancel, prefs: prefs, stderr: io.Discard,
		dispatch: func(fn func()) { fn() }, savePreferences: func(p preferences.Preferences) error { return p.Save() },
		drafts: map[string]*draft{}, pending: map[uint64]pendingAction{}, fileViews: map[string]*fileView{}, lineSelections: map[string]diffview.ServerRange{},
		fileCollapsed: map[string]bool{}, folderCollapsed: map[string]bool{}, repositoryCollapsed: map[string]bool{},
		commentExpanded: map[string]bool{}, commentFilter: "Open", sidebarTab: "Files",
		lastVisibleFirst: -1, lastWidths: [2]float32{prefs.ProjectPaneWidth, prefs.FilePaneWidth},
		startSidecar:   startSidecar,
		resolveSidecar: resolveSidecar,
		newController: func(client *goclient.ClientWithResponses, notify func()) controller {
			return workspace.New(client, notify)
		},
	}
}

func (a *application) receive(s workspace.State) {
	a.state = s
	key := reviewOwner{}
	if s.Current != nil {
		key = reviewOwner{ContextID: s.Current.Id, Scope: s.Scope, CommitID: s.CommitID}
	}
	if key != a.snapshotKey {
		a.snapshotKey = key
		a.feedback, a.actionError = "", ""
		a.selectedFile, a.jumpFile = "", ""
		a.fileViews = map[string]*fileView{}
		a.lineSelections = map[string]diffview.ServerRange{}
		a.fileCollapsed = map[string]bool{}
		a.diffList = ui.ListState{}
		a.lastVisibleFirst = -1
		a.activeDraft = ""
	}
	// Discard native buffers with the bounded workspace preview cache. Drafts
	// and server-coordinate selections retain no source contents.
	for id := range a.fileViews {
		if _, present := s.Patches[id]; !present {
			delete(a.fileViews, id)
		}
	}
	for id, pending := range a.pending {
		result, ok := s.Results[id]
		if !ok || !result.Done {
			continue
		}
		delete(a.pending, id)
		if d := a.drafts[pending.draftKey]; d != nil && d.Saving == id {
			d.Saving = 0
			d.Error = result.Error
			if result.Error == "" && d.Body == pending.body {
				delete(a.drafts, pending.draftKey)
				if a.activeDraft == pending.draftKey {
					a.activeDraft = ""
				}
			}
		}
		if result.Error != "" {
			a.actionError = result.Error
		} else if pending.advanceFile != "" && result.ContextID == contextID(s) && result.Scope == s.Scope && s.CommitID == "" {
			a.fileCollapsed[pending.advanceFile] = pending.reviewed
			if pending.reviewed && (pending.advance || a.prefs.HideReviewed) && pending.advanceFile == a.selectedFile {
				a.advanceFrom(pending.advanceFile)
			} else if pending.reviewed && pending.advanceFile == a.selectedFile && !a.prefs.HideReviewed {
				a.jumpFile = pending.advanceFile
			}
		}
		if result.Error == "" && result.Kind == "clear-reviewed" && result.ContextID == contextID(s) && result.Scope == s.Scope {
			clear(a.fileCollapsed)
		}
		if a.controller != nil {
			a.controller.ForgetResult(id)
		}
	}
	if len(slices.DeleteFunc(slices.Clone(s.Contexts), func(c goclient.Context) bool { return !workspace.CurrentWork(c) })) > 1 {
		a.showProjects = true
	}
	if !maps.Equal(a.prefs.Seen, s.Seen) && s.Seen != nil {
		a.prefs.Seen = maps.Clone(s.Seen)
		a.persistMetadata()
	}
	if s.SavedScopes != nil {
		scopes := make(map[string]string, len(s.SavedScopes))
		for key, scope := range s.SavedScopes {
			scopes[key] = string(scope)
		}
		if !maps.Equal(a.prefs.Scopes, scopes) {
			a.prefs.Scopes = scopes
			a.persistMetadata()
		}
	}
	files := a.visibleFiles()
	if !slices.ContainsFunc(files, func(f goclient.ChangedFile) bool { return f.Id == a.selectedFile }) {
		if len(files) > 0 {
			a.selectedFile = files[0].Id
		} else {
			a.selectedFile = ""
		}
	}
}

func (a *application) persist() {
	if a.savePreferences != nil {
		if err := a.savePreferences(a.prefs); err != nil {
			a.preferenceError = err.Error()
			return
		}
	}
	a.preferenceError = ""
}

func (a *application) persistMetadata() {
	// Background metadata cannot overwrite an invalid preferences file.
	if a.preferenceError == "" {
		a.persist()
	}
}

func contextID(s workspace.State) string {
	if s.Current != nil {
		return s.Current.Id
	}
	return ""
}
func stringValue(v interface{ Get() (string, error) }) string { s, _ := v.Get(); return s }

func (a *application) capture() bool {
	return a.state.Bootstrap != nil && a.state.Bootstrap.Input == "capture"
}
func (a *application) commentsEnabled() bool {
	return a.state.CommitID == "" && a.state.Session != nil && a.state.Session.Capabilities.Review.Comments.State == goclient.CapabilityStateEnabled
}
func (a *application) mutableReview() bool {
	return a.state.CommitID == "" && a.state.Diff != nil && a.state.Session != nil
}
func (a *application) contentsEnabled() bool {
	return a.state.Session != nil && a.state.Session.Capabilities.Files.Contents.State == goclient.CapabilityStateEnabled
}

func (a *application) reviewed(f goclient.ChangedFile) bool {
	if a.state.Diff == nil || a.state.CommitID != "" {
		return false
	}
	return slices.ContainsFunc(a.state.Marks, func(m goclient.ReviewMark) bool {
		// The server's scoped read projects marks across snapshots when the
		// stored patch is unchanged. Original version IDs remain provenance.
		return m.FileId == f.Id && m.FileVersion == f.Fingerprint && m.Scope == a.state.Scope
	})
}

func (a *application) visibleFiles() []goclient.ChangedFile {
	if a.state.Diff == nil {
		return nil
	}
	query := strings.ToLower(a.fileFilter)
	out := make([]goclient.ChangedFile, 0, len(a.state.Diff.Files))
	for _, f := range a.orderedFiles() {
		if a.state.CommitID == "" && a.prefs.HideReviewed && a.reviewed(f) {
			continue
		}
		if strings.Contains(strings.ToLower(f.Path), query) {
			out = append(out, f)
		}
	}
	return out
}

// Match the web tree's directory-first traversal in every navigation surface.
func (a *application) orderedFiles() []goclient.ChangedFile {
	if a.state.Diff == nil {
		return nil
	}
	files := slices.Clone(a.state.Diff.Files)
	slices.SortFunc(files, func(a, b goclient.ChangedFile) int {
		x, y := strings.Split(a.Path, "/"), strings.Split(b.Path, "/")
		for i := 0; i < min(len(x), len(y)); i++ {
			if x[i] == y[i] {
				continue
			}
			xdir, ydir := i < len(x)-1, i < len(y)-1
			if xdir != ydir {
				if xdir {
					return -1
				}
				return 1
			}
			if order := strings.Compare(strings.ToLower(x[i]), strings.ToLower(y[i])); order != 0 {
				return order
			}
			return strings.Compare(x[i], y[i])
		}
		return strings.Compare(a.Path, b.Path)
	})
	return files
}

func (a *application) selectFile(id string) {
	a.selectedFile, a.jumpFile = id, id
	a.fileDrawer = false
	a.fileCollapsed[id] = false
	for _, f := range a.visibleFiles() {
		if f.Id == id {
			for dir := path.Dir(f.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
				a.folderCollapsed[dir] = false
			}
			break
		}
	}
}

func (a *application) stepFile(delta int) {
	files := a.visibleFiles()
	if len(files) == 0 {
		return
	}
	i := slices.IndexFunc(files, func(f goclient.ChangedFile) bool { return f.Id == a.selectedFile })
	a.selectFile(files[min(max(i+delta, 0), len(files)-1)].Id)
}

func (a *application) advanceFrom(id string) {
	if a.state.Diff == nil {
		return
	}
	ordered := a.orderedFiles()
	i := slices.IndexFunc(ordered, func(f goclient.ChangedFile) bool { return f.Id == id })
	files := a.visibleFiles()
	for _, f := range ordered[min(i+1, len(ordered)):] {
		if slices.ContainsFunc(files, func(v goclient.ChangedFile) bool { return v.Id == f.Id }) {
			a.selectFile(f.Id)
			return
		}
	}
	if len(files) > 0 {
		a.selectFile(files[len(files)-1].Id)
	}
}

func (a *application) markFile(f goclient.ChangedFile, advance bool) {
	if a.controller == nil || !a.mutableReview() {
		return
	}
	for _, pending := range a.pending {
		if pending.advanceFile == f.Id {
			return
		}
	}
	reviewed := !a.reviewed(f)
	a.actionError = ""
	id := a.controller.MarkReviewed(f.Id, reviewed)
	if id != 0 {
		a.pending[id] = pendingAction{advanceFile: f.Id, advance: reviewed && advance, reviewed: reviewed}
	}
}

func (a *application) requestConfirm(title, detail string, action func()) {
	a.confirmTitle, a.confirmDetail, a.confirmAction = title, detail, action
}

func (a *application) openContext(id string) {
	if a.controller != nil {
		a.controller.OpenContext(id)
	}
	a.picker, a.repoPicker, a.branchPicker, a.projectDrawer = false, false, false, false
}

func (a *application) openLatest(c goclient.Context) {
	if a.controller == nil {
		return
	}
	if stream := stringValue(c.StreamId); stream != "" {
		a.controller.OpenLatest(stream)
	} else {
		a.controller.OpenContext(c.Id)
	}
	a.repoPicker, a.branchPicker, a.projectDrawer = false, false, false
}

func (a *application) stepCommit(delta int) {
	if a.controller == nil || a.state.CommitID == "" {
		return
	}
	i := slices.IndexFunc(a.state.Commits, func(c goclient.CommitSummary) bool { return c.Oid == a.state.CommitID })
	if next := i + delta; next >= 0 && next < len(a.state.Commits) {
		a.controller.OpenCommit(a.state.Commits[next].Oid)
	}
}

func (a *application) draftsInSidebar() bool {
	return a.sidebarTab == "Comments" && (a.fileDrawer || a.viewportWidth >= 1012 && !a.prefs.FilePaneCollapsed)
}
