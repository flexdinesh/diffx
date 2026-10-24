import assert from "node:assert/strict";
import { test } from "node:test";
import {
  languageOverride,
  readDiffTheme,
  readLineDiffType,
  savedDiffTheme,
  themesFor,
} from "../src/display-options.ts";
import { PALETTE_CODE_THEME } from "../src/theme.ts";

test("validates persisted diff appearance options", () => {
  assert.equal(readDiffTheme("github"), "github");
  assert.equal(readDiffTheme("pierre"), "pierre");
  assert.equal(readDiffTheme("unknown"), "match");
  assert.equal(readLineDiffType("char"), "char");
  assert.equal(readLineDiffType("unknown"), "word-alt");
  assert.deepEqual(themesFor("catppuccin", "tokyo-night"), {
    light: "catppuccin-latte",
    dark: "catppuccin-mocha",
  });
});

test("code theme matches the app theme by default", () => {
  assert.deepEqual(themesFor("match", "system"), {
    light: "pierre-light",
    dark: "pierre-dark",
  });
  assert.deepEqual(themesFor("match", "tokyo-night"), {
    light: "tokyo-night",
    dark: "tokyo-night",
  });
  assert.deepEqual(themesFor("match", "rose-pine"), {
    light: "rose-pine-dawn",
    dark: "rose-pine-dawn",
  });
  assert.deepEqual(themesFor("match", "hackerman"), {
    light: PALETTE_CODE_THEME,
    dark: PALETTE_CODE_THEME,
  });
});

test("legacy code theme migrates its saved Pierre default to match", () => {
  const storage = (entries: Record<string, string>) => (key: string) =>
    entries[key] ?? null;
  assert.equal(savedDiffTheme(storage({})), "match");
  assert.equal(savedDiffTheme(storage({ "diff-theme": "pierre" })), "match");
  assert.equal(savedDiffTheme(storage({ "diff-theme": "vitesse" })), "vitesse");
  assert.equal(
    savedDiffTheme(storage({ "code-theme": "pierre", "diff-theme": "github" })),
    "pierre",
  );
});

test("overrides ambiguous filenames and extensionless shebangs", () => {
  assert.equal(languageOverride("ops/Dockerfile.dev"), "dockerfile");
  assert.equal(languageOverride("config/.env.local"), "dotenv");
  assert.equal(languageOverride("Justfile"), "just");
  assert.equal(languageOverride("Procfile"), "shellscript");
  assert.equal(
    languageOverride("scripts/release", "#!/usr/bin/env python3"),
    "python",
  );
  assert.equal(languageOverride("README", "plain text"), undefined);
});
