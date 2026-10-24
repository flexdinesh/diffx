import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  readThemePreference,
  resolveTheme,
  swatchPalette,
  themePalette,
  THEMES,
} from "../src/theme.ts";

test("theme preference defaults invalid and missing values to system", () => {
  assert.equal(readThemePreference(null), "system");
  assert.equal(readThemePreference("unknown"), "system");
  assert.equal(readThemePreference("system"), "system");
  assert.equal(readThemePreference("light"), "light");
  assert.equal(readThemePreference("dark"), "dark");
});

test("system theme resolves from the operating system", () => {
  assert.equal(resolveTheme("system", false), "light");
  assert.equal(resolveTheme("system", true), "dark");
  assert.equal(resolveTheme("light", true), "light");
  assert.equal(resolveTheme("dark", false), "dark");
});

test("palette themes keep their fixed mode and palette", () => {
  assert.equal(readThemePreference("tokyo-night"), "tokyo-night");
  assert.equal(readThemePreference("starship"), "system");
  assert.equal(resolveTheme("tokyo-night", false), "dark");
  assert.equal(resolveTheme("flexoki-light", true), "light");
  assert.equal(themePalette("tokyo-night"), "tokyo-night");
  assert.equal(themePalette("dark"), undefined);
  assert.equal(themePalette("system"), undefined);
});

test("swatches preview base themes through their base palettes", () => {
  assert.equal(swatchPalette("light"), "diffx-light");
  assert.equal(swatchPalette("dark"), "diffx-dark");
  assert.equal(swatchPalette("nord"), "nord");
});

test("theme registry lists base themes and the 22 palette themes", () => {
  const ids = THEMES.map((theme) => theme.id);
  assert.equal(new Set(ids).size, ids.length);
  assert.deepEqual(
    THEMES.filter((theme) => theme.kind === "base").map((theme) => theme.id),
    ["system", "light", "dark"],
  );
  assert.equal(THEMES.filter((theme) => theme.kind === "palette").length, 22);
});

test("boot script lists every palette with its mode", () => {
  const html = readFileSync(new URL("../index.html", import.meta.url), "utf8");
  const listed = (name: string) =>
    [
      ...(
        html.match(new RegExp(`${name} = \\[([^\\]]*)\\]`))?.[1] ?? ""
      ).matchAll(/"([^"]+)"/g),
    ].map((match) => match[1]);
  const palettes = (mode: string) =>
    THEMES.filter(
      (theme) => theme.kind === "palette" && theme.mode === mode,
    ).map((theme) => theme.id);
  assert.deepEqual(listed("lightPalettes"), palettes("light"));
  assert.deepEqual(listed("darkPalettes"), palettes("dark"));
});
