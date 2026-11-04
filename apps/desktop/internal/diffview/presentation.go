package diffview

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

const MaxSelectionLines = 200
const MaxContextStep = 200
const eofText = `\ No newline at end of file`

var (
	ErrPatch     = errors.New("invalid presentation patch")
	ErrContents  = errors.New("matching complete contents required")
	ErrRange     = errors.New("invalid or noncontiguous line range")
	ErrExpansion = errors.New("invalid context expansion")
)

// ServerRange is one-based and inclusive. The controller translates server
// deletions/additions to DiffOld/DiffNew; it retains all server identities.
type ServerRange struct {
	Side       editor.DiffSide
	Start, End int
}

type EOFMarker struct {
	Side editor.DiffSide
	Line int
}

// A non-nil CompleteContents means BOTH sides are complete, even when empty.
// Request ownership, snapshot identity, fetching and cancellation are controller
// responsibilities. Verification below additionally checks the exact bytes.
type CompleteContents struct{ Before, After string }

type coordinateIndex struct {
	lines   []int
	inverse map[int]int
}

type contextMargin struct{ before, after int }
type hunkGroup struct{ first, last int }

// Presentation is immutable after construction. No methods return aliased slices
// or maps. Patch returns a detached rendering value, never the stored identity.
type Presentation struct {
	raw           string
	stored, patch editor.FilePatch
	indices       [2]coordinateIndex
	markers       []EOFMarker
	margins       []contextMargin
	groups        []hunkGroup
}

func NewPresentation(raw string) (*Presentation, error) {
	files, err := editor.ParsePatch(raw)
	if err != nil || len(files) != 1 {
		return nil, fmt.Errorf("%w: parse: %v, files=%d", ErrPatch, err, len(files))
	}
	p := files[0]
	if err := checkRawHunks(raw, p); err != nil {
		return nil, err
	}
	indices, markers, err := indexPatch(p)
	if err != nil {
		return nil, err
	}
	groups := make([]hunkGroup, len(p.Hunks))
	for i := range groups {
		groups[i] = hunkGroup{i, i}
	}
	return &Presentation{raw: raw, stored: p, patch: p, indices: indices,
		markers: markers, margins: make([]contextMargin, len(p.Hunks)), groups: groups}, nil
}

// ParsePatch permits unconsumed patch-like lines and normalizes unprefixed blank
// rows. Reject both rather than silently dropping changes or inventing context.
func checkRawHunks(raw string, p editor.FilePatch) error {
	lines := strings.Split(raw, "\n")
	hunk := 0
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if strings.HasPrefix(line, "@@") {
			if hunk >= len(p.Hunks) {
				return fmt.Errorf("%w: malformed header", ErrPatch)
			}
			for _, row := range p.Hunks[hunk].Lines {
				i++
				if i >= len(lines) || row != lines[i] || row == "" {
					return fmt.Errorf("%w: malformed hunk row", ErrPatch)
				}
			}
			hunk++
			continue
		}
		if line != "" && strings.ContainsRune(" +-\\", rune(line[0])) {
			if hunk == 0 && (strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")) {
				continue
			}
			return fmt.Errorf("%w: unconsumed hunk row", ErrPatch)
		}
	}
	return nil
}

// boundary is the number of lines preceding a hunk, including zero-count
// insertion/deletion anchors. Subtraction form avoids overflowing start+count.
func boundary(start, count int) (int, error) {
	if start < 0 || count < 0 || count > 0 && start == 0 {
		return 0, ErrPatch
	}
	b := start
	if count > 0 {
		b--
	}
	if count > math.MaxInt-b {
		return 0, ErrPatch
	}
	return b, nil
}

