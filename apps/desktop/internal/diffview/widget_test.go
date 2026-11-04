package diffview

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func widgetTester(v *ReadOnlyView, width, height int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, width, height)
}

func TestNativeMeasuredCommentGeometryBothSides(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			v := NewReadOnlyView(adapterPresentation(t, sparse), layout, false, nil)
			notes := []Placement{
				{"old", "old comment", ServerRange{editor.DiffOld, 41, 41}, 24},
				{"new", "new comment", ServerRange{editor.DiffNew, 71, 71}, 24},
				{"later-old", "later old comment", ServerRange{editor.DiffOld, 401, 401}, 24},
				{"later-new", "later new comment", ServerRange{editor.DiffNew, 701, 701}, 24},
			}
			if err := v.SetPlacements(notes); err != nil {
				t.Fatal(err)
			}
			heights := map[string]float32{"old": 42, "new": 68, "later-old": 32, "later-new": 32}
			widths := map[string]float32{}
			v.SetAnnotationRenderer(func(c *ui.Context, p Placement, width float32) float32 {
				widths[p.Key] = width
				ui.Column(c).Height(heights[p.Key]).FillWidth().Children(func() { ui.Text(c, p.Label) })
				return heights[p.Key]
			})
			tt := widgetTester(v, 800, 1000)
			tt.Frame()
			a, b := find(t, tt, "later old comment"), find(t, tt, "later new comment")
			if layout == editor.DiffSplit && math.Abs(float64(a.Y-b.Y)) > 1 {
				t.Fatalf("split comments misaligned: %v %v", a, b)
			}
			for key, width := range widths {
				if width <= 0 || width >= 800 {
					t.Fatalf("annotation %q width %g", key, width)
				}
			}
			// The app may supply the same placements every frame; measured values must
			// not reset to admission estimates or make a never-settling frame loop.
			if err := v.SetPlacements(notes); err != nil {
				t.Fatal(err)
			}
			heights["new"] = 108
			tt.Frame()
			later := find(t, tt, "later new comment")
			if math.Abs(float64(later.Y-b.Y-40)) > 1 {
				t.Fatalf("measured growth %v -> %v", b, later)
			}
			if tt.FramePending() {
				t.Fatal("measurement never settled")
			}
			if got := v.Mapping()[1].Placement.Height; got != 24 {
				t.Fatalf("caller placement mutated: %g", got)
			}
			tt.SetScale(2)
			tt.SetZoom(1.25)
			tt.Frame()
			find(t, tt, "old comment")
			find(t, tt, "new comment")
			if layout == editor.DiffSplit {
				old, new := find(t, tt, "later old comment"), find(t, tt, "later new comment")
				if math.Abs(float64(old.Y-new.Y)) > 1 {
					t.Fatalf("scaled split comments misaligned: %v %v", old, new)
				}
			}
			if err := v.Error(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeMeasuredTextWrapFontAndWindowResize(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		v := NewReadOnlyView(adapterPresentation(t, "@@ -1,2 +1,2 @@\n-old\n+new\n tail\n"), layout, false, nil)
		if err := v.SetPlacements([]Placement{
			{"body", strings.Repeat("measured native comment words ", 15), ServerRange{editor.DiffNew, 1, 1}, 24},
			{"after", "after measured comment", ServerRange{editor.DiffNew, 2, 2}, 24},
		}); err != nil {
			t.Fatal(err)
		}
		font := float32(12)
		v.SetAnnotationRenderer(func(c *ui.Context, p Placement, width float32) float32 {
			text := ui.Text(c, p.Label).FillWidth().FontSize(font)
			if h := text.Bounds().H; h > 0 {
				return h
			}
			return p.Height
		})
		tt := widgetTester(v, 1000, 2000)
		before := find(t, tt, "after measured comment").Y
		tt.SetSize(500, 2000)
		tt.Frame()
		wrapped := find(t, tt, "after measured comment").Y
		if wrapped <= before+20 {
			t.Fatalf("resize did not remeasure text: %g -> %g", before, wrapped)
		}
		font = 20
		tt.Frame()
		scaled := find(t, tt, "after measured comment").Y
		if scaled <= wrapped+20 {
			t.Fatalf("font size did not remeasure text: %g -> %g", wrapped, scaled)
		}
		tt.SetSize(1000, 2000)
		font = 12
		tt.Frame()
		shrunk := find(t, tt, "after measured comment").Y
		if math.Abs(float64(shrunk-before)) > 1 {
			t.Fatalf("shrinking did not restore geometry: %g -> %g", before, shrunk)
		}
	}
}

func TestNativeContextControlsRemapCommentsAndSelection(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		p := adapterPresentation(t, "@@ -3 +3 @@\n-old\n+new\n@@ -7 +7 @@\n-tail\n+TAIL\n")
		v := NewReadOnlyView(p, layout, true, nil)
		r := ServerRange{editor.DiffNew, 3, 3}
		if err := v.SetSelection(r); err != nil {
			t.Fatal(err)
		}
		if err := v.SetPlacements([]Placement{{"existing", "anchored comment", r, 42}, {"hidden", "expanded comment", ServerRange{editor.DiffOld, 2, 2}, 35}}); err != nil {
			t.Fatal(err)
		}
		tt := widgetTester(v, 900, 1000)
		if tt.HasText("Expand before") || tt.HasText("Expand after") {
			t.Fatal("unverified contents offered expansion")
		}
		source := &goclient.FileContents{Before: "a\nb\nold\nd\ne\nf\ntail\nh\n", After: "a\nb\nnew\nd\ne\nf\nTAIL\nh\n"}
		contents := CompleteContentsFromAPI(source)
		if err := v.SetCompleteContents(contents); err != nil {
			t.Fatal(err)
		}
		contents.Before = "mutated"
		source.After = "mutated"
		tt.Frame()
		if err := tt.Click("Expand before"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if got, _ := v.Selection(); got != r {
			t.Fatalf("selection reanchored: %+v", got)
		}
		find(t, tt, "expanded comment")
		if err := v.Expand(0, 0, 200); err != nil {
			t.Fatal(err)
		}
		if len(v.Presentation().Patch().Hunks) != 1 {
			t.Fatal("verified gap not merged")
		}
		if a, d := v.Stats(); a != 2 || d != 2 {
			t.Fatalf("stored change runs lost: %d %d", a, d)
		}
		v.Collapse()
		tt.Frame()
		if tt.HasText("expanded comment") {
			t.Fatal("collapsed comment attached to another line")
		}
		if got, _ := v.Selection(); got != r {
			t.Fatal("collapsed selection lost")
		}
		if !reflectRunsEqual(p, v.Presentation()) {
			t.Fatal("collapse changed stored runs")
		}
	}
}

func reflectRunsEqual(a, b *Presentation) bool {
	ar, br := adapterRuns(a.patch), adapterRuns(b.patch)
	return slices.EqualFunc(ar, br, func(x, y []string) bool { return slices.Equal(x, y) })
}

func TestNativeKeyboardFocusAndPositionRestoration(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("@@ -40,90 +70,90 @@\n")
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&raw, " line %d\n", i)
	}
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		v := NewReadOnlyView(adapterPresentation(t, raw.String()), layout, false, nil)
		if err := v.SetSelection(ServerRange{editor.DiffNew, 75, 75}); err != nil {
			t.Fatal(err)
		}
		focused := false
		tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill(); focused = v.Focused(c) }, 800, 300)
		v.Focus()
		tt.Frame()
		if !focused {
			t.Fatal("focus request not applied")
		}
		tt.Key(ui.Alt|ui.Shift, ui.KeyDown)
		if r, _ := v.Selection(); r.Start != 75 || r.End != 76 {
			t.Fatalf("focused keyboard range: %+v", r)
		}
		if err := v.ScrollTo(ServerRange{editor.DiffNew, 130, 130}); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saved := v.Position()
		if saved.Y <= 0 || !saved.Focus {
			t.Fatalf("position not captured: %+v", saved)
		}
		if err := v.ScrollTo(ServerRange{editor.DiffNew, 70, 70}); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if v.Position().Y >= saved.Y {
			t.Fatal("scroll-to did not move")
		}
		if err := v.Restore(saved); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if math.Abs(float64(v.Position().Y-saved.Y)) > 1 || !focused {
			t.Fatalf("position/focus not restored: %+v -> %+v", saved, v.Position())
		}
		if err := v.SetDisplay(editor.DiffUnified, true); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !focused {
			t.Fatal("display change lost focus")
		}
		if r, _ := v.Selection(); r.Start != 75 || r.End != 76 {
			t.Fatal("display change lost server selection")
		}
		copy := v.Position()
		if copy.Selection == nil {
			t.Fatal("missing bookmark selection")
		}
		copy.Selection.Start = 1
		if r, _ := v.Selection(); r.Start != 75 {
			t.Fatal("bookmark aliases selection")
		}
	}
}

