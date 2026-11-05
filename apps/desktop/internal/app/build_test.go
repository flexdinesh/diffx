package app

import (
	"strings"
	"testing"
)

func TestPackagedRecordOwnsDesktopVersion(t *testing.T) {
	previous := BuildIdentity
	t.Cleanup(func() { BuildIdentity = previous })
	BuildIdentity = "diffx-build-v1|desktop|1.2.3|" + strings.Repeat("a", 40) + "|end"
	if buildLabel() != "1.2.3" {
		t.Fatal("desktop did not report retained package identity")
	}
}
