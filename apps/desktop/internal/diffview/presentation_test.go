package diffview

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func adapterPresentation(t *testing.T, raw string) *Presentation {
	t.Helper()
	p, err := NewPresentation(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func adapterExpand(t *testing.T, p *Presentation, hunk, before, after int, contents *CompleteContents) *Presentation {
	t.Helper()
	out, err := p.Expand(hunk, before, after, contents)
	if err != nil {
		t.Fatal(err)
	}
	if out.Raw() != p.Raw() {
		t.Fatal("raw patch changed")
	}
	if !reflect.DeepEqual(adapterRuns(p.stored), adapterRuns(out.patch)) {
		t.Fatal("stored change runs changed")
	}
	return out
}

// Keep every raw +/- run, including boundaries between adjacent stored hunks.
func adapterRuns(p editor.FilePatch) [][]string {
	var out [][]string
	for _, h := range p.Hunks {
		var run []string
		flush := func() {
			if len(run) > 0 {
				out = append(out, run)
				run = nil
			}
		}
		for _, row := range h.Lines {
			if row[0] == '-' || row[0] == '+' {
				run = append(run, row)
			} else if row[0] == ' ' {
				flush()
			}
		}
		flush()
	}
	return out
}

func adapterAssertMaps(t *testing.T, p *Presentation, side editor.DiffSide, want []int) {
	t.Helper()
	if got := p.Lines(side); !slices.Equal(got, want) {
		t.Fatalf("side=%d map=%v want=%v", side, got, want)
	}
	for compact, line := range want {
		if got, ok := p.FileLine(side, compact); !ok || got != line {
			t.Fatalf("forward %d -> %d %v", compact, got, ok)
		}
		if got, ok := p.CompactLine(side, line); !ok || got != compact {
			t.Fatalf("inverse %d -> %d %v", line, got, ok)
		}
	}
}

func TestAdapterSparseMapsAndContiguousSelections(t *testing.T) {
	p := adapterPresentation(t, sparse)
	for _, side := range []editor.DiffSide{editor.DiffOld, editor.DiffNew} {
		want := []int{40, 41, 42, 400, 401, 402}
		if side == editor.DiffNew {
			want = []int{70, 71, 72, 700, 701, 702}
		}
		adapterAssertMaps(t, p, side, want)
		for _, r := range []editor.DiffLineRange{{Side: side, Start: 0, End: 2}, {Side: side, Start: 2, End: 0}} {
			got, err := p.ServerSelection(r)
			if err != nil || got != (ServerRange{side, want[0], want[2]}) {
				t.Fatalf("range=%+v err=%v", got, err)
			}
		}
		for _, r := range []editor.DiffLineRange{{Side: side, Start: 2, End: 3}, {Side: side, Start: -1, End: 1}, {Side: side, Start: 0, End: 6}} {
			if _, err := p.ServerSelection(r); !errors.Is(err, ErrRange) {
				t.Fatalf("accepted %+v", r)
			}
		}
		mapped, err := p.Remap(ServerRange{side, want[0], want[2] + 1})
		if err != nil || mapped.Visible {
			t.Fatalf("sparse placement visible %+v %v", mapped, err)
		}
	}
	if _, err := p.ServerSelection(editor.DiffLineRange{Side: 255}); !errors.Is(err, ErrRange) {
		t.Fatal(err)
	}
	if _, ok := p.CompactLine(255, 1); ok {
		t.Fatal("invalid side")
	}
	if _, ok := p.CompactLine(editor.DiffOld, 0); ok {
		t.Fatal("line zero")
	}
}

func TestAdapterSelectionLimit(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("@@ -1,201 +1,201 @@\n")
	for i := 0; i < 201; i++ {
		raw.WriteString(" same\n")
	}
	p := adapterPresentation(t, raw.String())
	if _, err := p.ServerSelection(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 199}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ServerSelection(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 200}); !errors.Is(err, ErrRange) {
		t.Fatal("accepted >200")
	}
	if _, err := p.Remap(ServerRange{editor.DiffOld, 1, 201}); !errors.Is(err, ErrRange) {
		t.Fatal("remap >200")
	}
	v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
	if err := v.SetSelection(ServerRange{editor.DiffNew, 1, 200}); err != nil {
		t.Fatal(err)
	}
	if err := v.MoveSelection(1, true); !errors.Is(err, ErrRange) {
		t.Fatal("keyboard exceeded 200 lines")
	}
	if selected, _ := v.Selection(); selected.End != 200 {
		t.Fatal("rejected keyboard range changed selection")
	}
}

func TestAdapterRejectsMalformedHunks(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":           "",
		"negative":        "@@ --1 +1 @@\n-x\n+y\n",
		"zeroStart":       "@@ -0 +1 @@\n-x\n+y\n",
		"negativeCount":   "@@ -1,-1 +1 @@\n+y\n",
		"emptyHunk":       "@@ -0,0 +0,0 @@\n",
		"tooMany":         "@@ -1 +1 @@\n-x\n+y\n+discarded\n",
		"beforeHeader":    "--- a/x\n+++ b/x\n-discarded\n+discarded\n@@ -1 +1 @@\n-x\n+y\n",
		"missingOldSide":  "--- /dev/null\n+++ b/x\n@@ -1 +1 @@\n-x\n+y\n",
		"missingNewSide":  "--- a/x\n+++ /dev/null\n@@ -1 +1 @@\n-x\n+y\n",
		"binaryAndText":   "diff --git a/x b/x\nBinary files a/x and b/x differ\n@@ -1 +1 @@\n-x\n+y\n",
		"tooFew":          "@@ -1,2 +1,2 @@\n-x\n+y\n",
		"unprefixedBlank": "@@ -1 +1 @@\n\n",
		"badHeader":       "@@ -1 +1 @@\n-x\n+y\n@@broken\n",
		"oldOverlap":      "@@ -2 +2 @@\n-x\n+y\n@@ -2 +4 @@\n-x\n+z\n",
		"newOverlap":      "@@ -2 +2 @@\n-x\n+y\n@@ -4 +2 @@\n-z\n+y\n",
		"zeroInside":      "@@ -2,2 +2 @@\n-x\n-z\n+y\n@@ -2,0 +3 @@\n+w\n",
		"badMarker":       "@@ -1 +1 @@\n-x\n\\ arbitrary\n+y\n",
		"orphanMarker":    "@@ -1 +1 @@\n\\ No newline at end of file\n-x\n+y\n",
		"duplicateMarker": "@@ -1 +1 @@\n-x\n\\ No newline at end of file\n\\ No newline at end of file\n+y\n",
		"afterEOF":        "@@ -1,2 +1 @@\n-x\n\\ No newline at end of file\n-z\n+y\n",
		"multipleFiles":   "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-x\n+y\n--- a/z\n+++ b/z\n@@ -1 +1 @@\n-a\n+b\n",
		"overflow":        fmt.Sprintf("@@ -%d,2 +1,2 @@\n a\n b\n", math.MaxInt),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPresentation(raw); !errors.Is(err, ErrPatch) {
				t.Fatalf("accepted raw or wrong error: %v", err)
			}
		})
	}
}