func indexPatch(p editor.FilePatch) ([2]coordinateIndex, []EOFMarker, error) {
	var indices [2]coordinateIndex
	if p.Binary && len(p.Hunks) > 0 {
		return indices, nil, fmt.Errorf("%w: binary patch with text hunks", ErrPatch)
	}
	for side := range indices {
		indices[side].inverse = make(map[int]int)
	}
	var markers []EOFMarker
	var previousEnd [2]int
	var ended [2]bool
	for hi, h := range p.Hunks {
		// Empty names on BOTH sides are a valid hunk-only patch. When a path
		// identifies an added/deleted file, its missing side has no coordinates.
		if p.OldName != "" || p.NewName != "" {
			if p.OldName == "" && (h.OldStart != 0 || h.OldLines != 0) || p.NewName == "" && (h.NewStart != 0 || h.NewLines != 0) {
				return indices, nil, fmt.Errorf("%w: missing side has coordinates", ErrPatch)
			}
		}
		starts := [2]int{h.OldStart, h.NewStart}
		counts := [2]int{h.OldLines, h.NewLines}
		var bases [2]int
		for side := range bases {
			b, err := boundary(starts[side], counts[side])
			if err != nil || hi > 0 && b < previousEnd[side] {
				return indices, nil, fmt.Errorf("%w: overlapping or invalid hunk %d", ErrPatch, hi)
			}
			bases[side], previousEnd[side] = b, b+counts[side]
		}
		if counts == [2]int{} {
			return indices, nil, fmt.Errorf("%w: empty hunk", ErrPatch)
		}
		var consumed [2]int
		for ri, row := range h.Lines {
			if row == "" {
				return indices, nil, ErrPatch
			}
			if row[0] == '\\' {
				if strings.TrimSuffix(row, "\r") != eofText || ri == 0 || h.Lines[ri-1][0] == '\\' {
					return indices, nil, fmt.Errorf("%w: misplaced EOF marker", ErrPatch)
				}
				for side := range indices {
					if rowOnSide(h.Lines[ri-1][0], side) {
						if ended[side] {
							return indices, nil, ErrPatch
						}
						ended[side] = true
						markers = append(markers, EOFMarker{editor.DiffSide(side), bases[side] + consumed[side]})
					}
				}
				continue
			}
			if row[0] != ' ' && row[0] != '-' && row[0] != '+' {
				return indices, nil, ErrPatch
			}
			for side := range indices {
				if !rowOnSide(row[0], side) {
					continue
				}
				if ended[side] || consumed[side] >= counts[side] {
					return indices, nil, ErrPatch
				}
				consumed[side]++
				line := bases[side] + consumed[side]
				indices[side].inverse[line] = len(indices[side].lines)
				indices[side].lines = append(indices[side].lines, line)
			}
		}
		if consumed != counts {
			return indices, nil, fmt.Errorf("%w: hunk counts", ErrPatch)
		}
	}
	return indices, markers, nil
}

func rowOnSide(prefix byte, side int) bool {
	return prefix == ' ' || side == 0 && prefix == '-' || side == 1 && prefix == '+'
}

func clonePatch(p editor.FilePatch) editor.FilePatch {
	p.Hunks = slices.Clone(p.Hunks)
	for i := range p.Hunks {
		p.Hunks[i].Lines = slices.Clone(p.Hunks[i].Lines)
	}
	return p
}

func (p *Presentation) Raw() string             { return p.raw }
func (p *Presentation) Patch() editor.FilePatch { return clonePatch(p.patch) }
func (p *Presentation) EOFMarkers() []EOFMarker { return slices.Clone(p.markers) }

func validSide(side editor.DiffSide) bool { return side == editor.DiffOld || side == editor.DiffNew }

func (p *Presentation) Lines(side editor.DiffSide) []int {
	if !validSide(side) {
		return nil
	}
	return slices.Clone(p.indices[side].lines)
}

func (p *Presentation) FileLine(side editor.DiffSide, compact int) (int, bool) {
	if !validSide(side) || compact < 0 || compact >= len(p.indices[side].lines) {
		return 0, false
	}
	return p.indices[side].lines[compact], true
}

func (p *Presentation) CompactLine(side editor.DiffSide, line int) (int, bool) {
	if !validSide(side) {
		return 0, false
	}
	i, ok := p.indices[side].inverse[line]
	return i, ok
}

func validateRange(r ServerRange) error {
	if !validSide(r.Side) || r.Start < 1 || r.End < r.Start || r.End-r.Start >= MaxSelectionLines {
		return ErrRange
	}
	return nil
}

func (p *Presentation) ServerSelection(r editor.DiffLineRange) (ServerRange, error) {
	if r.End < r.Start {
		r.Start, r.End = r.End, r.Start
	}
	start, ok := p.FileLine(r.Side, r.Start)
	end, endOK := p.FileLine(r.Side, r.End)
	result := ServerRange{r.Side, start, end}
	if !ok || !endOK || validateRange(result) != nil || end-start != r.End-r.Start {
		return ServerRange{}, ErrRange
	}
	// All maps are strictly increasing, so equal endpoint distances prove every
	// interior index is contiguous. Endpoints alone without this check do not.
	return result, nil
}

