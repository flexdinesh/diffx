// Package design adapts canonical Diffx tokens to native MyGo primitives.
package design

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type ThemeChoice struct{ ID, Label, Kind, Mode string }
type Choice struct{ ID, Label string }
type tokenSet struct {
	dark    bool
	colors  map[string]uint32
	metrics map[string]float32
}
type Palette struct{ tokens tokenSet }

// PaletteFor resolves system appearance. Unknown saved choices follow system.
// Palette maps are private so consumers cannot mutate global canonical data.
func PaletteFor(preference string, systemDark bool) Palette {
	if p, ok := generatedPalettes[preference]; ok {
		return Palette{p}
	}
	if systemDark {
		return Palette{generatedPalettes["dark"]}
	}
	return Palette{generatedPalettes["light"]}
}
func (p Palette) Dark() bool { return p.tokens.dark }
func (p Palette) Color(name string) ui.Color {
	v, ok := p.tokens.colors[name]
	if !ok {
		panic("design: unknown color token " + name)
	}
	return ui.Color{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}
func (p Palette) Metric(name string) float32 {
	v, ok := p.tokens.metrics[name]
	if !ok {
		panic("design: unknown metric token " + name)
	}
	return v
}

func Theme(preference string, systemDark bool) ui.Theme {
	p := PaletteFor(preference, systemDark)
	return ui.Theme{Dark: p.Dark(), Background: p.Color("bg"), Surface: p.Color("panel"), SurfaceHover: p.Color("hover"), SurfacePressed: p.Color("accent-bg"), Border: p.Color("border-control"), Text: p.Color("fg"), TextMuted: p.Color("muted"), Accent: p.Color("accent"), AccentHover: p.Color("accent-hover"), AccentPressed: p.Color("accent-hover"), AccentText: p.Color("on-accent"), Danger: p.Color("error"), Warning: p.Color("warning"), Success: p.Color("success"), Selection: p.Color("accent-bg"), Focus: p.Color("focus-outline-color"), Inverse: p.Color("fg"), InverseText: p.Color("bg"), Scrollbar: p.Color("scrollbar-thumb"), ScrollbarWidth: p.Metric("scrollbar-size"), Radius: p.Metric("radius-control"), Spacing: p.Metric("space-1"), FontSize: 14}
}

// Apply must be called once at the beginning of each frame, before any other
// SetTheme. MyGo starts each frame with the current system appearance.
func Apply(c *ui.Context, preference string) {
	t := Theme(preference, c.Theme().Dark)
	prefs := c.Preferences()
	if prefs.TextScale > 0 {
		t.FontSize *= prefs.TextScale
	}
	if prefs.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
	}
	c.SetTheme(&t)
}

var CodeThemes = []Choice{{"match", "Match app theme"}, {"pierre", "Pierre"}, {"github", "GitHub"}, {"vitesse", "Vitesse"}, {"catppuccin", "Catppuccin"}}

// CodeTheme supplies colors only. The renderer owns native Go tokenization,
// patch coordinates, wrapping and measured geometry.
func CodeTheme(choice, preference string, systemDark bool) *editor.Theme {
	p := PaletteFor(preference, systemDark)
	t := Theme(preference, systemDark)
	ct := editor.DefaultTheme(&t)
	mode := "light"
	if p.Dark() {
		mode = "dark"
	}
	if choice == "catppuccin" {
		id := "catppuccin-latte"
		if p.Dark() {
			id = "catppuccin"
		}
		p = PaletteFor(id, systemDark)
		ct = paletteCode(p, &t)
	} else if choice == "match" && hasSyntax(p) {
		ct = paletteCode(p, &t)
	} else {
		if choice != "github" && choice != "vitesse" {
			choice = "pierre"
		}
		source := generatedCodeThemes[choice+"-"+mode]
		ct.Background, ct.Text, ct.Tokens = source.Background, source.Text, source.Tokens
	}
	// Status and anchors keep their semantic meaning across code overrides.
	ct.Added, ct.Deleted, ct.Modified = p.Color("success"), p.Color("error"), p.Color("warning")
	ct.Error, ct.Warning, ct.Info, ct.Hint = p.Color("error"), p.Color("warning"), p.Color("accent"), p.Color("muted")
	ct.LineNumber, ct.ActiveLineNumber = p.Color("muted"), ct.Text
	ct.Selection = t.Accent.Alpha(0.25)
	ct.InactiveSelection = t.Accent.Alpha(0.12)
	ct.Cursor, ct.MatchingBracket = t.Accent, t.Focus
	ct.IndentGuide, ct.WrapGuide = p.Color("border"), p.Color("border")
	ct.ActiveLine = p.Color("hover").Alpha(0.25)
	ct.Tokens[editor.TokenStrong].Bold = true
	ct.Tokens[editor.TokenEmphasis].Italic = true
	return ct
}
func hasSyntax(p Palette) bool { _, ok := p.tokens.colors["diffs-token-keyword"]; return ok }
func paletteCode(p Palette, t *ui.Theme) *editor.Theme {
	ct := editor.DefaultTheme(t)
	ct.Background, ct.Text = p.Color("diffs-background"), p.Color("diffs-foreground")
	for _, m := range []struct {
		kind editor.TokenKind
		role string
	}{
		{editor.TokenKeyword, "keyword"}, {editor.TokenOperator, "keyword"}, {editor.TokenType, "constant"}, {editor.TokenFunction, "function"}, {editor.TokenVariable, "parameter"}, {editor.TokenProperty, "parameter"}, {editor.TokenConstant, "constant"}, {editor.TokenNumber, "constant"}, {editor.TokenString, "string"}, {editor.TokenEscape, "string-expression"}, {editor.TokenRegexp, "string"}, {editor.TokenComment, "comment"}, {editor.TokenPunctuation, "punctuation"}, {editor.TokenTag, "keyword"}, {editor.TokenAttribute, "constant"}, {editor.TokenMeta, "function"}, {editor.TokenHeading, "function"}, {editor.TokenLink, "link"}, {editor.TokenCode, "string"}, {editor.TokenInserted, "inserted"}, {editor.TokenDeleted, "deleted"}, {editor.TokenInvalid, "deleted"},
	} {
		ct.Tokens[m.kind].Color = p.Color("diffs-token-" + m.role)
	}
	return ct
}