func TestAdapterVerifiedContextAndGapMerging(t *testing.T) {
	raw := "--- a/x\n+++ b/x\n@@ -3 +3 @@ one\n-old1\n+new1\n@@ -7 +7 @@ two\n-old2\n+new2\n"
	c := &CompleteContents{Before: "a\nb\nold1\nd\ne\nf\nold2\nh\ni\n", After: "a\nb\nnew1\nd\ne\nf\nnew2\nh\ni\n"}
	p := adapterPresentation(t, raw)
	first := adapterExpand(t, p, 0, 1, 1, c)
	if got := first.Patch().Hunks[0].Lines; !slices.Equal(got, []string{" b", "-old1", "+new1", " d"}) {
		t.Fatal(got)
	}
	if len(first.Patch().Hunks) != 2 {
		t.Fatal("premature merge")
	}
	adapterAssertMaps(t, first, editor.DiffOld, []int{2, 3, 4, 7})
	second := adapterExpand(t, first, 1, 2, 1, c)
	if len(second.Patch().Hunks) != 1 {
		t.Fatalf("gap not merged: %+v", second.Patch())
	}
	adapterAssertMaps(t, second, editor.DiffNew, []int{2, 3, 4, 5, 6, 7, 8})
	full := adapterExpand(t, second, 0, 200, 0, c)
	full = adapterExpand(t, full, 0, 0, 200, c)
	adapterAssertMaps(t, full, editor.DiffNew, []int{1, 2, 3, 4, 5, 6, 7, 8, 9})
	if got, err := full.Patch().Apply(c.Before); err != nil || got != c.After {
		t.Fatalf("presentation application %q %v", got, err)
	}
	if !reflect.DeepEqual(full.Collapse().Patch(), p.Patch()) {
		t.Fatal("collapse changed stored patch")
	}
	if len(p.Patch().Hunks[0].Lines) != 2 || len(first.Patch().Hunks) != 2 {
		t.Fatal("earlier snapshot mutated")
	}
	// Overlapping revealed windows must insert their intersection once.
	overlap := adapterExpand(t, p, 0, 0, 2, c)
	overlap = adapterExpand(t, overlap, 1, 2, 0, c)
	adapterAssertMaps(t, overlap, editor.DiffOld, []int{3, 4, 5, 6, 7})
}

