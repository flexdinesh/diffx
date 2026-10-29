package review

import "testing"

// Stored marks carry by this digest; it must never change.
func TestPatchDigestIsStable(t *testing.T) {
	if got := PatchDigest(FilePatch{Patch: "@@ -1 +1 @@\n-a\n+b\n"}); got != "e66fa3de3ec593c4b23137378a0b59e5d819381338db5491eac41874b05c698c" {
		t.Fatalf("patch digest = %q", got)
	}
}
