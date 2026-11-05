import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { THEMES } from "@diffx/review-app/theme";

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