type MappedRange struct {
	Stored  ServerRange
	Compact editor.DiffLineRange
	Visible bool
}

func (p *Presentation) Remap(r ServerRange) (MappedRange, error) {
	if err := validateRange(r); err != nil {
		return MappedRange{}, err
	}
	result := MappedRange{Stored: r}
	start, ok := p.CompactLine(r.Side, r.Start)
	end, endOK := p.CompactLine(r.Side, r.End)
	if ok && endOK && end-start == r.End-r.Start {
		result.Compact = editor.DiffLineRange{Side: r.Side, Start: start, End: end}
		result.Visible = true
	}
	return result, nil
}

type contentLine struct {
	text    string
	newline bool
}

func splitContents(text string) []contentLine {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	terminated := parts[len(parts)-1] == ""
	if terminated {
		parts = parts[:len(parts)-1]
	}
	lines := make([]contentLine, len(parts))
	for i, part := range parts {
		lines[i] = contentLine{part, i < len(parts)-1 || terminated}
	}
	return lines
}

// verifyContents walks the STORED topology. Every row and unchanged gap must
// match exact bytes and newline metadata. No search, reanchoring or diff occurs.
func (p *Presentation) verifyContents(c *CompleteContents) ([2][]contentLine, error) {
	var lines [2][]contentLine
	if c == nil || p.stored.Binary {
		return lines, ErrContents
	}
	if p.stored.OldName != "" || p.stored.NewName != "" {
		if p.stored.OldName == "" && c.Before != "" || p.stored.NewName == "" && c.After != "" {
			return lines, ErrContents
		}
	}
	lines = [2][]contentLine{splitContents(c.Before), splitContents(c.After)}
	at := [2]int{}
	for _, h := range p.stored.Hunks {
		oldBase, _ := boundary(h.OldStart, h.OldLines)
		newBase, _ := boundary(h.NewStart, h.NewLines)
		bases := [2]int{oldBase, newBase}
		if oldBase > len(lines[0]) || newBase > len(lines[1]) ||
			!slices.Equal(lines[0][at[0]:oldBase], lines[1][at[1]:newBase]) {
			return lines, ErrContents
		}
		at = bases
		for ri, row := range h.Lines {
			if row[0] == '\\' {
				continue
			}
			newline := !(ri+1 < len(h.Lines) && h.Lines[ri+1][0] == '\\')
			for side := range lines {
				if !rowOnSide(row[0], side) {
					continue
				}
				if at[side] >= len(lines[side]) || lines[side][at[side]] != (contentLine{row[1:], newline}) {
					return lines, ErrContents
				}
				at[side]++
			}
		}
	}
	if !slices.Equal(lines[0][at[0]:], lines[1][at[1]:]) {
		return lines, ErrContents
	}
	return lines, nil
}

// Expand adds at most MaxContextStep unchanged lines per request, clamped to
// adjacent stored hunks/file boundaries. Hunk refers to the current snapshot.
// Repeated calls may reveal a whole file; this is not a file-size policy.
func (p *Presentation) Expand(hunk, before, after int, c *CompleteContents) (*Presentation, error) {
	if hunk < 0 || hunk >= len(p.groups) || before < 0 || after < 0 || before > MaxContextStep || after > MaxContextStep-before || before+after == 0 {
		return nil, ErrExpansion
	}
	lines, err := p.verifyContents(c)
	if err != nil {
		return nil, err
	}
	margins := slices.Clone(p.margins)
	g := p.groups[hunk]
	first, last := p.stored.Hunks[g.first], p.stored.Hunks[g.last]
	start, _ := boundary(first.OldStart, first.OldLines)
	prevEnd := 0
	if g.first > 0 {
		prev := p.stored.Hunks[g.first-1]
		prevEnd, _ = boundary(prev.OldStart, prev.OldLines)
		prevEnd += prev.OldLines
	}
	end, _ := boundary(last.OldStart, last.OldLines)
	end += last.OldLines
	nextStart := len(lines[0])
	if g.last+1 < len(p.stored.Hunks) {
		next := p.stored.Hunks[g.last+1]
		nextStart, _ = boundary(next.OldStart, next.OldLines)
	}
	// Avoid overflow even for very large coordinates/margins.
	margins[g.first].before += min(before, start-prevEnd-margins[g.first].before)
	margins[g.last].after += min(after, nextStart-end-margins[g.last].after)
	return p.rebuild(margins, lines[0])
}

