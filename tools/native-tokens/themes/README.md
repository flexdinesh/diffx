# Native code colors

These source JSON color assets match the packages pinned in pnpm-lock.yaml:

- `pierre-light.json`, `pierre-dark.json`: `@pierre/theme` 2.0.0, Apache-2.0, copyright Pierre Computer Company. Original files `themes/<name>.json` from https://registry.npmjs.org/@pierre/theme/-/theme-2.0.0.tgz. Full license in `LICENSE-theme-2.0.0`.
- `github-light.json`, `github-dark.json`, `vitesse-light.json`, `vitesse-dark.json`: `@shikijs/themes` 4.4.3, MIT. Original JSON payloads in `dist/<name>.mjs` from https://registry.npmjs.org/@shikijs/themes/-/themes-4.4.3.tgz, decoded without executing JavaScript. Full license in `LICENSE-themes-4.4.3`. GitHub themes originate at https://github.com/primer/github-vscode-theme (MIT, `LICENSE-github-theme`); Vitesse at https://github.com/antfu/vscode-theme-vitesse (MIT, `LICENSE-vitesse-theme`).

Only color data enters generated Go. Native MyGo Go tokenizers replace Shiki's language runtime. The generator maps representative TextMate scopes to MyGo categories, preserving native syntax semantics. Colors are close ports, not TextMate rendering parity. All 22 app palettes, including explicit Catppuccin light/dark, use canonical CSS data rather than forked palette assets. Base Match and Pierre use the pinned Pierre colors. GitHub and Vitesse overrides use these vendored assets. No network or JS is needed to regenerate.

Native token colors composite source alpha against the code background. Where a source token falls below 4.5:1, generation makes the smallest sRGB mix toward black or white that reaches that contrast after channel rounding. Readable tokens and canonical CSS palettes stay unchanged. These adjustments concern the coarser native syntax port only; the vendored source assets retain upstream values.

Run `go run ./tools/native-tokens` from the repository root. `-check` compares exact deterministic output. CSS parsing accepts hex, variables and sRGB color mixes; native geometry accepts rem/px and the canonical simple calc expressions. Unsupported relevant expressions fail generation. Browser media queries do not govern native window geometry.
