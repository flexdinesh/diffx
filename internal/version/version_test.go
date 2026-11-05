package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestStringDefaultsToDevelopmentVersion(t *testing.T) {
	setBuildMetadata(t, "dev", "", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true)
	if got, want := String(), "diffx dev"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestBuildIncludesDevelopmentRevisionAndDirtyState(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.time", Value: "2026-10-29T00:00:00Z"}, {Key: "vcs.modified", Value: "true"}}}
	setBuildMetadata(t, "dev", "", "", info, true)
	got := Build()
	if got.Version != "dev" || got.Commit != "abc123" || !got.Modified || got.Date != "2026-10-29T00:00:00Z" {
		t.Fatalf("missing build facts: %+v", got)
	}
	if String() != "diffx dev" {
		t.Fatal("diagnostics changed CLI version formatting")
	}
}

func TestStringIncludesReleaseMetadata(t *testing.T) {
	setBuildMetadata(t, "0.1.0", "abc123", "2026-09-17T00:00:00Z", nil, false)
	if got, want := String(), "diffx 0.1.0 abc123 2026-09-17T00:00:00Z"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringUsesTaggedGoInstallMetadata(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.2.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "def456"},
			{Key: "vcs.time", Value: "2026-09-17T01:00:00Z"},
		},
	}
	setBuildMetadata(t, "dev", "", "", info, true)
	if got, want := String(), "diffx 0.2.0 def456 2026-09-17T01:00:00Z"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestPackagedRecordOwnsVersionAndSource(t *testing.T) {
	setBuildMetadata(t, "dev", "", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-source"}}, true)
	source := strings.Repeat("a", 40)
	BuildIdentity = "diffx-build-v1|cli|1.2.3|" + source + "|end"
	if Number() != "1.2.3" || Explicit() != "1.2.3" || Build().Commit != source {
		t.Fatal("packaged identity lost to inferred Go metadata")
	}
}

func setBuildMetadata(t *testing.T, version, commit, date string, info *debug.BuildInfo, ok bool) {
	t.Helper()
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	oldIdentity := BuildIdentity
	oldReadBuildInfo := readBuildInfo
	Version, Commit, Date = version, commit, date
	BuildIdentity = ""
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, ok }
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
		BuildIdentity = oldIdentity
		readBuildInfo = oldReadBuildInfo
	})
}