func (p *Presentation) Collapse() *Presentation {
	// Already validated immutable raw input; cannot fail on reconstruction.
	indices, markers, _ := indexPatch(p.stored)
	groups := make([]hunkGroup, len(p.stored.Hunks))
	for i := range groups {
		groups[i] = hunkGroup{i, i}
	}
	return &Presentation{raw: p.raw, stored: p.stored, patch: p.stored, indices: indices,
		markers: markers, margins: make([]contextMargin, len(groups)), groups: groups}
}

func addContext(h *editor.PatchHunk, lines []contentLine) {
	for _, line := range lines {
		h.Lines = append(h.Lines, " "+line.text)
		if !line.newline {
			h.Lines = append(h.Lines, eofText)
		}
	}
	h.OldLines += len(lines)
	h.NewLines += len(lines)
}

// Adjacent stored runs must not silently coalesce merely because hunk headers
// disappeared. Keep a header boundary when there is no unchanged separating row.
func mergeSafe(a, b editor.PatchHunk, gap int) bool {
	if gap > 0 {
		return true
	}
	last := len(a.Lines) - 1
	if last >= 0 && a.Lines[last][0] == '\\' {
		last--
	}
	return last >= 0 && a.Lines[last][0] == ' ' || len(b.Lines) > 0 && b.Lines[0][0] == ' '
}

func (p *Presentation) rebuild(margins []contextMargin, old []contentLine) (*Presentation, error) {
	out := p.stored
	out.Hunks = nil
	var groups []hunkGroup
	for i, source := range p.stored.Hunks {
		base, _ := boundary(source.OldStart, source.OldLines)
		left := margins[i].before
		merged := false
		if i > 0 {
			prev := p.stored.Hunks[i-1]
			prevBase, _ := boundary(prev.OldStart, prev.OldLines)
			gap := base - prevBase - prev.OldLines
			// Compare by subtraction rather than adding potentially large margins.
			merged = margins[i-1].after >= gap-left && mergeSafe(prev, source, gap)
			if merged {
				h := &out.Hunks[len(out.Hunks)-1]
				// Previous right context was deliberately deferred until this decision.
				addContext(h, old[prevBase+prev.OldLines:base])
				h.Lines = append(h.Lines, source.Lines...)
				h.OldLines += source.OldLines
				h.NewLines += source.NewLines
				groups[len(groups)-1].last = i
			} else {
				h := &out.Hunks[len(out.Hunks)-1]
				end := prevBase + prev.OldLines
				addContext(h, old[end:end+margins[i-1].after])
			}
		}
		if !merged {
			h := source
			h.Lines = nil
			h.OldLines, h.NewLines = 0, 0
			addContext(&h, old[base-left:base])
			h.Lines = append(h.Lines, source.Lines...)
			h.OldLines += source.OldLines
			h.NewLines += source.NewLines
			out.Hunks = append(out.Hunks, h)
			groups = append(groups, hunkGroup{i, i})
		}
	}
	if len(out.Hunks) > 0 {
		i := len(p.stored.Hunks) - 1
		source := p.stored.Hunks[i]
		base, _ := boundary(source.OldStart, source.OldLines)
		end := base + source.OldLines
		h := &out.Hunks[len(out.Hunks)-1]
		addContext(h, old[end:end+margins[i].after])
	}
	for i := range out.Hunks {
		h := &out.Hunks[i]
		first := groups[i].first
		source := p.stored.Hunks[first]
		oldBase, _ := boundary(source.OldStart, source.OldLines)
		newBase, _ := boundary(source.NewStart, source.NewLines)
		h.OldStart, h.NewStart = oldBase-margins[first].before, newBase-margins[first].before
		if h.OldLines > 0 {
			h.OldStart++
		}
		if h.NewLines > 0 {
			h.NewStart++
		}
	}
	indices, _, err := indexPatch(out)
	if err != nil {
		return nil, err
	}
	// Only raw markers are authoritative. A displayed formerly-hidden final
	// unchanged line can require widget metadata, but does not invent a raw marker.
	return &Presentation{raw: p.raw, stored: p.stored, patch: out, indices: indices,
		markers: p.markers, margins: margins, groups: groups}, nil
}