func TestNativeGutterCallbackUsesServerCoordinates(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		for _, side := range []editor.DiffSide{editor.DiffOld, editor.DiffNew} {
			v := NewReadOnlyView(adapterPresentation(t, sparse), layout, true, nil)
			line := 41
			if side == editor.DiffNew {
				line = 71
			}
			r := ServerRange{side, line, line}
			if err := v.SetPlacements([]Placement{{"note", "gutter anchor", r, 32}}); err != nil {
				t.Fatal(err)
			}
			var calls []ServerRange
			v.SetGutterCommentHandler(func(r ServerRange) { calls = append(calls, r) })
			tt := widgetTester(v, 800, 900)
			x := float32(30)
			if layout == editor.DiffSplit && side == editor.DiffNew {
				x = 430
			}
			note := find(t, tt, "gutter anchor")
			tt.Move(x, note.Y-10)
			tt.Frame()
			if layout == editor.DiffUnified {
				tt.ClickAt(x-20, note.Y-10)
				tt.Frame()
			}
			// Both native hover and selected-line actions name server coordinates.
			if err := tt.Click(commentLabel(r)); err != nil {
				t.Fatal(err, tt.Texts())
			}
			if len(calls) != 1 || calls[0] != r {
				t.Fatalf("gutter callbacks=%+v want=%+v", calls, r)
			}
			v.SetGutterCommentHandler(nil)
			tt.Frame()
			if tt.HasText(commentLabel(r)) {
				t.Fatal("disabled gutter action exposed")
			}
		}
	}
}

