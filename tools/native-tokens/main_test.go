package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalGenerationAndDrift(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := generate(root, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "apps/web/src/theme.ts"))
	if err != nil {
		t.Fatal(err)
	}
	choices, err := registry(string(data))
	if err != nil {
		t.Fatal(err)
	}
	palettes := 0
	for _, c := range choices {
		if c.Kind == "palette" {
			palettes++
		}
	}
	if len(choices) != 25 || palettes != 22 {
		t.Fatalf("registry choices=%d palettes=%d", len(choices), palettes)
	}
	// Prove that check detects a real stale generated value, rather than only
	// asserting that the generator can format its own output.
	dir := t.TempDir()
	path := filepath.Join(dir, "generated.go")
	if err := output(dir, "generated.go", []byte("package design\nconst Accent=1\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := output(dir, "generated.go", []byte("package design\nconst Accent=2\n"), true); err == nil {
		t.Fatal("missed changed canonical value")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "Accent = 1") {
		t.Fatal("check modified file")
	}
}

func TestCanonicalCascadeEquivalence(t *testing.T) {
	css, err := os.ReadFile("../../packages/ui/src/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := stylesheet(string(css))
	if err != nil {
		t.Fatal(err)
	}
	// Literal browser token values plus inherited var() and color-mix(). These
	// fixtures cover base/override order, not a duplicated full palette system.
	for _, tc := range []struct {
		theme, mode, key string
		want             rgba
	}{
		{"light", "light", "diff-file-header", rgba{248, 249, 251, 255}},
		{"dark", "dark", "border-muted", rgba{48, 49, 55, 255}},
		{"catppuccin", "dark", "diffs-token-inserted", rgba{166, 227, 161, 255}},
		{"rose-pine", "light", "diff-file-header", rgba{237, 231, 225, 255}},
		{"white", "light", "fg", rgba{0, 0, 0, 255}},
	} {
		kind := "palette"
		if tc.theme == "light" || tc.theme == "dark" {
			kind = "base"
		}
		vars, err := cascade(rules, choice{ID: tc.theme, Kind: kind, Mode: tc.mode})
		if err != nil {
			t.Fatal(err)
		}
		got, err := resolveColor(tc.key, vars, map[string]bool{})
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s %s: %+v != %+v", tc.theme, tc.key, got, tc.want)
		}
		metric, err := resolveMetric("panel-header-height", vars, map[string]bool{})
		if err != nil || metric != 36 {
			t.Fatalf("header geometry: %v %v", metric, err)
		}
	}
}

func TestPremultipliedSRGBMixAndFailClosed(t *testing.T) {
	vars := map[string]string{"muted": "#686c77", "thumb": "color-mix(in srgb, var(--muted) 40%, transparent)", "a": "var(--b)", "b": "var(--a)"}
	c, err := resolveColor("thumb", vars, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if c.r != 104 || c.g != 108 || c.b != 119 || c.a != 102 {
		t.Fatalf("transparent mix lost unpremultiplied color: %+v", c)
	}
	for _, v := range []string{"oklch(50% .1 25)", "var(--missing)", "color-mix(in oklab, #fff 40%, #000)", "rgb(0,0,0)", "var(--a)"} {
		if _, err := color(v, vars, map[string]bool{}); err == nil {
			t.Errorf("accepted unsupported/cyclic color %q", v)
		}
	}
	for _, v := range []string{"min(320px,88vw)", "calc(2 * unknown)", "var(--missing)", "calc(2 / 0)"} {
		if _, err := metric(v, vars, map[string]bool{}); err == nil {
			t.Errorf("accepted unsupported metric %q", v)
		}
	}
}

func TestRegistryRejectsExpressionsAndMissingFields(t *testing.T) {
	entry := `export const THEMES = [{id:"system",label:"Auto",kind:"base",mode:"system",code:pierre}];`
	if _, err := registry(entry); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		strings.Replace(entry, `label:"Auto"`, `label:label()`, 1),
		strings.Replace(entry, `id:"system"`, `id:prefix+"system"`, 1),
		strings.Replace(entry, `code:pierre`, `code:other()`, 1),
		strings.Replace(entry, `mode:"system",`, "", 1),
		strings.Replace(entry, `id:"system"`, `...base,id:"system"`, 1),
	} {
		if _, err := registry(src); err == nil {
			t.Errorf("accepted unsupported registry: %s", src)
		}
	}
}

func TestStylesheetRejectsUntranslatedConditionsAndSelectors(t *testing.T) {
	for _, css := range []string{
		`.file { --fg: #fff; }`,
		`:root[data-palette="nord"] .file { --fg: #fff; }`,
		`@media (prefers-color-scheme: dark) { :root { --fg: #fff; } }`,
		`@supports (color: oklch(1 0 0)) { :root { --fg: #fff; } }`,
	} {
		if _, err := stylesheet(css); err == nil {
			t.Errorf("accepted untranslated CSS %s", css)
		}
	}
	if _, err := stylesheet(`@media (max-width: 767px) { :root { --page-gutter: 12px; } }`); err != nil {
		t.Fatal(err)
	}
}

func TestNativeColorPortPreservesReadableSourceAndCorrectsLowContrast(t *testing.T) {
	white := rgba{255, 255, 255, 255}
	black := rgba{0, 0, 0, 255}
	readableSource := rgba{40, 60, 90, 255}
	if got := readable(readableSource, white); got != readableSource {
		t.Fatalf("changed already readable source %+v", got)
	}
	for _, tc := range []struct{ ink, canvas rgba }{
		{rgba{25, 159, 67, 255}, white},                   // Pierre Light string.
		{rgba{106, 115, 125, 255}, rgba{36, 41, 46, 255}}, // GitHub Dark comment.
		{rgba{102, 102, 102, 255}, black},
		{rgba{117, 133, 117, 221}, rgba{18, 18, 18, 255}}, // Vitesse translucent comment.
	} {
		got := readable(tc.ink, tc.canvas)
		if ratio(got, tc.canvas) < 4.5 {
			t.Errorf("unreadable native port %+v contrast %.2f", got, ratio(got, tc.canvas))
		}
	}
}
