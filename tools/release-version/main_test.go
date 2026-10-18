package main

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestSelectTagStartsSeriesAtPatchZero(t *testing.T) {
	series := mustParseSeries(t, "0.1")
	if got, want := selectedTag(t, series, nil, nil), "v0.1.0"; got != want {
		t.Fatalf("selectTag() = %q, want %q", got, want)
	}
}

func TestSelectTagIncrementsHighestPatch(t *testing.T) {
	series := mustParseSeries(t, "0.1")
	tags := []string{"v0.1.1", "v0.1.9", "v0.1.3", "v0.2.20", "v0.1.10-beta.1"}
	if got, want := selectedTag(t, series, tags, nil), "v0.1.10"; got != want {
		t.Fatalf("selectTag() = %q, want %q", got, want)
	}
}

func TestSelectTagReusesReleaseAtHead(t *testing.T) {
	series := mustParseSeries(t, "0.1")
	tags := []string{"v0.1.0", "v0.1.1"}
	if got, want := selectedTag(t, series, tags, []string{"v0.1.1"}), "v0.1.1"; got != want {
		t.Fatalf("selectTag() = %q, want %q", got, want)
	}
}

func TestReleaseTagsFromMatchesFirstParent(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(name, "test")
	}
	for _, name := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(name, "test@example.com")
	}
	git(t, "init", "--quiet")
	git(t, "commit", "--quiet", "--allow-empty", "--message", "main")
	git(t, "tag", "v0.1.0")
	release := git(t, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "release: v0.1.1")
	git(t, "tag", "v0.1.1", release)
	tags, err := releaseTagsFrom("HEAD", []string{"v0.1.0", "v0.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"v0.1.1"}; !slices.Equal(tags, want) {
		t.Fatalf("releaseTagsFrom() = %q, want %q", tags, want)
	}
}

func TestSelectTagStartsChangedSeriesAtPatchZero(t *testing.T) {
	series := mustParseSeries(t, "1.0")
	tags := []string{"v0.1.8", "v0.2.4"}
	if got, want := selectedTag(t, series, tags, nil), "v1.0.0"; got != want {
		t.Fatalf("selectTag() = %q, want %q", got, want)
	}
}

func TestParseSeriesRejectsFullOrPaddedVersions(t *testing.T) {
	for _, value := range []string{"0.1.0", "v0.1", "01.2", "1.02", "next"} {
		if _, err := parseSeries(value); err == nil {
			t.Fatalf("parseSeries(%q) succeeded", value)
		}
	}
}

func mustParseSeries(t *testing.T, value string) releaseSeries {
	t.Helper()
	series, err := parseSeries(value)
	if err != nil {
		t.Fatal(err)
	}
	return series
}

func selectedTag(t *testing.T, series releaseSeries, tags, headTags []string) string {
	t.Helper()
	tag, err := selectTag(series, tags, headTags)
	if err != nil {
		t.Fatal(err)
	}
	return tag
}

func git(t *testing.T, arguments ...string) string {
	t.Helper()
	output, err := exec.Command("git", arguments...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return strings.TrimSpace(string(output))
}
