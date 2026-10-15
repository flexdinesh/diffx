package review

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

// sideLines maps one side's hunk line numbers to preserved comment code
// ("+ ", "- " or "  " followed by content). Context lines belong to both sides.
func sideLines(patch, side string) map[int]string {
	selected := make(map[int]string)
	oldLine, newLine, inHunk := 0, 0, false
	for _, line := range strings.Split(patch, "\n") {
		if match := hunkHeader.FindStringSubmatch(line); len(match) == 5 {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[3])
			inHunk = true
			continue
		}
		if !inHunk || line == "" || strings.HasPrefix(line, "\\ No newline") {
			continue
		}
		prefix := line[0]
		switch prefix {
		case ' ':
			if side == "additions" {
				selected[newLine] = "  " + line[1:]
			} else {
				selected[oldLine] = "  " + line[1:]
			}
			oldLine++
			newLine++
		case '+':
			if side == "additions" {
				selected[newLine] = "+ " + line[1:]
			}
			newLine++
		case '-':
			if side == "deletions" {
				selected[oldLine] = "- " + line[1:]
			}
			oldLine++
		}
	}
	return selected
}

func PatchContext(patch, side string, start, end int) (string, bool) {
	if start > end {
		start, end = end, start
	}
	if start < 1 || end-start >= 200 || (side != "additions" && side != "deletions") {
		return "", false
	}
	selected := sideLines(patch, side)
	lines := make([]string, 0, end-start+1)
	for line := start; line <= end; line++ {
		value, ok := selected[line]
		if !ok {
			return "", false
		}
		lines = append(lines, value)
	}
	return strings.Join(lines, "\n"), true
}

const anchorLines = 3

// PatchAnchor captures up to three contiguous same-side hunk lines before and
// after a selection, as content without diff prefixes.
func PatchAnchor(patch, side string, start, end int) *CommentAnchor {
	lines := sideLines(patch, side)
	before := make([]string, 0, anchorLines)
	for line := start - 1; line >= start-anchorLines; line-- {
		value, ok := lines[line]
		if !ok {
			break
		}
		before = append([]string{strings.TrimSuffix(lineContent(value), "\r")}, before...)
	}
	after := make([]string, 0, anchorLines)
	for line := end + 1; line <= end+anchorLines; line++ {
		value, ok := lines[line]
		if !ok {
			break
		}
		after = append(after, strings.TrimSuffix(lineContent(value), "\r"))
	}
	return &CommentAnchor{Before: strings.Join(before, "\n"), After: strings.Join(after, "\n")}
}

// lineContent strips the two-character diff prefix of preserved comment code.
func lineContent(line string) string {
	if len(line) < 2 {
		return ""
	}
	return line[2:]
}
