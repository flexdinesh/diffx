package diffview

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestContentParticipatesInParentScroll(t *testing.T) {
	raw := "@@ -1,46 +1,46 @@\n" + strings.Repeat(" context\n", 45) + "-old\n+new\n"
	v := NewReadOnlyView(adapterPresentation(t, raw), editor.DiffSplit, false, nil)
	var parent ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).TrackScroll(&parent).Fill().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				v.DrawContent(c)
				ui.Text(c, "Following file").Height(80)
			})
		})
	}, 800, 240)
	before := find(t, tt, "Following file")
	tt.Scroll(400, 100, 0, 400)
	after := find(t, tt, "Following file")
	if parent.Y <= 0 || after.Y >= before.Y-100 {
		t.Fatalf("file list did not own scrolling: before=%v after=%v scroll=%g", before, after, parent.Y)
	}
}

func TestCommitDisablesReviewRangesAndGutterActions(t *testing.T) {
	selected := 0
	v := NewReadOnlyView(adapterPresentation(t, "@@ -1 +1 @@\n-old\n+new\n"), editor.DiffSplit, false, func(_ ServerRange, valid bool) {
		if valid {
			selected++
		}
	})
	v.SetGutterCommentHandler(func(ServerRange) { t.Error("commit offered a review gutter action") })
	if err := v.SetPlacements([]Placement{{"row", "Row anchor", ServerRange{editor.DiffNew, 1, 1}, 24}}); err != nil {
		t.Fatal(err)
	}
	if err := v.SetSelection(ServerRange{editor.DiffNew, 1, 1}); err != nil {
		t.Fatal(err)
	}
	v.SetLineSelectionEnabled(false)
	if _, ok := v.Selection(); ok {
		t.Fatal("commit retained a review range")
	}
	if err := v.SetSelection(ServerRange{editor.DiffNew, 1, 1}); !errors.Is(err, ErrRange) {
		t.Fatalf("commit accepted a new review range: %v", err)
	}
	tt := widgetTester(v, 800, 240)
	row := find(t, tt, "Row anchor")
	tt.ClickAt(410, row.Y-10)
	if selected != 0 {
		t.Fatal("commit gutter selected a review range")
	}
	v.SetLineSelectionEnabled(true)
	tt.Frame()
	tt.ClickAt(10, row.Y-10)
	if _, ok := v.Selection(); !ok {
		t.Fatal("returning to review did not restore line selection")
	}
}
