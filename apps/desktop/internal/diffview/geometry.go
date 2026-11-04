package diffview

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Position is a detached UI-thread bookmark. Selection remains in server
// coordinates even when hidden by a later collapse. Focus is opt-in on restore.
type Position struct {
	X, Y      float32
	Selection *ServerRange
	Focus     bool
	raw       string
	anchor    *scrollAnchor
	sourceY   float32
}

type geometryAnchor struct {
	handle        ui.Handle
	y             float32 // position in scroll content, measured from the native widget
	valid         bool
	width, height float32
}

type scrollAnchor struct {
	key      string
	offset   float32
	attempts int
	stable   int
}

func (v *ReadOnlyView) Focus()                     { v.handle.Focus() }
func (v *ReadOnlyView) Focused(c *ui.Context) bool { return v.handle.FocusWithin(c) }

func (v *ReadOnlyView) Position() Position {
	p := Position{X: v.scroll.X, Y: v.scroll.Y, Focus: v.focused, raw: v.presentation.raw, anchor: v.closestAnchor(), sourceY: v.scroll.Y}
	if v.selection != nil {
		copy := *v.selection
		p.Selection = &copy
	}
	return p
}

func (v *ReadOnlyView) Restore(p Position) error {
	if p.raw != "" && p.raw != v.presentation.raw {
		return ErrPatch
	}
	if !finiteNonnegative(p.X) || !finiteNonnegative(p.Y) {
		return ErrRange
	}
	if p.Selection != nil {
		if err := validateRange(*p.Selection); err != nil {
			return err
		}
		if err := v.SetSelection(*p.Selection); err != nil {
			return err
		}
	} else {
		v.ClearSelection()
	}
	v.scroll.X, v.scroll.Y = p.X, p.Y
	v.pendingAnchor = nil
	if p.anchor != nil {
		copy := *p.anchor
		copy.offset += p.sourceY - p.Y
		v.pendingAnchor = &copy
	}
	if p.Focus {
		v.Focus()
	}
	return nil
}

func (v *ReadOnlyView) ScrollTo(r ServerRange) error {
	mapped, err := v.presentation.Remap(r)
	if err != nil || !mapped.Visible {
		return ErrRange
	}
	copy := r
	v.reveal, v.pendingAnchor = &copy, nil
	v.bindAnnotations()
	return nil
}

func rangeKey(r ServerRange) string { return fmt.Sprintf("line:%d:%d:%d", r.Side, r.Start, r.End) }

func (v *ReadOnlyView) addRangeAnchor(r ServerRange) {
	mapped, err := v.presentation.Remap(r)
	if err != nil || !mapped.Visible {
		return
	}
	key := rangeKey(r)
	for _, a := range v.state.Annotations {
		if note, ok := a.Key.(viewNote); ok && note.key == key {
			return
		}
	}
	v.state.Annotations = append(v.state.Annotations, editor.DiffAnnotation{
		Side: r.Side, Line: mapped.Compact.End, Key: viewNote{key: key, anchor: true, server: r},
	})
}

func (v *ReadOnlyView) geometryFor(key string) *geometryAnchor {
	v.activeGeometry[key] = true
	if g := v.geometry[key]; g != nil {
		return g
	}
	g := &geometryAnchor{}
	v.geometry[key] = g
	return g
}

func (v *ReadOnlyView) closestAnchor() *scrollAnchor {
	var best *scrollAnchor
	distance := float32(math.MaxFloat32)
	bestRank := 5
	for key, g := range v.geometry {
		if !g.valid {
			continue
		}
		rank := 0
		if strings.HasPrefix(key, "hunk:") {
			rank = 1
		}
		if g.y < v.scroll.Y || g.y > v.scroll.Y+v.viewportHeight {
			rank += 2
		}
		if rank > bestRank {
			continue
		}
		d := float32(math.Abs(float64(g.y - v.scroll.Y)))
		if rank < bestRank || d < distance || d == distance && (best == nil || key < best.key) {
			bestRank, distance, best = rank, d, &scrollAnchor{key: key, offset: g.y - v.scroll.Y}
		}
	}
	return best
}

func (v *ReadOnlyView) captureAnchor() {
	if v.pendingAnchor == nil {
		v.pendingAnchor = v.closestAnchor()
	}
}

// updateGeometry runs after native layout and painting. Only public Handle
// bounds and ScrollState are read; MyGo's editor buffers/geometry stay private.
func (v *ReadOnlyView) updateGeometry(c *ui.Context, p *ui.Painter, viewport ui.Rect) {
	v.viewportHeight = viewport.H
	for key, g := range v.geometry {
		if !v.activeGeometry[key] {
			delete(v.geometry, key)
			continue
		}
		r := g.handle.Bounds(c)
		if r.W > 0 && (r.W != g.width || r.H != g.height) {
			p.AnimationFrame()
		}
		g.width, g.height = r.W, r.H
		g.valid = r.W > 0
		if g.valid {
			g.y = r.Y - viewport.Y + v.scroll.Y
		}
	}
	target := v.scroll.Y
	if v.reveal != nil {
		if g := v.geometry[rangeKey(*v.reveal)]; g != nil && g.valid {
			if g.y < target {
				target = max(0, g.y-max(24, v.state.Font.Size*v.state.LineHeight))
			}
			if g.y > target+viewport.H {
				target = g.y - viewport.H + min(24, viewport.H)
			}
			v.reveal = nil
		}
	} else if v.pendingAnchor != nil {
		if g := v.geometry[v.pendingAnchor.key]; g != nil && g.valid {
			target = g.y - v.pendingAnchor.offset
			v.pendingAnchor.attempts++
		} else {
			// SetPatch rebuilds native geometry in more than one pass. Wait for the
			// replacement annotation's committed bounds, rather than using its old ID.
			v.pendingAnchor.attempts++
			if v.pendingAnchor.attempts < 4 {
				p.AnimationFrame()
			} else {
				v.pendingAnchor = nil
			}
		}
	}
	target = max(0, min(target, v.scroll.MaxY))
	if v.pendingAnchor != nil {
		if math.Abs(float64(target-v.scroll.Y)) <= 0.25 {
			v.pendingAnchor.stable++
		} else {
			v.pendingAnchor.stable = 0
		}
		if v.pendingAnchor.stable >= 2 || v.pendingAnchor.attempts >= 8 {
			v.pendingAnchor = nil
		} else {
			p.AnimationFrame()
		}
	}
	if math.Abs(float64(target-v.scroll.Y)) > 0.25 {
		v.scroll.Y = target
		p.AnimationFrame()
	}
}

