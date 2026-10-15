package review

import (
	"strings"
	"testing"
)

func TestCommentTargets(t *testing.T) {
	file := ReviewComment{ID: "file-comment", DiffID: "diff", Path: "new.ts", Scope: DiffAll, Fingerprint: "current", Target: "file", Side: "additions", Body: "Review the whole file", Status: "open"}
	if !file.Valid() {
		t.Fatal("whole-file comment should be valid without line context")
	}
	for _, selection := range []struct {
		target, side string
		start, end   int
		code         string
	}{
		{"", "additions", 0, 0, ""},
		{"lines", "additions", 0, 0, ""},
		{"other", "additions", 0, 0, ""},
		{"file", "additions", 1, 1, ""},
		{"file", "deletions", 0, 0, ""},
		{"file", "additions", 0, 0, "+ synthetic context"},
	} {
		invalid := file
		invalid.Target, invalid.Side, invalid.Start, invalid.End, invalid.Code = selection.target, selection.side, selection.start, selection.end, selection.code
		if invalid.Valid() {
			t.Fatalf("invalid selection accepted: %#v", invalid)
		}
	}
	for _, target := range []string{"", "lines"} {
		line := file
		line.Target, line.Start, line.End, line.Code = target, 1, 1, "+ value"
		if !line.Valid() {
			t.Fatalf("line comment rejected: %#v", line)
		}
	}
}

func TestFileCommentExportAndPlacement(t *testing.T) {
	comment := ReviewComment{ID: "file-comment", DiffID: "diff", VersionID: "v1", Path: "new.ts", Scope: DiffAll, Fingerprint: "current", Target: "file", Side: "additions", Body: "Review <file>", Status: "open"}
	oldPath := "old.ts"
	comment.Origin = &ReviewOrigin{Source: "stdin", File: ReviewFileOrigin{Status: "R", OldPath: &oldPath}}
	repository := RepositoryDiff{ID: "diff", VersionID: "v2", Mode: DiffAll, Files: []ChangedFile{{Path: "new.ts", Fingerprint: "changed"}}}
	noPatch := func(ChangedFile) (FilePatch, error) { t.Fatal("file comments need no patch"); return FilePatch{}, nil }
	placement, err := Locate(comment, &repository, noPatch)
	if err != nil || placement != (CommentPlacement{Applicability: Anchored, Side: "additions"}) {
		t.Fatalf("file comment follows its file across snapshots: %#v %v", placement, err)
	}
	comment.Placement = &placement
	output := FormatComments([]ReviewComment{comment}, false)
	for _, expected := range []string{`selection="file"`, `applicability="anchored"`, `old-path="old.ts" change="renamed"`, "<body>Review &lt;file&gt;</body>"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q in export: %s", expected, output)
		}
	}
	for _, unexpected := range []string{` line=`, ` end-line=`, ` side=`, "<code>", "stale="} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("synthetic line context %q in export: %s", unexpected, output)
		}
	}
	repository.Files[0].Path = "elsewhere.ts"
	placement, err = Locate(comment, &repository, noPatch)
	if err != nil || placement.Applicability != Outdated {
		t.Fatalf("file comment outlived its file: %#v %v", placement, err)
	}
	comment.Placement = &placement
	for _, includeResolved := range []bool{false, true} {
		output = FormatComments([]ReviewComment{comment}, includeResolved)
		if !strings.Contains(output, `selection="file"`) || !strings.Contains(output, `applicability="outdated" stale="true"`) {
			t.Fatalf("export must keep outdated file comment as stale: %s", output)
		}
	}
}

func TestPatchAnchorCapturesSameSideContext(t *testing.T) {
	patch := "@@ -1,6 +1,7 @@\n one\n two\n-old\n+new\n+added\n three\n four\n five\n six\n"
	anchor := PatchAnchor(patch, "additions", 3, 4)
	if *anchor != (CommentAnchor{Before: "one\ntwo", After: "three\nfour\nfive"}) {
		t.Fatalf("additions anchor: %#v", anchor)
	}
	anchor = PatchAnchor(patch, "deletions", 3, 3)
	if *anchor != (CommentAnchor{Before: "one\ntwo", After: "three\nfour\nfive"}) {
		t.Fatalf("deletions anchor: %#v", anchor)
	}
	anchor = PatchAnchor(patch, "additions", 1, 1)
	if anchor.Before != "" || anchor.After != "two\nnew\nadded" {
		t.Fatalf("hunk edge anchor: %#v", anchor)
	}
}

func TestMovedCommentExportsCurrentLines(t *testing.T) {
	comment := ReviewComment{ID: "line", DiffID: "diff", VersionID: "v1", Path: "a.go", Scope: DiffAll, Side: "additions", Start: 2, End: 3, Code: "+ x\n+ y", Body: "check", Status: "open",
		Placement: &CommentPlacement{Applicability: Moved, Side: "additions", Start: 10, End: 11}}
	output := FormatComments([]ReviewComment{comment}, false)
	if !strings.Contains(output, `selection="range" line="10" end-line="11" side="additions" scope="all" status="open" applicability="moved">`) {
		t.Fatalf("moved comment must report current lines: %s", output)
	}
}
