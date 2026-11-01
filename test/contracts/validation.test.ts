import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

test("public package test commands enter mise instead of starting host fixtures", () => {
  for (const path of [
    "../../package.json",
    "../../apps/web/package.json",
    "../../apps/console/package.json",
    "../../packages/plugin-opencode/package.json",
    "../../packages/plugin-pi/package.json",
  ]) {
    const manifest: unknown = JSON.parse(
      readFileSync(new URL(path, import.meta.url), "utf8"),
    );
    assert.ok(typeof manifest === "object" && manifest !== null);
    assert.ok("scripts" in manifest);
    const scripts = manifest.scripts;
    assert.ok(typeof scripts === "object" && scripts !== null);
    for (const [name, command] of Object.entries(scripts)) {
      if (
        (name === "test" || name.startsWith("test:")) &&
        !name.startsWith("test:exec") &&
        name !== "test:browser:install"
      ) {
        assert.ok(
          typeof command === "string" && command.startsWith("mise "),
          `${path} ${name}: route public tests through mise; raw commands belong in test:exec scripts`,
        );
      }
    }
  }
});
