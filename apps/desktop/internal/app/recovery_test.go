package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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

func TestCommentAnnotationsRequireTheirCapturedScope(t *testing.T) {
	a, f := fixture(t)
	fileTarget := goclient.ReviewCommentTargetFile
	comments := []goclient.ReviewComment{
		{Id: "staged-line", Body: "staged inline", Path: "src/one.go", Scope: "staged", Status: "open", Placement: &goclient.CommentPlacement{Applicability: "anchored", Side: "additions", Start: 2, End: 2}},
		{Id: "staged-file", Body: "staged file", Path: "src/one.go", Scope: "staged", Status: "open", Target: &fileTarget},
		{Id: "current-line", Body: "current inline", Path: "src/one.go", Scope: "all", Status: "open", Placement: &goclient.CommentPlacement{Applicability: "anchored", Side: "additions", Start: 2, End: 2}},
	}
	f.state.Comments = comments
	a.receive(f.state)
	p, err := diffview.NewPresentation(f.state.Patches["file-one"].Patch)
	if err != nil {
		t.Fatal(err)
	}
	v := diffview.NewReadOnlyView(p, editor.DiffSplit, false, nil)
	a.bindAnnotations(v, f.state.Diff.Files[0])
	if mapped := v.Mapping(); len(mapped) != 1 || mapped[0].Placement.Key != "current-line" {
		t.Fatalf("cross-scope inline placements: %+v", mapped)
	}
	tt := ui.NewTester(func(c *ui.Context) { a.fileAnnotations(c, f.state.Diff.Files[0]) }, 800, 600)
	if _, found := tt.Find("staged file"); found {
		t.Fatal("cross-scope file annotation rendered")
	}
	tt = ui.NewTester(func(c *ui.Context) { a.commentCard(c, comments[0], true) }, 800, 600)
	if _, found := tt.Find("Comparison: staged"); !found {
		t.Fatal("cross-scope sidebar omitted scope")
	}
	a.navigateComment(comments[0])
	if len(f.scopes) != 1 || f.scopes[0] != "staged" || a.commentTarget == nil {
		t.Fatalf("comment navigation did not switch scope: %v %+v", f.scopes, a.commentTarget)
	}
}

func TestCommentNavigationRetainsLineUntilPreviewMounts(t *testing.T) {
	a, f := fixture(t)
	patch := "@@ -1,250 +1,250 @@\n-old\n+new\n" + strings.Repeat(" context\n", 249)
	delete(f.state.Patches, "file-two")
	a.receive(f.state)
	comment := goclient.ReviewComment{Path: "src/two.go", Scope: "all", Placement: &goclient.CommentPlacement{Applicability: "anchored", Side: "additions", Start: 200, End: 200}}
	a.navigateComment(comment)
	tt := nativeTester(t, a, 1280, 800)
	if a.commentTarget == nil {
		t.Fatal("loading preview discarded line target")
	}
	f.state.Patches["file-two"] = goclient.FilePatch{Patch: patch}
	a.receive(f.state)
	tt.Frame()
	if a.commentTarget != nil || a.diffScroll.Y < 500 {
		t.Fatalf("mounted preview did not reveal line: target=%+v offset=%g", a.commentTarget, a.diffScroll.Y)
	}
	// A later navigation must not revive a stale target after cache eviction.
	a.navigateComment(comment)
	a.selectFile("file-one")
	if a.commentTarget != nil {
		t.Fatal("manual navigation retained deferred target")
	}
}

type closingController struct {
	controller
	settle func()
}

func (c closingController) Close() { c.settle() }

func TestSessionReplacementSettlesOldDraftOperations(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "missing"} {
		t.Run(outcome, func(t *testing.T) {
			a, f := fixture(t)
			a.beginDraft(f.state.Diff.Files[0], nil)
			key := a.activeDraft
			a.drafts[key].Body = "preserve my review"
			a.saveDraft(key)
			a.controller = closingController{controller: f, settle: func() {
				if outcome == "missing" {
					delete(f.state.Results, 1)
					return
				}
				result := f.state.Results[1]
				result.Done = true
				if outcome == "failure" {
					result.Error = "workspace closed before save completed"
				}
				f.state.Results[1] = result
			}}
			a.resolveSidecar = func() (string, error) { return "", errors.New("test startup failure") }
			deliveries := make(chan func(), 8)
			a.dispatch = func(fn func()) { deliveries <- fn }
			a.start([]string{"/another-checkout"})
			if len(a.pending) != 0 {
				t.Fatal("old operations survived replacement")
			}
			d := a.drafts[key]
			if outcome == "success" {
				if d != nil {
					t.Fatal("acknowledged save retained draft")
				}
			} else if d == nil || d.Saving != 0 || d.Error == "" || d.Body != "preserve my review" {
				t.Fatalf("draft stranded: %+v", d)
			}
			// A fresh controller may reuse ID 1. It cannot settle an old draft.
			a.receive(workspace.State{Results: map[uint64]workspace.MutationResult{1: {ID: 1, Done: true}}})
			if outcome != "success" && a.drafts[key] == nil {
				t.Fatal("new operation ID settled old draft")
			}
		})
	}
}

