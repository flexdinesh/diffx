package main

import "strings"

type checkGroup struct {
	Name     string
	Commands [][]string
	Build    bool
	Database bool
	Browser  bool
	Host     bool
	Desktop  bool
}

func groups(race bool) []checkGroup {
	goTask := "test:exec:go"
	desktopTask := "test:exec:desktop"
	if race {
		goTask = "test:exec:race"
		desktopTask = "test:exec:desktop:race"
	}
	return []checkGroup{
		{"static", [][]string{{"mise", "run", "check:exec:static"}, {"mise", "run", "check:format:go"}, {"mise", "run", "check:boundaries"}, {"mise", "run", "check:design"}, {"mise", "run", "check:artifacts"}}, false, false, false, true, false},
		{"contracts", [][]string{{"mise", "run", "check:exec:contracts"}}, true, false, false, false, false},
		{"go", [][]string{{"go", "vet", "./..."}, {"mise", "run", goTask}}, true, true, false, false, false},
		{"unit", [][]string{{"pnpm", "--filter", "@diffx/review-app", "test:unit"}, {"pnpm", "--filter", "@diffx/desktop", "test:unit"}, {"pnpm", "--filter", "@diffx/web", "test:unit"}, {"pnpm", "--filter", "@diffx/console", "test:dom"}, {"pnpm", "--recursive", "--filter", "@diffx/plugin-*", "test"}}, false, false, false, false, false},
		{"browser", [][]string{{"pnpm", "--filter", "@diffx/web", "test:browser"}, {"pnpm", "--filter", "@diffx/console", "test:browser"}}, true, true, true, false, false},
		{"conformance", [][]string{{"pnpm", "test:conformance"}}, true, true, false, false, false},
		{"docs", [][]string{{"mise", "run", "--force", "docs:check"}}, false, false, false, false, false},
		{"release", [][]string{{"pnpm", "test:release"}}, false, false, false, false, false},
		{"generated", [][]string{{"mise", "run", "generate"}, {"mise", "run", "check:exec:generated"}}, false, false, false, false, false},
		{"desktop", [][]string{{"go", "-C", "apps/desktop", "vet", "-mod=readonly", "./..."}, {"mise", "run", desktopTask}, {"mise", "run", "test:exec:desktop:release"}}, true, false, false, false, true},
		{Name: "desktop-window", Commands: [][]string{{"mise", "run", "test:exec:desktop:window"}}, Build: true, Desktop: true},
		{Name: "desktop-package", Commands: [][]string{{"mise", "run", "test:exec:desktop:package"}}, Build: true, Desktop: true},
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
	if group != "static" && group != "contracts" && group != "desktop-package" {
		excluded = append(excluded, "docs", "README.md")
	}
	if group == "docs" {
		excluded = append(excluded, "cmd", "internal", "test", "apps")
	}
	return excluded
}
