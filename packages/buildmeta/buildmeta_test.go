package buildmeta

import (
	"strings"
	"testing"
)

func TestPackagedIdentityOwnsReportedVersionAndSource(t *testing.T) {
	source := strings.Repeat("a", 40)
	record := "diffx-build-v1|desktop|1.2.3|" + source + "|end"
	identity := Resolve(record, "desktop", "dev", "stale")
	if identity.Version != "1.2.3" || identity.Source != source {
		t.Fatalf("packaged metadata ignored: %+v", identity)
	}
	if got := Resolve("", "cli", "dev", source); got.Version != "dev" || got.Source != source {
		t.Fatal("standalone labels changed")
	}
	if got := Resolve(record, "cli", "dev", source); got.Version != "invalid-build" {
		t.Fatal("wrong binary role accepted")
	}
}

func TestMalformedBuildRecordsAreRefused(t *testing.T) {
	for _, record := range []string{"", "diffx-build-v1|desktop|1.2.3|short|end", "diffx-build-v1|other|1.2.3|" + strings.Repeat("a", 40) + "|end", "diffx-build-v1|cli|1.2.3|" + strings.Repeat("z", 40) + "|end", "diffx-build-v1|cli|1.2.3|" + strings.Repeat("a", 40) + "|end-extra"} {
		if _, err := Parse(record); err == nil {
			t.Fatalf("invalid record accepted: %q", record)
		}
	}
}