func TestFirstCatalogPersistsSeenInitialization(t *testing.T) {
	a, _ := fixture(t)
	a.prefs.SeenInitialized = false
	a.receive(workspace.State{Seen: map[string]string{}})
	if !a.prefs.SeenInitialized {
		t.Fatal("empty successful catalog not initialized")
	}
	if err := a.prefs.Validate(); err != nil {
		t.Fatal(fmt.Errorf("initialized preferences: %w", err))
	}
}

func TestStartupDistinguishesFreshAndInitializedEmptyCatalog(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		t.Run(fmt.Sprint(initialized), func(t *testing.T) {
			prefs := preferences.Defaults()
			prefs.SeenInitialized = initialized
			a := newApplication(prefs)
			t.Cleanup(a.close)
			f := &fakeController{}
			a.newController = func(*goclient.ClientWithResponses, func()) controller { return f }
			a.resolveSidecar = func() (string, error) { return "/private/diffx", nil }
			a.startSidecar = func(context.Context, session.Options) (*localSession, error) {
				return &localSession{Ready: localprotocol.Readiness{}, Close: func() error { return nil }, Done: make(chan error)}, nil
			}
			deliveries := make(chan func(), 8)
			a.dispatch = func(fn func()) { deliveries <- fn }
			a.start(nil)
			select {
			case fn := <-deliveries:
				fn()
			case <-time.After(3 * time.Second):
				t.Fatal("startup did not dispatch")
			}
			if (f.state.Seen != nil) != initialized {
				t.Fatalf("fresh catalog did not request baseline: initialized=%v seen=%v", initialized, f.state.Seen)
			}
		})
	}
}

func TestNativeFileListPreservesMeasuredAnchor(t *testing.T) {
	for _, layout := range []string{"split", "unified"} {
		t.Run(layout, func(t *testing.T) {
			a, f := fixture(t)
			a.prefs.Layout = layout
			f.state.Diff.Files = f.state.Diff.Files[:1]
			f.state.Patches["file-one"] = goclient.FilePatch{Patch: "@@ -50 +50 @@\n-old50\n+new50\n@@ -90 +90 @@\n-old90\n+new90\n"}
			var before, after strings.Builder
			for i := 1; i <= 120; i++ {
				if i == 50 || i == 90 {
					fmt.Fprintf(&before, "old%d\n", i)
					fmt.Fprintf(&after, "new%d\n", i)
				} else {
					fmt.Fprintf(&before, "same%d\n", i)
					fmt.Fprintf(&after, "same%d\n", i)
				}
			}
			f.state.Contents["file-one"] = goclient.FileContents{Before: before.String(), After: after.String()}
			f.state.Comments = []goclient.ReviewComment{
				{Id: "above", Path: "src/one.go", Body: "above anchor", Status: "open", Scope: "all", Placement: &goclient.CommentPlacement{Applicability: "anchored", Side: "additions", Start: 50, End: 50}},
				{Id: "anchor", Path: "src/one.go", Body: "viewport anchor", Status: "open", Scope: "all", Placement: &goclient.CommentPlacement{Applicability: "anchored", Side: "additions", Start: 90, End: 90}},
			}
			a.receive(f.state)
			tt := nativeTester(t, a, 1280, 800)
			v := a.fileViews["file-one"].view
			if err := v.Expand(0, 30, 0); err != nil {
				t.Fatal(err)
			}
			if err := v.Expand(1, 0, 30); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := v.ScrollTo(diffview.ServerRange{Side: editor.DiffNew, Start: 90, End: 90}); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			anchor, ok := tt.Find("viewport anchor")
			if !ok {
				t.Fatal("viewport anchor absent")
			}
			viewport, _ := tt.Find("File diffs")
			a.diffScroll.Y += anchor.Y - viewport.Y - 90
			tt.Frame()
			anchor, _ = tt.Find("viewport anchor")
			initial := anchor.Y
			if err := v.Expand(0, 0, 200); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			anchor, _ = tt.Find("viewport anchor")
			if math.Abs(float64(anchor.Y-initial)) > 1 {
				t.Fatalf("context moved native-list anchor: %g -> %g", initial, anchor.Y)
			}
			f.state.Comments[0].Body = strings.Repeat("a much longer comment above the viewport\n", 20)
			a.receive(f.state)
			tt.Frame()
			anchor, _ = tt.Find("viewport anchor")
			if math.Abs(float64(anchor.Y-initial)) > 1 {
				t.Fatalf("comment growth moved native-list anchor: %g -> %g", initial, anchor.Y)
			}
		})
	}
}