// SetAnnotationRenderer draws app-owned controls and accepts their measured
// height. Measurement settles on the next native frame, outside MyGo's build.
func (v *ReadOnlyView) SetAnnotationRenderer(render func(*ui.Context, Placement, float32) float32) {
	v.renderAnnotation = render
}

func (v *ReadOnlyView) drawAnnotation(c *ui.Context, a editor.DiffAnnotation) {
	note, ok := a.Key.(viewNote)
	if !ok {
		return
	}
	box := ui.Box(c).FillWidth()
	if note.key != "" {
		box.Bind(&v.geometryFor(note.key).handle)
	}
	if note.anchor {
		if v.externalScroll && v.reveal != nil && note.server == *v.reveal {
			box.ScrollIntoView()
			v.reveal = nil
		}
		if v.gutterComment != nil && v.selection != nil && note.server == *v.selection {
			label := commentLabel(note.server)
			button := ui.ButtonBase(c).Absolute().Left(-22).Top(-max(21, v.options.Font.Size*1.5)+2).Size(18, 18).KeepFocus().Label(label).Tooltip(label).Cursor(ui.CursorPointer)
			button.Children(func() { ui.Text(c, "+").TextColor(c.Theme().Accent) })
			if button.Clicked() {
				v.gutterComment(note.server)
			}
		}
		return
	}
	box.Children(func() {
		if note.eof || v.renderAnnotation == nil {
			ui.Text(c, note.label).TextColor(c.Theme().TextMuted)
			return
		}
		key := strings.TrimPrefix(note.key, "comment:")
		for _, placement := range v.placements {
			if placement.Key != key {
				continue
			}
			measured := v.renderAnnotation(c, placement, box.Bounds().W)
			if !finiteNonnegative(measured) || measured == 0 {
				v.lastError = ErrRange
				return
			}
			if math.Abs(float64(measured-a.Height)) > 0.25 {
				v.pendingHeights[key] = measured
				c.AnimationFrame()
			}
			return
		}
	})
}

func (v *ReadOnlyView) SetGutterCommentHandler(fn func(ServerRange)) {
	v.gutterComment = fn
	v.state.OnGutterButton = nil
	if fn == nil || v.selectionDisabled || v.options.Layout == editor.DiffUnified {
		return
	}
	v.state.RenderGutterButton = func(c *ui.Context, btn ui.Element, r editor.DiffLineRange) {
		editor.DefaultGutterButton(c, btn, r)
		if stored, err := v.presentation.ServerSelection(r); err == nil {
			btn.Label(commentLabel(stored)).Tooltip(commentLabel(stored))
		} else {
			btn.Hidden(true)
		}
	}
	v.state.OnGutterButton = func(r editor.DiffLineRange) {
		stored, err := v.presentation.ServerSelection(r)
		if err == nil && v.gutterComment != nil {
			v.gutterComment(stored)
		}
	}
}

func (v *ReadOnlyView) drawSeparator(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
	// SetPatch supplies headers only. Never invoke built-in full-buffer expansion.
	index := -1
	for i, h := range v.presentation.patch.Hunks {
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
		if h.Section != "" {
			header += " " + h.Section
		}
		if header == s.Header {
			index = i
			break
		}
	}
	if index < 0 {
		editor.DefaultSeparator(c, sep, s)
		return
	}
	key := fmt.Sprintf("hunk:%d", v.presentation.groups[index].first)
	sep.Bind(&v.geometryFor(key).handle)
	sep.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, s.Header).Font(s.Font().Family).FontSize(max(11, s.Font().Size-1)).TextColor(c.Theme().TextMuted).SingleLine().Shrink(1)
			before, after, ok := v.Context(index)
			if !ok {
				return
			}
			if before > 0 && ui.Button(c, "Expand before").Clicked() {
				v.pendingContext = func() error { return v.Expand(index, min(before, MaxContextStep), 0) }
				c.AnimationFrame()
			}
			if after > 0 && ui.Button(c, "Expand after").Clicked() {
				v.pendingContext = func() error { return v.Expand(index, 0, min(after, MaxContextStep)) }
				c.AnimationFrame()
			}
		})
	})
}

func commentLabel(r ServerRange) string {
	side := "new"
	if r.Side == editor.DiffOld {
		side = "old"
	}
	if r.Start == r.End {
		return fmt.Sprintf("Comment on %s line %d", side, r.Start)
	}
	return fmt.Sprintf("Comment on %s lines %d-%d", side, r.Start, r.End)
}
