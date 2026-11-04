package design

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestThemeRegistryAndSemanticMapping(t *testing.T) {
	if len(Themes) != 25 {
		t.Fatalf("theme choices=%d", len(Themes))
	}
	for _, choice := range Themes {
		for _, dark := range []bool{false, true} {
			p := PaletteFor(choice.ID, dark)
			th := Theme(choice.ID, dark)
			wantDark := choice.Mode == "dark" || (choice.Mode == "system" && dark)
			if th.Dark != wantDark {
				t.Errorf("%s wrong appearance", choice.ID)
			}
			if th.Text != p.Color("fg") || th.Background != p.Color("bg") || th.Focus != p.Color("accent") || th.Border != p.Color("border-control") {
				t.Errorf("%s lost canonical semantic mapping", choice.ID)
			}
			if th.Radius != 4 || th.Spacing != 4 || p.Metric("control-compact") != 24 {
				t.Errorf("%s lost compact geometry", choice.ID)
			}
			for _, pair := range [][2]ui.Color{{th.Text, th.Background}, {th.TextMuted, th.Background}, {th.AccentText, th.Accent}, {th.Danger, th.Background}, {th.Success, th.Background}, {th.Warning, th.Background}} {
				if ratio := contrast(pair[0], pair[1]); ratio < 4.5 {
					t.Errorf("%s contrast %.2f: %v / %v", choice.ID, ratio, pair[0], pair[1])
				}
			}
			for _, fg := range []ui.Color{th.Text, th.TextMuted, th.Danger, th.Warning, th.Success} {
				if ratio := contrast(fg, th.Surface); ratio < 4.5 {
					t.Errorf("%s panel text contrast %.2f", choice.ID, ratio)
				}
			}
		}
	}
	if Theme("missing", true).Background != Theme("dark", false).Background {
		t.Fatal("unknown choice must follow system")
	}
}

func TestCodeThemeChoicesUseNativeColorsAndCanonicalPalettes(t *testing.T) {
	if len(CodeThemes) != 5 {
		t.Fatal("five separate code choices required")
	}
	for _, choice := range Themes {
		for _, code := range CodeThemes {
			ct := CodeTheme(code.ID, choice.ID, false)
			if ct.Background.A != 255 || ct.Text.A == 0 || ct.Tokens[editor.TokenKeyword].Color.A == 0 || ct.Tokens[editor.TokenString].Color.A == 0 {
				t.Errorf("%s/%s missing code colors", choice.ID, code.ID)
			}
			if ct.Selection.A == 0 || ct.Added.A == 0 || ct.Deleted.A == 0 {
				t.Errorf("%s/%s missing diff colors", choice.ID, code.ID)
			}
			for kind, style := range ct.Tokens {
				if style.Color.A != 0 && contrast(style.Color.Over(ct.Background), ct.Background) < 4.5 {
					t.Errorf("%s/%s token %d contrast %.2f", choice.ID, code.ID, kind, contrast(style.Color.Over(ct.Background), ct.Background))
				}
			}
		}
	}
	for _, id := range []string{"catppuccin", "nord", "white"} {
		p := PaletteFor(id, false)
		ct := CodeTheme("match", id, false)
		if ct.Background != p.Color("diffs-background") || ct.Tokens[editor.TokenKeyword].Color != p.Color("diffs-token-keyword") {
			t.Errorf("%s forked palette code colors", id)
		}
	}
	for _, dark := range []bool{false, true} {
		pierre := CodeTheme("pierre", "system", dark)
		github := CodeTheme("github", "system", dark)
		vitesse := CodeTheme("vitesse", "system", dark)
		if pierre.Tokens[editor.TokenKeyword].Color == github.Tokens[editor.TokenKeyword].Color || github.Tokens[editor.TokenKeyword].Color == vitesse.Tokens[editor.TokenKeyword].Color {
			t.Error("separate code choices collapsed")
		}
	}
}

