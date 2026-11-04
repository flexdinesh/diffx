package app

import "testing"

func TestNativeThemeIntegrationKeepsReviewControlsAccessible(t *testing.T) {
	for _, theme := range []string{"dark", "catppuccin", "flexoki-light"} {
		t.Run(theme, func(t *testing.T) {
			a, _ := fixture(t)
			a.prefs.Theme = theme
			tt := nativeTester(t, a, 1280, 800)
			click(t, tt, "Leave review comment on file src/one.go")
			click(t, tt, "Comment body")
			tt.Type("Theme preserves readable review controls")
			if !tt.HasText("File review comment") {
				t.Fatal("native comment controls disappeared after theme resolution")
			}
			tt.SetScale(1.5)
			tt.Frame()
			uiEvidence(t, tt, "theme-"+theme)
			if _, ok := tt.Find("Save comment"); !ok {
				t.Fatal("theme hid the comment save action")
			}
		})
	}
}