func TestAdapterRepeatedTextPreservesStoredRuns(t *testing.T) {
	raw := "@@ -2 +1,0 @@\n-X\n@@ -5,0 +5 @@\n+X\n"
	c := &CompleteContents{Before: "X\nX\nX\nX\nX\nX\n", After: "X\nX\nX\nX\nX\nX\n"}
	p := adapterPresentation(t, raw)
	p = adapterExpand(t, p, 0, 1, 3, c)
	p = adapterExpand(t, p, 0, 0, 1, c)
	if len(p.Patch().Hunks) != 1 {
		t.Fatal("expected adjacent-context merge")
	}
	v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
	if added, deleted := v.Stats(); added != 1 || deleted != 1 {
		t.Fatalf("fresh diff substituted: +%d -%d", added, deleted)
	}
	if len(v.Hunks()) != 2 {
		t.Fatal("stored separate runs coalesced")
	}
}

func TestAdapterAdjacentChangesKeepRunBoundary(t *testing.T) {
	p := adapterPresentation(t, "@@ -1 +1 @@\n-a\n+b\n@@ -2 +2 @@\n-c\n+d\n")
	c := &CompleteContents{Before: "a\nc\nx\n", After: "b\nd\nx\n"}
	out := adapterExpand(t, p, 1, 0, 1, c)
	if len(out.Patch().Hunks) != 2 || len(NewReadOnlyView(out, editor.DiffUnified, false, nil).Hunks()) != 2 {
		t.Fatal("adjacent stored runs coalesced")
	}
}

func TestAdapterExpansionOrderAndMultipleMergedHunks(t *testing.T) {
	raw := "@@ -2 +2 @@ first\n-old1\n+new1\n@@ -5 +5,2 @@ second\n-old2\n+new2\n+extra\n@@ -8 +9 @@ third\n-old3\n+new3\n"
	c := &CompleteContents{"a\nold1\nb\nc\nold2\nd\ne\nold3\nf\n", "a\nnew1\nb\nc\nnew2\nextra\nd\ne\nnew3\nf\n"}
	var want editor.FilePatch
	for _, order := range [][3]int{{2, 5, 9}, {2, 9, 5}, {5, 2, 9}, {5, 9, 2}, {9, 2, 5}, {9, 5, 2}} {
		p := adapterPresentation(t, raw)
		for _, line := range order {
			hunk := -1
			for i, h := range p.Patch().Hunks {
				if line >= h.NewStart && line-h.NewStart < h.NewLines {
					hunk = i
					break
				}
			}
			p = adapterExpand(t, p, hunk, 100, 100, c)
		}
		adapterAssertMaps(t, p, editor.DiffOld, []int{1, 2, 3, 4, 5, 6, 7, 8, 9})
		adapterAssertMaps(t, p, editor.DiffNew, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
		if len(p.Patch().Hunks) != 1 {
			t.Fatal("three-way merge failed")
		}
		if got, err := p.Patch().Apply(c.Before); err != nil || got != c.After {
			t.Fatalf("byte roundtrip: %q %v", got, err)
		}
		if len(want.Hunks) == 0 {
			want = p.Patch()
		} else if !reflect.DeepEqual(p.Patch(), want) {
			t.Fatalf("order-dependent output: %v", order)
		}
	}
}

func TestAdapterZeroCountBeforeAnotherHunk(t *testing.T) {
	p := adapterPresentation(t, "@@ -0,0 +1 @@\n+x\n@@ -3 +4 @@\n-c\n+C\n")
	c := &CompleteContents{"a\nb\nc\nd\n", "x\na\nb\nC\nd\n"}
	p = adapterExpand(t, p, 0, 0, 1, c)
	adapterAssertMaps(t, p, editor.DiffOld, []int{1, 3})
	adapterAssertMaps(t, p, editor.DiffNew, []int{1, 2, 4})
	if got, err := p.Patch().Apply(c.Before); err != nil || got != c.After {
		t.Fatalf("zero-count start: %q %v", got, err)
	}
	p = adapterExpand(t, p, 1, 1, 0, c)
	adapterAssertMaps(t, p, editor.DiffOld, []int{1, 2, 3})
	adapterAssertMaps(t, p, editor.DiffNew, []int{1, 2, 3, 4})
}

func TestAdapterMetadataOnlyAndBinary(t *testing.T) {
	for _, raw := range []string{
		"diff --git a/x b/x\nBinary files a/x and b/x differ\n",
		"diff --git a/x b/y\nsimilarity index 100%\nrename from x\nrename to y\n",
		"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n",
	} {
		p := adapterPresentation(t, raw)
		if len(p.Lines(editor.DiffOld)) != 0 || len(p.Lines(editor.DiffNew)) != 0 {
			t.Fatal("invented metadata code lines")
		}
		if _, err := p.ServerSelection(editor.DiffLineRange{}); !errors.Is(err, ErrRange) {
			t.Fatal("phantom metadata line selected")
		}
		if _, err := p.Expand(0, 1, 1, &CompleteContents{}); !errors.Is(err, ErrExpansion) {
			t.Fatal("metadata expanded")
		}
		v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
		tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, 800, 400)
		tt.Frame()
		if added, deleted := v.Stats(); added != 0 || deleted != 0 {
			t.Fatal("metadata change stats")
		}
	}
}

