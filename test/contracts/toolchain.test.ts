import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));

function read(path: string) {
  return readFileSync(join(root, path), "utf8");
}

function capture(text: string, pattern: RegExp, label: string) {
  const value = pattern.exec(text)?.[1];
  assert.ok(value, `${label} not found`);
  return value;
}

const mise = read("mise.toml");
const minVersion = capture(
  mise,
  /^min_version = "([^"]+)"$/m,
  "mise.toml min_version",
);
const goVersion = capture(mise, /^go = "([^"]+)"$/m, "mise.toml go pin");

test("Docker builds with the mise Go pin", () => {
  assert.equal(
    capture(
      read("Dockerfile"),
      /^FROM golang:(\S+) /m,
      "Dockerfile golang tag",
    ),
    goVersion,
  );
});

test("workflows install the mise version that mise.toml requires", () => {
  const workflows = join(root, ".github/workflows");
  for (const name of readdirSync(workflows)) {
    const text = read(join(".github/workflows", name));
    const uses = text.match(/uses: jdx\/mise-action@/g)?.length ?? 0;
    const versions = [
      ...text.matchAll(
        /uses: jdx\/mise-action@\S+\n\s+with:\n\s+version: (\S+)/g,
      ),
    ].map((match) => match[1]);
    assert.equal(
      versions.length,
      uses,
      `${name}: every mise-action step pins version`,
    );
    for (const version of versions) {
      assert.equal(version, minVersion, `${name}: mise-action version`);
    }
  }
});

test("development docs require the mise version that mise.toml requires", () => {
  assert.equal(
    capture(
      read("docs/development.md"),
      /\[mise\]\(https:\/\/mise\.jdx\.dev\) (\S+) or later/,
      "docs mise requirement",
    ),
    minVersion,
  );
});
