package diffview

import (
	"math"
	"reflect"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

// Options changes rendering only. It never reparses text or computes a new diff.
// Language nil deliberately disables syntax. Theme nil follows the window theme.
type Options struct {
	Layout                      editor.DiffLayout
	Wrap                        bool
	Inline                      editor.DiffInline
	Language                    *editor.Language
	Theme                       *editor.Theme
	Font                        ui.Font
	LineHeight                  float32
	TabSize                     int
	Indicators                  editor.DiffIndicators
	NoBackground, NoLineNumbers bool
}

func finiteNonnegative(n float32) bool {
	return n >= 0 && !math.IsInf(float64(n), 0) && !math.IsNaN(float64(n))
}

func (v *ReadOnlyView) SetOptions(o Options) error {
	if o.Layout > editor.DiffUnified || o.Inline > editor.DiffInlineNone || o.Indicators > editor.DiffNoIndicators ||
		!finiteNonnegative(o.Font.Size) || !finiteNonnegative(o.Font.LineHeight) || !finiteNonnegative(o.LineHeight) || o.TabSize < 0 {
		return ErrRange
	}
	// Theme is a value palette. Detach it so caller mutation cannot change geometry
	// or rendering behind this widget. Language is a framework-owned language.
	if o.Theme != nil {
		copy := *o.Theme
		o.Theme = &copy
	}
	a, b := o, v.options
	a.Theme, b.Theme = nil, nil
	if a == b && reflect.DeepEqual(o.Theme, v.options.Theme) {
		return nil
	}
	v.captureAnchor()
	if v.focused && o.Layout != v.options.Layout {
		v.Focus()
	}
	v.options = o
	v.applyOptions()
	v.bindAnnotations()
	return nil
}

func (v *ReadOnlyView) applyOptions() {
	o := v.options
	v.state.Layout, v.state.Wrap, v.state.Inline = o.Layout, o.Wrap, o.Inline
	v.state.Theme, v.state.Font, v.state.TabSize = o.Theme, o.Font, o.TabSize
	v.state.LineHeight = o.LineHeight
	if v.state.LineHeight == 0 {
		v.state.LineHeight = 1.5
	}
	v.state.Indicators, v.state.NoBackground, v.state.NoLineNumbers = o.Indicators, o.NoBackground, o.NoLineNumbers
	v.state.Old.SetLanguage(o.Language)
	v.state.New.SetLanguage(o.Language)
	v.SetGutterCommentHandler(v.gutterComment)
}

func CompleteContentsFromAPI(c *goclient.FileContents) *CompleteContents {
	if c == nil {
		return nil
	}
	return &CompleteContents{Before: c.Before, After: c.After}
}

func (v *ReadOnlyView) Presentation() *Presentation { return v.presentation }

func (v *ReadOnlyView) SetCompleteContents(c *CompleteContents) error {
	if c == nil {
		v.contents = nil
		v.contentLines = 0
		return nil
	}
	if v.contents != nil && *v.contents == *c {
		return nil
	}
	lines, err := v.presentation.verifyContents(c)
	if err != nil {
		return err
	}
	copy := *c
	v.contents, v.contentLines = &copy, len(lines[0])
	return nil
}

func (v *ReadOnlyView) Expand(hunk, before, after int) error {
	p, err := v.presentation.Expand(hunk, before, after, v.contents)
	if err != nil {
		return err
	}
	return v.Replace(p)
}

func (v *ReadOnlyView) Collapse() { _ = v.Replace(v.presentation.Collapse()) }

// Context reports how many more verified lines can be shown around a displayed
// hunk. It is false without admitted complete contents, including metadata files.
func (v *ReadOnlyView) Context(hunk int) (before, after int, ok bool) {
	if v.contents == nil || hunk < 0 || hunk >= len(v.presentation.groups) {
		return 0, 0, false
	}
	p := v.presentation
	g := p.groups[hunk]
	first, last := p.stored.Hunks[g.first], p.stored.Hunks[g.last]
	start, _ := boundary(first.OldStart, first.OldLines)
	end, _ := boundary(last.OldStart, last.OldLines)
	end += last.OldLines
	prevEnd, nextStart := 0, v.contentLines
	if g.first > 0 {
		prev := p.stored.Hunks[g.first-1]
		prevEnd, _ = boundary(prev.OldStart, prev.OldLines)
		prevEnd += prev.OldLines
	}
	if g.last+1 < len(p.stored.Hunks) {
		next := p.stored.Hunks[g.last+1]
		nextStart, _ = boundary(next.OldStart, next.OldLines)
	}
	return start - prevEnd - p.margins[g.first].before, nextStart - end - p.margins[g.last].after, true
}

// Error reports a deferred native context-control or measurement error. Failed
// actions retain the previous presentation, selection and placement coordinates.
func (v *ReadOnlyView) Error() error { return v.lastError }