func TestApplyFollowsSystemAndAccessibilityEachFrame(t *testing.T) {
	var got ui.Theme
	preference := "system"
	tt := ui.NewTester(func(c *ui.Context) { Apply(c, preference); got = *c.Theme(); Text(c, "Review", Body) }, 300, 100)
	if got.Dark {
		t.Fatal("unexpected dark default")
	}
	tt.SetDark(true)
	if !got.Dark {
		t.Fatal("system dark change ignored")
	}
	preference = "light"
	tt.Frame()
	if got.Dark {
		t.Fatal("explicit light ignored")
	}
	tt.SetPreferences(ui.Preferences{TextScale: 1.5, HighContrast: true})
	if got.FontSize != 21 || got.TextMuted == Theme("light", false).TextMuted {
		t.Fatal("OS text scale/high contrast ignored")
	}
	preference = "system"
	tt.Frame()
	if !got.Dark {
		t.Fatal("return to system retained explicit light")
	}
}

func TestNativePrimitivesKeyboardDisabledAndCompactGeometry(t *testing.T) {
	clicks := 0
	disabled := false
	tt := ui.NewTester(func(c *ui.Context) {
		Apply(c, "light")
		Toolbar(c).Children(func() {
			Button(c, "Sync", Primary).Disabled(disabled).OnClick(func() { clicks++ })
			Button(c, "Retry", Quiet)
			Button(c, "Delete", Destructive)
		})
		Status(c, "Connected", StatusSuccess)
	}, 500, 120)
	r, ok := tt.Find("Sync")
	if !ok || r.H < 24 {
		t.Fatalf("compact control absent or too small: %+v", r)
	}
	if err := tt.Click("Sync"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Fatalf("native click count %d", clicks)
	}
	tt.Key(0, ui.KeyEnter)
	if clicks != 2 {
		t.Fatal("native Enter did not activate focused button")
	}
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("Retry") {
		t.Fatal("toolbar Right did not move native focus")
	}
	disabled = true
	tt.Frame()
	if err := tt.Click("Sync"); err != nil {
		t.Fatal(err)
	}
	if clicks != 2 {
		t.Fatal("disabled button activated")
	}
	if _, ok := tt.Find("Connected"); !ok {
		t.Fatal("status lost textual meaning")
	}
}

func TestNativeSelectedButtonAndGoTokenColors(t *testing.T) {
	selected := false
	tt := ui.NewTester(func(c *ui.Context) { Apply(c, "nord"); Button(c, "Review", Quiet).Selected(selected) }, 200, 70)
	r, ok := tt.Find("Review")
	if !ok {
		t.Fatal("button missing")
	}
	x, y := int(r.X+6), int(r.Y+3)
	before := tt.Image().RGBAAt(x, y)
	selected = true
	tt.Frame()
	after := tt.Image().RGBAAt(x, y)
	want := Theme("nord", false).Accent
	if before == after || after.R != want.R || after.G != want.G || after.B != want.B {
		t.Fatalf("Selected lost native visual feedback: before=%v after=%v want=%v", before, after, want)
	}
	language := editor.LanguageFor("example.go")
	if language == nil || language.Tokenizer == nil {
		t.Fatal("native Go tokenizer unavailable")
	}
	tokens, _ := language.Tokenizer.Tokenize(`var value = "hello"`, 0, nil)
	seenKeyword, seenString := false, false
	ct := CodeTheme("match", "nord", false)
	for _, token := range tokens {
		if token.Kind == editor.TokenKeyword {
			seenKeyword = true
			if ct.TokenColor(token.Kind) != PaletteFor("nord", false).Color("diffs-token-keyword") {
				t.Fatal("native Go keyword did not use canonical palette")
			}
		}
		if token.Kind == editor.TokenString {
			seenString = true
		}
	}
	if !seenKeyword || !seenString {
		t.Fatalf("native Go categories missing: %+v", tokens)
	}
}

func contrast(a, b ui.Color) float64 {
	lum := func(c ui.Color) float64 {
		linear := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return .2126*linear(c.R) + .7152*linear(c.G) + .0722*linear(c.B)
	}
	x, y := lum(a), lum(b)
	return (max(x, y) + .05) / (min(x, y) + .05)
}
