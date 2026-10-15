package review

import "strings"

// Applicability says where a stream comment applies in a viewed snapshot.
type Applicability string

const (
	Anchored   Applicability = "anchored"
	Moved      Applicability = "moved"
	Outdated   Applicability = "outdated"
	Unchanged  Applicability = "unchanged"
	OtherScope Applicability = "other-scope"
	Unknown    Applicability = "unknown"
)

// Inline reports whether the comment renders on lines of the viewed snapshot.
func (applicability Applicability) Inline() bool {
	return applicability == Anchored || applicability == Moved
}

// Stale reports whether the comment was written against code that changed.
func (applicability Applicability) Stale() bool {
	return applicability == Outdated || applicability == Unchanged
}

type CommentPlacement struct {
	Applicability Applicability `json:"applicability"`
	Side          string        `json:"side"`
	Start         int           `json:"start"`
	End           int           `json:"end"`
}

// PatchReader returns the viewed snapshot's stored patch for one of its files.
type PatchReader func(ChangedFile) (FilePatch, error)

// Locate places a stream comment in the viewed snapshot of a scope. Line
// comments re-anchor by content: the preserved code block must occur exactly
// once on the comment's side, preferring a match whose captured surrounding
// lines also match. Ambiguity never anchors.
func Locate(comment ReviewComment, viewed *RepositoryDiff, patch PatchReader) (CommentPlacement, error) {
	original := CommentPlacement{Side: comment.Side, Start: comment.Start, End: comment.End}
	placed := func(applicability Applicability) CommentPlacement {
		original.Applicability = applicability
		return original
	}
	if viewed == nil || viewed.ID == "" || comment.DiffID == "" {
		return placed(Unknown), nil
	}
	if comment.Scope != viewed.Mode {
		return placed(OtherScope), nil
	}
	if comment.VersionID != "" && comment.VersionID == viewed.VersionID {
		for _, file := range viewed.Files {
			if file.Path == comment.Path && file.Fingerprint == comment.Fingerprint {
				return placed(Anchored), nil
			}
		}
	}
	file, found := FileFor(comment.Path, viewed.Files)
	if !found {
		return placed(Outdated), nil
	}
	if comment.Target == "file" {
		return CommentPlacement{Applicability: Anchored, Side: "additions"}, nil
	}
	block := codeContent(comment.Code)
	if len(block) == 0 {
		return placed(Outdated), nil
	}
	preview, err := patch(file)
	if err != nil {
		return CommentPlacement{}, err
	}
	lines := sideLines(preview.Patch, comment.Side)
	content := func(line int) (string, bool) {
		value, ok := lines[line]
		return strings.TrimSuffix(lineContent(value), "\r"), ok
	}
	var before, after []string
	if comment.Anchor != nil {
		before, after = anchorContent(comment.Anchor.Before), anchorContent(comment.Anchor.After)
	}
	blockMatches, contextMatches := []int{}, []int{}
	for start := range lines {
		if !matches(content, start, block) {
			continue
		}
		blockMatches = append(blockMatches, start)
		if matches(content, start-len(before), before) && matches(content, start+len(block), after) {
			contextMatches = append(contextMatches, start)
		}
	}
	for _, candidates := range [][]int{contextMatches, blockMatches} {
		if len(candidates) == 1 {
			applicability := Moved
			if candidates[0] == comment.Start {
				applicability = Anchored
			}
			return CommentPlacement{Applicability: applicability, Side: comment.Side, Start: candidates[0], End: candidates[0] + len(block) - 1}, nil
		}
	}
	if preview.Contents != nil {
		contents := preview.Contents.After
		if comment.Side == "deletions" {
			contents = preview.Contents.Before
		}
		full := strings.Split(contents, "\n")
		fullContent := func(line int) (string, bool) {
			if line < 1 || line > len(full) {
				return "", false
			}
			return strings.TrimSuffix(full[line-1], "\r"), true
		}
		found := []int{}
		for start := 1; start+len(block)-1 <= len(full); start++ {
			if matches(fullContent, start, block) {
				found = append(found, start)
			}
		}
		if len(found) == 1 {
			return CommentPlacement{Applicability: Unchanged, Side: comment.Side, Start: found[0], End: found[0] + len(block) - 1}, nil
		}
	}
	return placed(Outdated), nil
}

// FileFor finds a comment's file by path, or by a rename away from that path.
func FileFor(path string, files []ChangedFile) (ChangedFile, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}
	for _, file := range files {
		if file.OldPath != nil && *file.OldPath == path {
			return file, true
		}
	}
	return ChangedFile{}, false
}

func matches(content func(int) (string, bool), start int, expected []string) bool {
	for offset, want := range expected {
		got, ok := content(start + offset)
		if !ok || got != want {
			return false
		}
	}
	return true
}

func codeContent(code string) []string {
	if code == "" {
		return nil
	}
	lines := strings.Split(code, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(lineContent(line), "\r")
	}
	return lines
}

func anchorContent(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(line, "\r")
	}
	return lines
}