func TestNativeOptionsGuardsAndReadonly(t *testing.T) {
	v := NewReadOnlyView(adapterPresentation(t, sparse), editor.DiffSplit, false, nil)
	palette := editor.DefaultTheme(ui.LightTheme())
	options := Options{Layout: editor.DiffUnified, Wrap: true, Inline: editor.DiffInlineChar, Font: ui.Font{Family: "monospace", Size: 18}, TabSize: 8, Language: editor.LanguageNamed("go"), Theme: palette}
	if err := v.SetOptions(options); err != nil {
		t.Fatal(err)
	}
	palette.Background = ui.Hex("#abcdef")
	if v.state.Theme.Background == palette.Background {
		t.Fatal("theme aliases caller")
	}
	tt := widgetTester(v, 800, 700)
	old, nw := v.state.Old.String(), v.state.New.String()
	if err := tt.Click("New text"); err != nil {
		t.Fatal(err)
	}
	tt.Type("mutation")
	tt.Command("paste")
	if old != v.state.Old.String() || nw != v.state.New.String() || v.state.Editable || v.state.HunkActions {
		t.Fatal("readonly boundary lost")
	}
	for _, inline := range []editor.DiffInline{editor.DiffInlineWordAlt, editor.DiffInlineWord, editor.DiffInlineChar, editor.DiffInlineNone} {
		options.Inline = inline
		options.Language = nil
		if err := v.SetOptions(options); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if v.state.Old.Language() != nil || v.state.New.Language() != nil {
			t.Fatal("plain text did not clear syntax")
		}
		if a, d := v.Stats(); a != 2 || d != 2 {
			t.Fatal("display settings recomputed stored diff")
		}
	}
	for _, bad := range []Options{{Layout: 255}, {Inline: 255}, {Indicators: 255}, {TabSize: -1}, {Font: ui.Font{Size: float32(math.NaN())}}, {LineHeight: float32(math.Inf(1))}} {
		if err := v.SetOptions(bad); !errors.Is(err, ErrRange) {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestNativeCommentAnchorSurvivesContextAndHeightChanges(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		for _, side := range []editor.DiffSide{editor.DiffOld, editor.DiffNew} {
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
			p := adapterPresentation(t, "@@ -50 +50 @@\n-old50\n+new50\n@@ -90 +90 @@\n-old90\n+new90\n")
			v := NewReadOnlyView(p, layout, true, nil)
			notes := []Placement{{"above", "above anchor", ServerRange{side, 50, 50}, 40}, {"anchor", "viewport anchor", ServerRange{side, 90, 90}, 40}}
			if err := v.SetPlacements(notes); err != nil {
				t.Fatal(err)
			}
			if err := v.SetCompleteContents(&CompleteContents{before.String(), after.String()}); err != nil {
				t.Fatal(err)
			}
			if err := v.Expand(0, 30, 0); err != nil {
				t.Fatal(err)
			}
			if err := v.Expand(1, 0, 30); err != nil {
				t.Fatal(err)
			}
			heights := map[string]float32{"above": 40, "anchor": 40}
			v.SetAnnotationRenderer(func(c *ui.Context, p Placement, w float32) float32 { ui.Text(c, p.Label); return heights[p.Key] })
			tt := widgetTester(v, 800, 300)
			if err := v.ScrollTo(ServerRange{side, 90, 90}); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			pos := v.Position()
			a := find(t, tt, "viewport anchor")
			pos.Y += a.Y - 70
			if err := v.Restore(pos); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			initial := find(t, tt, "viewport anchor").Y
			// Show and merge all context between stored hunks. Comments keep server
			// identity and the viewport keeps its measured native anchor.
			if err := v.Expand(0, 0, 200); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			expanded := find(t, tt, "viewport anchor").Y
			if math.Abs(float64(expanded-initial)) > 1 {
				t.Fatalf("layout=%d side=%d context anchor %g -> %g", layout, side, initial, expanded)
			}
			heights["above"] = 100
			tt.Frame()
			grown := find(t, tt, "viewport anchor").Y
			if math.Abs(float64(grown-expanded)) > 1 {
				t.Fatalf("height anchor %g -> %g", expanded, grown)
			}
			if r := v.Mapping()[1].Placement.Range; r != (ServerRange{side, 90, 90}) {
				t.Fatal("comment reanchored")
			}
		}
	}
}

func TestNativeMetadataHasNoCounterfeitTextEditor(t *testing.T) {
	for _, raw := range []string{
		"diff --git a/x b/x\nBinary files a/x and b/x differ\n",
		"diff --git a/x b/y\nsimilarity index 100%\nrename from x\nrename to y\n",
	} {
		v := NewReadOnlyView(adapterPresentation(t, raw), editor.DiffSplit, false, nil)
		tt := widgetTester(v, 800, 300)
		if tt.HasText("Old text") || tt.HasText("New text") {
			t.Fatal("metadata-only patch created empty code editor")
		}
		if _, ok := v.Selection(); ok {
			t.Fatal("metadata selected a line")
		}
	}
}

func TestNativeRejectsInvalidMeasurementWithoutChangingPlacement(t *testing.T) {
	v := NewReadOnlyView(adapterPresentation(t, sparse), editor.DiffSplit, false, nil)
	notes := []Placement{{"note", "invalid measurement", ServerRange{editor.DiffNew, 71, 71}, 40}}
	if err := v.SetPlacements(notes); err != nil {
		t.Fatal(err)
	}
	v.SetAnnotationRenderer(func(c *ui.Context, p Placement, w float32) float32 { ui.Text(c, p.Label); return float32(math.NaN()) })
	tt := widgetTester(v, 800, 500)
	if !errors.Is(v.Error(), ErrRange) {
		t.Fatal("invalid measurement not reported")
	}
	find(t, tt, "invalid measurement")
	if got := v.Mapping()[0].Placement; got != notes[0] {
		t.Fatal("measurement changed placement")
	}
	if tt.FramePending() {
		t.Fatal("invalid measurement requested infinite frames")
	}
}

func TestNativeCodeFocusRestoredAcrossLayoutChange(t *testing.T) {
	v := NewReadOnlyView(adapterPresentation(t, sparse), editor.DiffSplit, false, nil)
	focused := false
	tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill(); focused = v.Focused(c) }, 800, 500)
	tt.ClickAt(100, 42)
	if !focused {
		t.Fatal("native editor not focused")
	}
	if err := v.SetDisplay(editor.DiffUnified, true); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !focused {
		t.Fatal("layout change lost diff focus")
	}
}

