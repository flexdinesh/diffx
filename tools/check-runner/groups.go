package main

import "strings"

type checkGroup struct {
	Name     string
	Commands [][]string
	Build    bool
	Database bool
	Browser  bool
}

func groups(race bool) []checkGroup {
	goTask := "test:go"
	if race {
		goTask = "test:race"
	}
	return []checkGroup{
		{"static", [][]string{{"mise", "run", "check:exec:static"}, {"mise", "run", "check:exec:go:static"}}, true, false, false},
		{"contracts", [][]string{{"mise", "run", "check:exec:contracts"}}, true, false, false},
		{"go", [][]string{{"mise", "run", goTask}}, true, true, false},
		{"unit", [][]string{{"pnpm", "--filter", "@diffx/web", "test:unit"}, {"pnpm", "--filter", "@diffx/console", "test:dom"}, {"pnpm", "--recursive", "--filter", "@diffx/plugin-*", "test"}}, false, false, false},
		{"browser", [][]string{{"pnpm", "--filter", "@diffx/web", "test:browser"}, {"pnpm", "--filter", "@diffx/console", "test:browser"}}, true, true, true},
		{"conformance", [][]string{{"pnpm", "test:conformance"}}, true, true, false},
		{"docs", [][]string{{"mise", "run", "--force", "docs:check"}}, false, false, false},
		{"release", [][]string{{"mise", "run", "test:release"}}, false, false, false},
		{"generated", [][]string{{"mise", "run", "generate"}, {"mise", "run", "check:exec:generated"}}, false, false, false},
	}
}

// Unknown inputs participate in every group. Only classified, unrelated trees
// are omitted. The same predicate drives the fingerprint and container source.
func groupInput(group, path string) bool {
	for _, excluded := range groupExclusions(group) {
		if path == excluded || strings.HasPrefix(path, excluded+"/") {
			return false
		}
	}
	return true
}

func groupExclusions(group string) []string {
	excluded := []string{".git"}
	if group != "docs" && group != "static" {
		excluded = append(excluded, "docs-website")
	}
	if group != "static" && group != "contracts" {
		excluded = append(excluded, "docs", "README.md")
	}
	if group == "docs" {
		excluded = append(excluded, "cmd", "internal", "test", "apps")
	}
	return excluded
}
