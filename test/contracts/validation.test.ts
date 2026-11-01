import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";

const root = new URL("../../", import.meta.url);

function manifests(): string[] {
  const paths = ["package.json"];
  for (const parent of ["apps", "packages"]) {
    for (const entry of readdirSync(new URL(`${parent}/`, root), {
      withFileTypes: true,
    })) {
      if (entry.isDirectory())
        paths.push(`${parent}/${entry.name}/package.json`);
    }
  }
  return paths;
}

test("package scripts run JS tooling and never delegate to the mise task runner", () => {
  for (const path of manifests()) {
    const manifest: unknown = JSON.parse(
      readFileSync(new URL(path, root), "utf8"),
    );
    assert.ok(typeof manifest === "object" && manifest !== null);
    if (!("scripts" in manifest)) continue;
    const scripts = manifest.scripts;
    assert.ok(typeof scripts === "object" && scripts !== null);
    for (const [name, command] of Object.entries(scripts)) {
      assert.ok(
        typeof command === "string" && !/(^|[\s;&|(])mise\s/.test(command),
        `${path} ${name}: repo tasks belong in mise.toml; package scripts only wrap JS tooling`,
      );
    }
  }
});