func TestNativeNewlineOnlyChangesKeepStoredRunAndMarkers(t *testing.T) {
	for _, tc := range []struct {
		raw, before, after string
		side               editor.DiffSide
	}{
		{"@@ -1 +1 @@\n-same\n\\ No newline at end of file\n+same\n", "same", "same\n", editor.DiffOld},
		{"@@ -1 +1 @@\n-same\n+same\n\\ No newline at end of file\n", "same\n", "same", editor.DiffNew},
	} {
		for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
			p := adapterPresentation(t, tc.raw)
			v := NewReadOnlyView(p, layout, true, nil)
			if err := v.SetCompleteContents(&CompleteContents{tc.before, tc.after}); err != nil {
				t.Fatal(err)
			}
			tt := widgetTester(v, 800, 400)
			if a, d := v.Stats(); a != 1 || d != 1 {
				t.Fatal("newline-only stored run disappeared")
			}
			side := "old"
			if tc.side == editor.DiffNew {
				side = "new"
			}
			find(t, tt, "No newline at end of file ("+side+")")
			if got, err := p.Patch().Apply(tc.before); err != nil || got != tc.after {
				t.Fatalf("raw newline bytes lost: %q %v", got, err)
			}
		}
	}
}

func TestNativeExpandedFinalUnterminatedContextHasEOFIndicators(t *testing.T) {
	p := adapterPresentation(t, "@@ -1 +1 @@\n-old\n+new\n")
	v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
	if err := v.SetCompleteContents(&CompleteContents{"old\ntail", "new\ntail"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Expand(0, 0, 1); err != nil {
		t.Fatal(err)
	}
	tt := widgetTester(v, 800, 500)
	find(t, tt, "No newline at end of file (old)")
	find(t, tt, "No newline at end of file (new)")
	if len(v.Presentation().EOFMarkers()) != 0 {
		t.Fatal("expanded metadata changed stored raw markers")
	}
}