func TestAdapterZeroCountAndMissingSides(t *testing.T) {
	for _, tc := range []struct {
		name, raw, before, after string
		old, new                 []int
	}{
		{"added", "--- /dev/null\n+++ b/x\n@@ -0,0 +1,2 @@\n+a\n+b\n", "", "a\nb\n", nil, []int{1, 2}},
		{"deleted", "--- a/x\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n", "a\nb\n", "", []int{1, 2}, nil},
		{"insertStart", "@@ -0,0 +1 @@\n+x\n", "a\nb\n", "x\na\nb\n", []int{1, 2}, []int{1, 2, 3}},
		{"insertMiddle", "@@ -1,0 +2 @@\n+x\n", "a\nb\n", "a\nx\nb\n", []int{1, 2}, []int{1, 2, 3}},
		{"deleteMiddle", "@@ -2 +1,0 @@\n-x\n", "a\nx\nb\n", "a\nb\n", []int{1, 2, 3}, []int{1, 2}},
		{"insertEnd", "@@ -2,0 +3 @@\n+x\n", "a\nb\n", "a\nb\nx\n", []int{1, 2}, []int{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := adapterPresentation(t, tc.raw)
			c := &CompleteContents{tc.before, tc.after}
			p = adapterExpand(t, p, 0, 50, 50, c)
			adapterAssertMaps(t, p, editor.DiffOld, tc.old)
			adapterAssertMaps(t, p, editor.DiffNew, tc.new)
			if got, err := p.Patch().Apply(tc.before); err != nil || got != tc.after {
				t.Fatalf("%q %v", got, err)
			}
			if len(tc.old) == 0 {
				if _, err := p.ServerSelection(editor.DiffLineRange{Side: editor.DiffOld}); !errors.Is(err, ErrRange) {
					t.Fatal("phantom old line selected")
				}
			}
			if len(tc.new) == 0 {
				if _, err := p.ServerSelection(editor.DiffLineRange{Side: editor.DiffNew}); !errors.Is(err, ErrRange) {
					t.Fatal("phantom new line selected")
				}
			}
		})
	}
}

