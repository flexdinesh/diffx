package diffview

import (
	"math"
	"reflect"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Placement is a server-resolved range. No local text search or reanchoring.
// Hidden placements remain in Mapping with Visible=false for sidebar rendering.
type Placement struct {
	Key, Label string
	Range      ServerRange
	Height     float32
}
type MappedPlacement struct {
	Placement Placement
	Range     MappedRange
}

type viewNote struct {
	key, label string
	anchor     bool
	server     ServerRange
	eof        bool
	side       editor.DiffSide
}

// ReadOnlyView encapsulates MyGo's mutable state and buffers. Single UI-thread
// ownership; Presentation snapshots can be shared independently.
type ReadOnlyView struct {
	state             editor.DiffState
	presentation      *Presentation
	selection         *ServerRange
	anchor, cursor    int
	placements        []Placement
	onSelection       func(ServerRange, bool)
	options           Options
	contents          *CompleteContents
	renderAnnotation  func(*ui.Context, Placement, float32) float32
	pendingHeights    map[string]float32
	measuredHeights   map[string]float32
	contentLines      int
	gutterComment     func(ServerRange)
	scroll            ui.ScrollState
	handle            ui.Handle
	geometry          map[string]*geometryAnchor
	activeGeometry    map[string]bool
	viewportHeight    float32
	pendingAnchor     *scrollAnchor
	reveal            *ServerRange
	focused           bool
	pendingContext    func() error
	lastError         error
	externalScroll    bool
	selectionDisabled bool
}

// p must be a successfully constructed Presentation, owned by this file view.
func NewReadOnlyView(p *Presentation, layout editor.DiffLayout, wrap bool, onSelection func(ServerRange, bool)) *ReadOnlyView {
	v := &ReadOnlyView{presentation: p, onSelection: onSelection}
	v.state.Layout, v.state.Wrap = layout, wrap
	v.state.LineHeight, v.state.LineSelection = 1.5, true
	v.options = Options{Layout: layout, Wrap: wrap, LineHeight: 1.5, Language: editor.LanguageFor(p.patch.NewName)}
	v.geometry = make(map[string]*geometryAnchor)
	v.activeGeometry = make(map[string]bool)
	v.pendingHeights = make(map[string]float32)
	v.measuredHeights = make(map[string]float32)
	v.state.RenderAnnotation = v.drawAnnotation
	v.state.RenderSeparator = v.drawSeparator
	v.state.OnLineSelection = func(r editor.DiffLineRange) {
		if v.selectionDisabled {
			return
		}
		stored, err := v.presentation.ServerSelection(r)
		if err != nil {
			v.restoreSelection()
			return
		}
		anchor, cursor := stored.Start, stored.End
		if v.selection != nil && v.selection.Side == stored.Side && stored.Start != v.selection.Start && stored.End == v.selection.End {
			anchor, cursor = stored.End, stored.Start
		}
		v.selection, v.anchor, v.cursor = &stored, anchor, cursor
		v.bindAnnotations()
		if v.onSelection != nil {
			v.onSelection(stored, true)
		}
	}
	v.rebind()
	return v
}

// Draw returns a native scroll container. The caller assigns its size.
func (v *ReadOnlyView) Draw(c *ui.Context) ui.Element {
	return v.draw(c, false)
}

// DrawContent fits this file inside its parent's scrolling file list. It does
// not create another scroll container or compete for wheel input.
func (v *ReadOnlyView) DrawContent(c *ui.Context) ui.Element {
	return v.draw(c, true)
}

func (v *ReadOnlyView) draw(c *ui.Context, external bool) ui.Element {
	v.externalScroll = external
	if v.pendingContext != nil {
		action := v.pendingContext
		v.pendingContext = nil
		v.lastError = action()
	}
	if len(v.pendingHeights) > 0 {
		v.captureAnchor()
		for key, height := range v.pendingHeights {
			v.measuredHeights[key] = height
		}
		clear(v.pendingHeights)
		v.bindAnnotations()
	}
	// MyGo toggles a single-line click off without invoking OnLineSelection.
	if v.selection != nil {
		mapped, _ := v.presentation.Remap(*v.selection)
		if _, selected := v.state.SelectedLines(); mapped.Visible && !selected {
			v.ClearSelection()
			if v.onSelection != nil {
				v.onSelection(ServerRange{}, false)
			}
		}
	}
	clear(v.activeGeometry)
	var root ui.Element
	if external {
		root = ui.Column(c).Shrink(0).FillWidth()
	} else {
		root = ui.Scroll(c).TrackScroll(&v.scroll)
	}
	root = root.Bind(&v.handle).Focusable().Label("Diff lines")
	root.Children(func() {
		// Metadata/binary/message-only chrome belongs to the app. Do not create
		// a code editor whose synthetic empty buffer looks like a file line.
		if len(v.presentation.patch.Hunks) > 0 {
			editor.DiffView(c, &v.state).FillWidth().Shrink(0)
		}
	})
	v.focused = root.FocusWithin()
	for _, key := range []ui.Key{ui.KeyUp, ui.KeyDown} {
		delta := 1
		if key == ui.KeyUp {
			delta = -1
		}
		if root.Shortcut(ui.Alt, key) {
			_ = v.MoveSelection(delta, false)
		}
		if root.Shortcut(ui.Alt|ui.Shift, key) {
			_ = v.MoveSelection(delta, true)
		}
	}
	if !external {
		root.DrawOver(func(p *ui.Painter, r ui.Rect) { v.updateGeometry(c, p, r) })
	}
	return root
}

// SetLineSelectionEnabled controls review line selection independently from
// native text selection/copy. Commit views retain text access without review
// ranges or gutter actions.
func (v *ReadOnlyView) SetLineSelectionEnabled(enabled bool) {
	if v.selectionDisabled == !enabled {
		return
	}
	v.selectionDisabled = !enabled
	v.state.LineSelection = enabled
	if !enabled {
		v.ClearSelection()
		if v.onSelection != nil {
			v.onSelection(ServerRange{}, false)
		}
	}
	v.SetGutterCommentHandler(v.gutterComment)
}

func (v *ReadOnlyView) Stats() (int, int)        { return v.state.Stats() }
func (v *ReadOnlyView) Hunks() []editor.DiffHunk { return slices.Clone(v.state.Hunks()) }
func (v *ReadOnlyView) Selection() (ServerRange, bool) {
	if v.selection == nil {
		return ServerRange{}, false
	}
	return *v.selection, true
}

func (v *ReadOnlyView) SetSelection(r ServerRange) error {
	if v.selectionDisabled {
		return ErrRange
	}
	mapped, err := v.presentation.Remap(r)
	if err != nil {
		return err
	}
	copy := mapped.Stored
	v.selection = &copy
	v.anchor, v.cursor = r.Start, r.End
	v.bindAnnotations()
	v.restoreSelection()
	return nil
}

func (v *ReadOnlyView) ClearSelection() {
	v.selection = nil
	v.state.SetSelectedLines(editor.DiffLineRange{}, false)
	v.bindAnnotations()
}

func (v *ReadOnlyView) SetDisplay(layout editor.DiffLayout, wrap bool) error {
	if layout != editor.DiffSplit && layout != editor.DiffUnified {
		return ErrRange
	}
	options := v.options
	options.Layout, options.Wrap = layout, wrap
	return v.SetOptions(options)
}

// Keyboard range controls call this from the app's focused shortcut handler.
// delta is +/-1. Sparse holes and >200-line ranges fail without changing state.
func (v *ReadOnlyView) MoveSelection(delta int, extend bool) error {
	if v.selectionDisabled || v.selection == nil || delta != -1 && delta != 1 {
		return ErrRange
	}
	if delta > 0 && v.cursor == math.MaxInt {
		return ErrRange
	}
	cursor, anchor := v.cursor+delta, v.anchor
	if !extend {
		anchor = cursor
	}
	r := ServerRange{v.selection.Side, min(anchor, cursor), max(anchor, cursor)}
	mapped, err := v.presentation.Remap(r)
	if err != nil || !mapped.Visible {
		return ErrRange
	}
	v.selection, v.anchor, v.cursor = &r, anchor, cursor
	v.reveal = &r
	v.bindAnnotations()
	v.restoreSelection()
	if v.onSelection != nil {
		v.onSelection(r, true)
	}
	return nil
}

func (v *ReadOnlyView) SetPlacements(placements []Placement) error {
	seen := make(map[string]bool)
	for _, placement := range placements {
		if _, err := v.presentation.Remap(placement.Range); err != nil {
			return err
		}
		if placement.Key == "" || seen[placement.Key] || placement.Height <= 0 || math.IsNaN(float64(placement.Height)) || math.IsInf(float64(placement.Height), 0) {
			return ErrRange
		}
		seen[placement.Key] = true
	}
	if reflect.DeepEqual(v.placements, placements) {
		return nil
	}
	v.captureAnchor()
	v.placements = slices.Clone(placements)
	clear(v.pendingHeights)
	clear(v.measuredHeights)
	v.bindAnnotations()
	return nil
}

func (v *ReadOnlyView) Mapping() []MappedPlacement {
	out := make([]MappedPlacement, len(v.placements))
	for i, placement := range v.placements {
		mapped, _ := v.presentation.Remap(placement.Range)
		out[i] = MappedPlacement{placement, mapped}
	}
	return out
}

func (v *ReadOnlyView) Replace(p *Presentation) error {
	if p == nil || p.raw != v.presentation.raw {
		return ErrPatch
	}
	if p == v.presentation {
		return nil
	}
	v.captureAnchor()
	v.presentation = p
	v.rebind()
	return nil
}

func (v *ReadOnlyView) restoreSelection() {
	v.state.SetSelectedLines(editor.DiffLineRange{}, false)
	if v.selection != nil {
		mapped, _ := v.presentation.Remap(*v.selection)
		v.state.SetSelectedLines(mapped.Compact, mapped.Visible)
	}
}

func (v *ReadOnlyView) rebind() {
	v.state.SetPatch(v.presentation.patch)
	v.applyOptions()
	v.state.Editable, v.state.HunkActions, v.state.FitContent = false, false, true
	v.state.LineSelection = !v.selectionDisabled
	v.bindAnnotations()
	v.restoreSelection()
}

func (v *ReadOnlyView) bindAnnotations() {
	v.state.Annotations = nil
	if v.reveal != nil {
		v.addRangeAnchor(*v.reveal)
	}
	if v.selection != nil {
		v.addRangeAnchor(*v.selection)
	}
	for _, placement := range v.Mapping() {
		if placement.Range.Visible {
			p := placement.Placement
			height := p.Height
			if measured, ok := v.measuredHeights[p.Key]; ok {
				height = measured
			}
			v.state.Annotations = append(v.state.Annotations, editor.DiffAnnotation{Side: p.Range.Side,
				Line: placement.Range.Compact.End, Height: height, Key: viewNote{key: "comment:" + p.Key, label: p.Label, server: p.Range}})
		}
	}
	_, markers, _ := indexPatch(v.presentation.patch)
	for _, marker := range markers {
		if line, ok := v.presentation.CompactLine(marker.Side, marker.Line); ok {
			side := "old"
			if marker.Side == editor.DiffNew {
				side = "new"
			}
			v.state.Annotations = append(v.state.Annotations, editor.DiffAnnotation{Side: marker.Side,
				Line: line, Height: max(24, v.options.Font.Size*1.5), Key: viewNote{eof: true, side: marker.Side, label: "No newline at end of file (" + side + ")"}})
		}
	}
	v.restoreSelection()
}
