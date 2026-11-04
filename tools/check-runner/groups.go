package main

import "strings"

type checkGroup struct {
	Name     string
	Commands [][]string
	Build    bool
	Database bool
	Browser  bool
	Host     bool
	Native   bool
}

func groups(race bool) []checkGroup {
	goTask := "test:exec:go"
	nativeTask := "test:exec:native"
	if race {
		goTask = "test:exec:race"
		nativeTask = "test:exec:native:race"
	}
	return []checkGroup{
		{"static", [][]string{{"mise", "run", "check:exec:static"}, {"mise", "run", "check:format:go"}, {"mise", "run", "check:boundaries"}, {"mise", "run", "check:design"}, {"mise", "run", "check:artifacts"}}, false, false, false, true, false},
		{"contracts", [][]string{{"mise", "run", "check:exec:contracts"}}, true, false, false, false, false},
		{"go", [][]string{{"go", "vet", "./..."}, {"mise", "run", goTask}}, true, true, false, false, false},
		{"unit", [][]string{{"pnpm", "--filter", "@diffx/web", "test:unit"}, {"pnpm", "--filter", "@diffx/console", "test:dom"}, {"pnpm", "--recursive", "--filter", "@diffx/plugin-*", "test"}}, false, false, false, false, false},
		{"browser", [][]string{{"pnpm", "--filter", "@diffx/web", "test:browser"}, {"pnpm", "--filter", "@diffx/console", "test:browser"}}, true, true, true, false, false},
		{"conformance", [][]string{{"pnpm", "test:conformance"}}, true, true, false, false, false},
		{"docs", [][]string{{"mise", "run", "--force", "docs:check"}}, false, false, false, false, false},
		{"release", [][]string{{"pnpm", "test:release"}}, false, false, false, false, false},
		{"generated", [][]string{{"mise", "run", "generate"}, {"mise", "run", "check:exec:generated"}}, false, false, false, false, false},
		{"native", [][]string{{"go", "-C", "apps/desktop", "vet", "-mod=readonly", "./..."}, {"mise", "run", nativeTask}, {"mise", "run", "test:exec:native:release"}}, true, false, false, false, true},
		{Name: "native-window", Commands: [][]string{{"mise", "run", "test:exec:native:window"}}, Build: true, Native: true},
		{Name: "native-package", Commands: [][]string{{"mise", "run", "test:exec:native:package"}}, Build: true, Native: true},
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
	if group != "static" && group != "contracts" && group != "native-package" {
		excluded = append(excluded, "docs", "README.md")
	}
	if group == "docs" {
		excluded = append(excluded, "cmd", "internal", "test", "apps")
	}
	return excluded
}