func TestAdapterRejectsAbsentAndInconsistentContents(t *testing.T) {
	p := adapterPresentation(t, "@@ -2 +2 @@\n-old\n+new\n")
	for name, c := range map[string]*CompleteContents{
		"absent": nil, "empty": {}, "wrongBefore": {Before: "a\nwrong\n", After: "a\nnew\n"},
		"wrongAfter":   {Before: "a\nold\n", After: "a\nwrong\n"},
		"prefix":       {Before: "a\nold\n", After: "b\nnew\n"},
		"suffix":       {Before: "a\nold\nz\n", After: "a\nnew\ny\n"},
		"short":        {Before: "old\n", After: "new\n"},
		"unterminated": {Before: "a\nold", After: "a\nnew"},
		"partial":      {Before: "old\n", After: "a\nnew\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := p.Expand(0, 1, 1, c); out != nil || !errors.Is(err, ErrContents) {
				t.Fatalf("out=%v err=%v", out, err)
			}
		})
	}
	// A later stored hunk must match even when expanding only the first hunk.
	p = adapterPresentation(t, "@@ -1 +1 @@\n-a\n+b\n@@ -3 +3 @@\n-c\n+d\n")
	if _, err := p.Expand(0, 0, 1, &CompleteContents{"a\nx\nc\n", "b\nx\nwrong\n"}); !errors.Is(err, ErrContents) {
		t.Fatal("unverified later hunk")
	}
	// Sparse coordinate offsets cannot be fabricated from matching text nearby.
	p = adapterPresentation(t, "@@ -2 +3 @@\n-old\n+new\n")
	if _, err := p.Expand(0, 1, 0, &CompleteContents{"a\nold\n", "a\nx\nnew\n"}); !errors.Is(err, ErrContents) {
		t.Fatal("unequal gap lengths")
	}
	p = adapterPresentation(t, "--- /dev/null\n+++ b/x\n@@ -0,0 +1 @@\n+x\n")
	if _, err := p.Expand(0, 0, 1, &CompleteContents{"tail\n", "x\ntail\n"}); !errors.Is(err, ErrContents) {
		t.Fatal("added file accepted a nonempty old side")
	}
	p = adapterPresentation(t, "--- a/x\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n")
	if _, err := p.Expand(0, 0, 1, &CompleteContents{"x\ntail\n", "tail\n"}); !errors.Is(err, ErrContents) {
		t.Fatal("deleted file accepted a nonempty new side")
	}
}

func TestAdapterBoundedExpansion(t *testing.T) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "same"
	}
	before := slices.Clone(lines)
	before[499] = "old"
	lines[499] = "new"
	c := &CompleteContents{strings.Join(before, "\n") + "\n", strings.Join(lines, "\n") + "\n"}
	p := adapterPresentation(t, "@@ -500 +500 @@\n-old\n+new\n")
	for _, request := range [][3]int{{-1, 1, 1}, {1, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, 0, 0}, {0, 201, 0}, {0, 100, 101}, {0, math.MaxInt, 1}} {
		if _, err := p.Expand(request[0], request[1], request[2], c); !errors.Is(err, ErrExpansion) {
			t.Fatalf("accepted %v: %v", request, err)
		}
	}
	out := adapterExpand(t, p, 0, 100, 100, c)
	if len(out.Lines(editor.DiffNew)) != 201 {
		t.Fatal("unbounded expansion")
	}
	if len(p.Lines(editor.DiffNew)) != 1 {
		t.Fatal("input mutated")
	}
}

func TestAdapterImmutableSnapshots(t *testing.T) {
	p := adapterPresentation(t, sparse)
	patch := p.Patch()
	patch.Hunks[0].Lines[0] = " MUTATED"
	patch.Hunks[0].OldStart = 1
	lines := p.Lines(editor.DiffNew)
	lines[0] = 1
	if p.Patch().Hunks[0].Lines[0] != " before" || p.Patch().Hunks[0].OldStart != 40 || p.Lines(editor.DiffNew)[0] != 70 || p.Raw() != sparse {
		t.Fatal("aliased snapshot")
	}
	eof := adapterPresentation(t, "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n")
	markers := eof.EOFMarkers()
	markers[0].Line = 99
	if eof.EOFMarkers()[0].Line != 1 {
		t.Fatal("aliased markers")
	}
}

func TestAdapterCRLFAndEOFMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, raw, before, after string
		markers                  []EOFMarker
	}{
		{"both", "@@ -2 +2 @@\n-old 文\n\\ No newline at end of file\n+new α\n\\ No newline at end of file\n", "same\nold 文", "same\nnew α", []EOFMarker{{editor.DiffOld, 2}, {editor.DiffNew, 2}}},
		{"oldOnly", "@@ -2 +2 @@\n-old\n\\ No newline at end of file\n+new\n", "same\nold", "same\nnew\n", []EOFMarker{{editor.DiffOld, 2}}},
		{"newOnly", "@@ -2 +2 @@\n-old\n+new\n\\ No newline at end of file\n", "same\nold\n", "same\nnew", []EOFMarker{{editor.DiffNew, 2}}},
		{"contextBoth", "@@ -2,2 +2,2 @@\n-old\n+new\n tail\n\\ No newline at end of file\n", "same\nold\ntail", "same\nnew\ntail", []EOFMarker{{editor.DiffOld, 3}, {editor.DiffNew, 3}}},
		{"CRLF", "@@ -2 +2 @@\r\n-old\r\n+new\r\n", "same\r\nold\r\ntail\r\n", "same\r\nnew\r\ntail\r\n", nil},
		{"CRLFmissingEOF", "@@ -2 +2 @@\r\n-old\n\\ No newline at end of file\r\n+new\n\\ No newline at end of file\r\n", "same\r\nold", "same\r\nnew", []EOFMarker{{editor.DiffOld, 2}, {editor.DiffNew, 2}}},
		{"hiddenFinal", "@@ -1 +1 @@\n-old\n+new\n", "old\ntail", "new\ntail", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := adapterPresentation(t, tc.raw)
			p = adapterExpand(t, p, 0, 20, 20, &CompleteContents{tc.before, tc.after})
			if !reflect.DeepEqual(p.EOFMarkers(), tc.markers) {
				t.Fatalf("markers=%+v", p.EOFMarkers())
			}
			if got, err := p.Patch().Apply(tc.before); err != nil || got != tc.after {
				t.Fatalf("byte roundtrip %q %v", got, err)
			}
			if p.Raw() != tc.raw {
				t.Fatal("raw newline bytes changed")
			}
		})
	}
	// EOF metadata must describe the actual last line and newline state.
	p := adapterPresentation(t, "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n")
	if _, err := p.Expand(0, 0, 1, &CompleteContents{"old\nx", "new\nx"}); !errors.Is(err, ErrContents) {
		t.Fatal("false EOF accepted")
	}
}

func TestAdapterSelectionAndPlacementRemapping(t *testing.T) {
	p := adapterPresentation(t, "@@ -3 +3 @@\n-old\n+new\n")
	c := &CompleteContents{"a\nb\nold\nd\ne\n", "a\nb\nnew\nd\ne\n"}
	v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
	stored := ServerRange{editor.DiffNew, 3, 3}
	if err := v.SetSelection(stored); err != nil {
		t.Fatal(err)
	}
	placements := []Placement{{"existing", "existing note", stored, 30}, {"hidden", "expanded note", ServerRange{editor.DiffNew, 2, 2}, 40}, {"outdated", "sidebar note", ServerRange{editor.DiffOld, 99, 99}, 30}}
	if err := v.SetPlacements(placements); err != nil {
		t.Fatal(err)
	}
	placements[0].Range.Start = 100
	if !v.Mapping()[0].Range.Visible || v.Mapping()[1].Range.Visible || v.Mapping()[2].Range.Visible {
		t.Fatal(v.Mapping())
	}
	expanded := adapterExpand(t, p, 0, 2, 2, c)
	if err := v.Replace(expanded); err != nil {
		t.Fatal(err)
	}
	if selected, ok := v.state.SelectedLines(); !ok || selected.Start != 2 {
		t.Fatalf("selection=%+v %v", selected, ok)
	}
	if v.Mapping()[0].Range.Compact.Start != 2 || !v.Mapping()[1].Range.Visible || v.Mapping()[1].Range.Compact.Start != 1 {
		t.Fatal(v.Mapping())
	}
	// Preserve an expanded-only selection when collapsed; never attach it to a
	// different compact line. It can be shown again after a later expansion.
	if err := v.SetSelection(ServerRange{editor.DiffNew, 2, 2}); err != nil {
		t.Fatal(err)
	}
	if err := v.Replace(expanded.Collapse()); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.state.SelectedLines(); ok {
		t.Fatal("collapsed selection reanchored")
	}
	if selected, ok := v.Selection(); !ok || selected.Start != 2 {
		t.Fatal("hidden stored selection lost")
	}
	if v.Mapping()[1].Range.Visible || v.Mapping()[0].Range.Compact.Start != 0 {
		t.Fatal(v.Mapping())
	}
	if err := v.Replace(expanded); err != nil {
		t.Fatal(err)
	}
	if err := v.SetDisplay(editor.DiffUnified, true); err != nil {
		t.Fatal(err)
	}
	if selected, ok := v.state.SelectedLines(); !ok || selected.Start != 1 {
		t.Fatal("expanded selection not restored")
	}
	copy := v.Mapping()
	copy[0].Placement.Key = "mutated"
	if v.Mapping()[0].Placement.Key != "existing" {
		t.Fatal("mapping aliased")
	}
	if err := v.Replace(adapterPresentation(t, "@@ -1 +1 @@\n-x\n+y\n")); !errors.Is(err, ErrPatch) {
		t.Fatal("replaced snapshot identity")
	}
}

