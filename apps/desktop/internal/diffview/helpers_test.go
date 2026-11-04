package diffview

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

const sparse = "--- a/x.go\n+++ b/x.go\n@@ -40,3 +70,3 @@ first\n before\n-old α🙂\n+new 文🙂\n after\n@@ -400,3 +700,3 @@ second\n later\n-old tail\n+new tail\n end\n"

func find(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q: %q", label, tt.Texts())
	}
	return r
}