func TestAdapterKeyboardBidirectionalRanges(t *testing.T) {
	p := adapterPresentation(t, "@@ -1,5 +1,5 @@\n a\n b\n-old\n+new\n d\n e\n")
	v := NewReadOnlyView(p, editor.DiffSplit, false, nil)
	if err := v.SetSelection(ServerRange{editor.DiffNew, 3, 3}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		delta      int
		extend     bool
		start, end int
	}{
		{-1, true, 2, 3}, {-1, true, 1, 3}, {1, true, 2, 3}, {1, true, 3, 3}, {1, true, 3, 4}, {-1, false, 3, 3},
	} {
		if err := v.MoveSelection(step.delta, step.extend); err != nil {
			t.Fatal(err)
		}
		if got, ok := v.Selection(); !ok || got.Start != step.start || got.End != step.end {
			t.Fatalf("step=%+v selection=%+v %v", step, got, ok)
		}
	}
	if err := v.SetSelection(ServerRange{editor.DiffOld, 1, 1}); err != nil {
		t.Fatal(err)
	}
	if err := v.MoveSelection(-1, true); !errors.Is(err, ErrRange) {
		t.Fatal("keyboard selected line zero")
	}
	v.ClearSelection()
	if _, ok := v.Selection(); ok {
		t.Fatal("clear selection failed")
	}
}

func TestAdapterSingleClickToggleClearsStoredSelection(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		var events []bool
		p := adapterPresentation(t, "@@ -1 +1 @@\n-old\n+new\n")
		v := NewReadOnlyView(p, layout, false, func(_ ServerRange, selected bool) { events = append(events, selected) })
		if err := v.SetPlacements([]Placement{{"note", "note", ServerRange{editor.DiffNew, 1, 1}, 24}}); err != nil {
			t.Fatal(err)
		}
		tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, 800, 500)
		tt.Frame()
		tt.Frame()
		x := float32(10)
		if layout == editor.DiffSplit {
			x = 410
		}
		tt.ClickAt(x, find(t, tt, "note").Y-10)
		tt.ClickAt(x, find(t, tt, "note").Y-10)
		tt.Frame()
		if _, ok := v.Selection(); ok {
			t.Fatal("stored selection survived toggle")
		}
		if !slices.Equal(events, []bool{true, false}) {
			t.Fatalf("events=%v", events)
		}
	}
}

func TestAdapterReadOnlyConstructionWithTester(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			p := adapterPresentation(t, sparse)
			v := NewReadOnlyView(p, layout, true, nil)
			tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, 800, 700)
			tt.Frame()
			tt.Frame()
			old, newText := v.state.Old.String(), v.state.New.String()
			for _, label := range []string{"Old text", "New text"} {
				if layout == editor.DiffUnified && label == "Old text" {
					continue
				}
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				tt.Type("MUTATION")
				tt.Key(0, ui.KeyBackspace)
				tt.Key(0, ui.KeyDelete)
				tt.Key(ui.Ctrl, ui.KeyA)
				tt.Type("REPLACEMENT")
				tt.SetClipboard("PASTED")
				tt.Command("paste")
				tt.Command("cut")
			}
			if old != v.state.Old.String() || newText != v.state.New.String() || v.state.Editable || v.state.HunkActions {
				t.Fatal("editable")
			}
			if tt.HasText("Revert change") || tt.HasText("Keep change") {
				t.Fatal("mutation actions exposed")
			}
			if p.Raw() != sparse {
				t.Fatal("raw changed")
			}
		})
	}
}

func TestAdapterEOFAnnotationsSurviveWrappingReplacementAndComments(t *testing.T) {
	old := strings.Repeat("old 文 ", 25)
	newText := strings.Repeat("new α ", 25)
	raw := "@@ -2 +2 @@\n-" + old + "\n\\ No newline at end of file\n+" + newText + "\n\\ No newline at end of file\n"
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		p := adapterPresentation(t, raw)
		v := NewReadOnlyView(p, layout, true, nil)
		if err := v.SetPlacements([]Placement{{"note", "line comment", ServerRange{editor.DiffNew, 2, 2}, 40}}); err != nil {
			t.Fatal(err)
		}
		tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, 600, 1400)
		tt.Frame()
		tt.Frame()
		check := func() {
			t.Helper()
			for _, side := range []string{"old", "new"} {
				if !tt.HasText("No newline at end of file (" + side + ")") {
					t.Fatal("EOF indicator absent", tt.Texts())
				}
			}
			if !tt.HasText("line comment") {
				t.Fatal("line comment absent")
			}
			for _, a := range v.state.Annotations {
				if note, ok := a.Key.(viewNote); ok && note.eof {
					line, ok := v.presentation.FileLine(a.Side, a.Line)
					if !ok || line != 2 || a.Side != note.side {
						t.Fatalf("misplaced EOF %+v", a)
					}
				}
			}
		}
		check()
		p = adapterExpand(t, p, 0, 1, 0, &CompleteContents{"same\r\n" + old, "same\r\n" + newText})
		if err := v.Replace(p); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		check()
		if err := v.Replace(p.Collapse()); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		check()
	}
}

func TestAdapterClickShiftClickAndKeyboardRangeContract(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		for _, side := range []editor.DiffSide{editor.DiffOld, editor.DiffNew} {
			var calls []ServerRange
			p := adapterPresentation(t, "@@ -40,3 +70,3 @@\n-a\n-b\n-c\n+x\n+y\n+z\n@@ -400 +700 @@\n-tail\n+tail\n")
			v := NewReadOnlyView(p, layout, false, func(r ServerRange, selected bool) {
				if selected {
					calls = append(calls, r)
				}
			})
			lines := p.Lines(side)
			if err := v.SetPlacements([]Placement{{"first", "first", ServerRange{side, lines[0], lines[0]}, 24}, {"last", "last", ServerRange{side, lines[2], lines[2]}, 24}, {"gap", "gap", ServerRange{side, lines[3], lines[3]}, 24}}); err != nil {
				t.Fatal(err)
			}
			tt := ui.NewTester(func(c *ui.Context) { v.Draw(c).Fill() }, 800, 900)
			tt.Frame()
			tt.Frame()
			x := float32(10)
			if layout == editor.DiffSplit && side == editor.DiffNew {
				x = 410
			}
			tt.ClickAt(x, find(t, tt, "first").Y-10)
			tt.ClickAtWith(ui.Shift, x, find(t, tt, "last").Y-10)
			if r, ok := v.Selection(); !ok || r != (ServerRange{side, lines[0], lines[2]}) {
				t.Fatalf("shift=%+v %v", r, ok)
			}
			count := len(calls)
			tt.ClickAtWith(ui.Shift, x, find(t, tt, "gap").Y-10)
			if len(calls) != count {
				t.Fatal("sparse selection callback escaped")
			}
			if r, _ := v.Selection(); r.End != lines[2] {
				t.Fatal("invalid selection retained")
			}
			if err := v.MoveSelection(-1, true); err != nil {
				t.Fatal(err)
			}
			if r, _ := v.Selection(); r.End != lines[1] {
				t.Fatal("keyboard shrink failed")
			}
			if err := v.MoveSelection(1, true); err != nil {
				t.Fatal(err)
			}
			if err := v.MoveSelection(1, true); !errors.Is(err, ErrRange) {
				t.Fatal("keyboard crossed sparse gap")
			}
			// The app owns focused keyboard binding; this proves its range-command
			// contract. Gutter drag is explicitly not claimed.
		}
	}
}

// This example is the promotion API: the app supplies snapshot-bound contents,
// keeps server ranges, and swaps only the presentation snapshot.
func ExamplePresentation() {
	p, err := NewPresentation("@@ -2 +2 @@\n-old\n+new\n")
	if err != nil {
		panic(err)
	}
	selected, err := p.ServerSelection(editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 0})
	if err != nil {
		panic(err)
	}
	expanded, err := p.Expand(0, 1, 1, &CompleteContents{Before: "before\nold\nafter\n", After: "before\nnew\nafter\n"})
	if err != nil {
		panic(err)
	}
	mapped, err := expanded.Remap(selected)
	if err != nil {
		panic(err)
	}
	v := NewReadOnlyView(p, editor.DiffSplit, true, nil)
	if err := v.SetSelection(selected); err != nil {
		panic(err)
	}
	if err := v.SetPlacements([]Placement{{Key: "comment-1", Label: "Review note", Range: selected, Height: 48}}); err != nil {
		panic(err)
	}
	if err := v.Replace(expanded); err != nil {
		panic(err)
	}
	fmt.Println(selected.Start, mapped.Compact.Start, mapped.Visible)
	if err := v.Replace(expanded.Collapse()); err != nil {
		panic(err)
	}
	fmt.Println(v.Mapping()[0].Range.Compact.Start, v.Mapping()[0].Range.Stored.Start)
	// Output:
	// 2 1 true
	// 0 2
}
