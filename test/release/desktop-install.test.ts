import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  readlink,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { type TestContext } from "node:test";
import { promisify } from "node:util";

const execute = promisify(execFile);
const installer = join(process.cwd(), "install-desktop.sh");
const releases = "https://github.com/flexdinesh/diffx/releases/download";

interface Options {
  os?: string;
  machine?: string;
  missingSidecar?: boolean;
  symlinkSidecar?: boolean;
  missingApp?: boolean;
  renameFailure?: boolean;
  aliasFailure?: string;
  downloadFailure?: string;
  shasum?: boolean;
}

interface Fixture {
  root: string;
  source: string;
  bin: string;
  archive: string;
  manifest: string;
  requests: string;
  extractions: string;
  app: string;
  sidecar: string;
  target: string;
  env: NodeJS.ProcessEnv;
}

const platforms = [
  { os: "Linux", machine: "x86_64", target: "linux_amd64" },
  { os: "Linux", machine: "aarch64", target: "linux_arm64" },
  { os: "Darwin", machine: "arm64", target: "darwin_arm64" },
];

for (const { os, machine, target } of platforms) {
  test(`desktop ${target}: exposes matching commands through one pointer, preserves user files`, async (t) => {
    const fixture = await createFixture(t, { os, machine });
    await install(fixture);
    const executable = await realpath(join(fixture.bin, "diffx-desktop"));
    assert.equal(await readFile(executable, "utf8"), program("desktop old"));
    assert.equal(
      await readFile(
        join(dirnamePair(executable, fixture), fixture.sidecar),
        "utf8",
      ),
      program("server old"),
    );
    assert.equal(
      await readFile(join(fixture.bin, "diffx"), "utf8"),
      program("server old"),
    );
    assert.equal(
      await readFile(join(fixture.bin, "diffx-server"), "utf8"),
      "user server",
    );
    assert.deepEqual((await readdir(fixture.bin)).sort(), [
      ".diffx-desktop-versions",
      "diffx",
      "diffx-desktop",
      "diffx-server",
    ]);
    assert.deepEqual(
      (await readFile(fixture.requests, "utf8")).trim().split("\n"),
      [
        `${releases}/edge/desktop-checksums.txt`,
        `${releases}/edge/diffx-desktop_edge_${target}.tar.gz`,
      ],
    );
    assert.equal(
      (await execute(join(fixture.bin, "diffx-desktop"))).stdout,
      "desktop old\n",
    );
    assert.equal(
      (await execute(join(fixture.bin, "diffx"))).stdout,
      "server old\n",
    );
    assert.equal(
      await readFile(
        join(dirnamePair(executable, fixture), "../previous/diffx"),
        "utf8",
      ),
      "user CLI",
    );
    await assertAliases(fixture);
    await assertClean(fixture, 1);
  });
}

test("desktop edge refuses macOS Intel before downloads", async (t) => {
  const fixture = await createFixture(t, { os: "Darwin", machine: "x86_64" });
  await assert.rejects(
    install(fixture),
    /edge does not support macOS Intel; use --version vX.Y.Z/,
  );
  assert.equal(await readFile(fixture.requests, "utf8"), "");
  assert.ok(!(await readdir(fixture.bin)).includes("diffx-desktop"));
});

test("desktop stable version installs on macOS Intel", async (t) => {
  const fixture = await createFixture(t, { os: "Darwin", machine: "x86_64" });
  await install(fixture, ["--version", "v1.2.3"]);
  assert.deepEqual(
    (await readFile(fixture.requests, "utf8")).trim().split("\n"),
    [
      `${releases}/v1.2.3/desktop-checksums.txt`,
      `${releases}/v1.2.3/diffx-desktop_1.2.3_darwin_amd64.tar.gz`,
    ],
  );
  assert.equal(
    (await execute(join(fixture.bin, "diffx-desktop"))).stdout,
    "desktop old\n",
  );
  assert.equal(
    (await execute(join(fixture.bin, "diffx"))).stdout,
    "server old\n",
  );
  await assertAliases(fixture);
  await assertClean(fixture, 1);
});

for (const { os, machine, target } of [
  ...platforms,
  { os: "Darwin", machine: "x86_64", target: "darwin_amd64" },
]) {
  test(`desktop ${target}: upgrade switches whole pair and retains old executable/server paths`, async (t) => {
    const fixture = await createFixture(t, { os, machine });
    await install(
      fixture,
      target === "darwin_amd64" ? ["--version", "v1.2.3"] : [],
    );
    const oldExecutable = await realpath(join(fixture.bin, "diffx-desktop"));
    const oldPair = dirnamePair(oldExecutable, fixture);
    const aliases = await Promise.all(
      ["diffx", "diffx-desktop"].map((name) =>
        readlink(join(fixture.bin, name)),
      ),
    );
    await writeFile(join(fixture.source, fixture.app), program("desktop new"));
    await writeFile(
      join(fixture.source, fixture.sidecar),
      program("server new"),
    );
    await archive(fixture);
    await install(fixture, ["--version", "v1.2.3"]);
    const nextExecutable = await realpath(join(fixture.bin, "diffx-desktop"));
    assert.notEqual(oldExecutable, nextExecutable);
    assert.equal(
      (await execute(join(fixture.bin, "diffx-desktop"))).stdout,
      "desktop new\n",
    );
    assert.equal(
      (await execute(join(fixture.bin, "diffx"))).stdout,
      "server new\n",
    );
    assert.deepEqual(
      await Promise.all(
        ["diffx", "diffx-desktop"].map((name) =>
          readlink(join(fixture.bin, name)),
        ),
      ),
      aliases,
    );
    await assertAliases(fixture);
    assert.equal(await readFile(oldExecutable, "utf8"), program("desktop old"));
    assert.equal(
      await readFile(join(oldPair, fixture.sidecar), "utf8"),
      program("server old"),
    );
    assert.equal(
      await readFile(
        join(dirnamePair(nextExecutable, fixture), fixture.sidecar),
        "utf8",
      ),
      program("server new"),
    );
    assert.match(
      await readFile(fixture.requests, "utf8"),
      new RegExp(`download/v1.2.3/diffx-desktop_1.2.3_${target}.tar.gz`),
    );
    await assertClean(fixture, 2);
  });
}

test("desktop failed checksum upgrade preserves an installed pair", async (t) => {
  const fixture = await createFixture(t);
  await install(fixture);
  const previous = await realpath(join(fixture.bin, "diffx-desktop"));
  const previousCLI = await realpath(join(fixture.bin, "diffx"));
  await writeFile(fixture.archive, "corrupt upgrade");
  await assert.rejects(install(fixture), /SHA-256 mismatch/);
  assert.equal(await realpath(join(fixture.bin, "diffx-desktop")), previous);
  assert.equal(await realpath(join(fixture.bin, "diffx")), previousCLI);
  assert.equal((await execute(previous)).stdout, "desktop old\n");
  await assertClean(fixture, 1);
});

test("desktop refuses invalid archives even with a matching checksum", async (t) => {
  const fixture = await createFixture(t);
  await writeFile(fixture.archive, "not a tar archive");
  await manifest(fixture);
  await assert.rejects(install(fixture), /could not list archive/);
  await assertClean(fixture, 0);
});

for (const failure of [
  "manifest-download",
  "archive-download",
  "checksum",
  "missing-checksum",
  "duplicate-checksum",
  "malformed-checksum",
  "missing-sidecar",
  "missing-app",
  "symlink-sidecar",
  "rename",
  "cli-alias",
  "desktop-alias",
]) {
  test(`desktop ${failure}: preserves previous pair and cleans staging`, async (t) => {
    const options: Options = {
      missingSidecar: failure === "missing-sidecar",
      missingApp: failure === "missing-app",
      symlinkSidecar: failure === "symlink-sidecar",
      renameFailure: failure === "rename",
    };
    if (failure === "cli-alias") options.aliasFailure = "diffx";
    if (failure === "desktop-alias") options.aliasFailure = "diffx-desktop";
    if (failure === "manifest-download")
      options.downloadFailure = "desktop-checksums.txt";
    if (failure === "archive-download") options.downloadFailure = ".tar.gz";
    const fixture = await createFixture(t, options);
    const previous = join(fixture.root, "previous pair");
    await mkdir(previous);
    await writeFile(join(previous, "diffx-desktop"), "previous desktop");
    await writeFile(join(previous, "diffx"), "previous server");
    await symlink(
      join(previous, "diffx-desktop"),
      join(fixture.bin, "diffx-desktop"),
    );
    const manifest = await readFile(fixture.manifest, "utf8");
    if (failure === "checksum")
      await writeFile(fixture.archive, "corrupt archive");
    if (failure === "missing-checksum") await writeFile(fixture.manifest, "");
    if (failure === "duplicate-checksum")
      await writeFile(fixture.manifest, manifest + manifest);
    if (failure === "malformed-checksum")
      await writeFile(
        fixture.manifest,
        manifest.replace(/[a-f0-9]{64}/g, "invalid"),
      );
    await assert.rejects(install(fixture), /diffx-desktop:/);
    assert.equal(
      await realpath(join(fixture.bin, "diffx-desktop")),
      join(previous, "diffx-desktop"),
    );
    assert.equal(
      await readFile(join(previous, "diffx-desktop"), "utf8"),
      "previous desktop",
    );
    assert.equal(
      await readFile(join(previous, "diffx"), "utf8"),
      "previous server",
    );
    assert.equal(
      await readFile(join(fixture.bin, "diffx"), "utf8"),
      "user CLI",
    );
    if (failure.includes("checksum") || failure.includes("download")) {
      assert.equal(await readFile(fixture.extractions, "utf8"), "");
    }
    await assertClean(fixture, 0);
  });
}

test("desktop relative destination with spaces and shasum fallback, no HOME", async (t) => {
  const fixture = await createFixture(t, { shasum: true });
  delete fixture.env.HOME;
  fixture.bin = join(fixture.root, "relative bin");
  await install(fixture, ["--bin-dir", "relative bin", "--version", "1.2.3"]);
  assert.equal(
    (await execute(join(fixture.bin, "diffx-desktop"))).stdout,
    "desktop old\n",
  );
  await assertClean(fixture, 1);
});

test("desktop activation failure preserves both aliases and active pair on upgrade", async (t) => {
  const fixture = await createFixture(t, { renameFailure: true });
  // Failure injection is enabled only after installing a verified old pair.
  delete fixture.env.FAIL_ACTIVATION;
  await install(fixture);
  const previous = await realpath(
    join(fixture.bin, ".diffx-desktop-versions/current"),
  );
  fixture.env.FAIL_ACTIVATION = "1";
  await writeFile(join(fixture.source, fixture.app), program("desktop new"));
  await writeFile(join(fixture.source, fixture.sidecar), program("server new"));
  await archive(fixture);
  await assert.rejects(install(fixture), /could not activate desktop pair/);
  assert.equal(
    await realpath(join(fixture.bin, ".diffx-desktop-versions/current")),
    previous,
  );
  assert.equal(
    (await execute(join(fixture.bin, "diffx"))).stdout,
    "server old\n",
  );
  assert.equal(
    (await execute(join(fixture.bin, "diffx-desktop"))).stdout,
    "desktop old\n",
  );
  await assertAliases(fixture);
  await assertClean(fixture, 1);
});

test("desktop migrates desktop-only installation and retains its private pair", async (t) => {
  const fixture = await createFixture(t);
  const old = join(fixture.bin, ".diffx-desktop-versions/install.legacy");
  await mkdir(old, { recursive: true });
  await writeFile(join(old, "diffx-desktop"), program("legacy desktop"));
  await writeFile(join(old, "diffx"), program("legacy server"));
  await symlink(join(old, "diffx-desktop"), join(fixture.bin, "diffx-desktop"));
  await install(fixture);
  await assertAliases(fixture);
  assert.equal(
    await readFile(join(old, "diffx"), "utf8"),
    program("legacy server"),
  );
  assert.equal(
    await readFile(join(fixture.bin, "diffx-desktop"), "utf8"),
    program("desktop old"),
  );
});

test("desktop restores a standalone CLI symlink after failed activation", async (t) => {
  const fixture = await createFixture(t, { renameFailure: true });
  const old = join(fixture.root, "standalone diffx");
  await writeFile(old, "standalone CLI");
  await rm(join(fixture.bin, "diffx"));
  await symlink(old, join(fixture.bin, "diffx"));
  await assert.rejects(install(fixture), /could not activate desktop pair/);
  assert.equal(await realpath(join(fixture.bin, "diffx")), old);
  assert.equal(await readFile(old, "utf8"), "standalone CLI");
  await assertClean(fixture, 0);
});

test("desktop repairs an alias overwritten by a standalone CLI installer", async (t) => {
  const fixture = await createFixture(t);
  await install(fixture);
  await rm(join(fixture.bin, "diffx"));
  await writeFile(join(fixture.bin, "diffx"), "standalone replacement");
  await install(fixture);
  await assertAliases(fixture);
  assert.equal(
    (await execute(join(fixture.bin, "diffx"))).stdout,
    "server old\n",
  );
  await assertClean(fixture, 2);
});

test("desktop refuses concurrent installation without removing its lock", async (t) => {
  const fixture = await createFixture(t);
  const lock = join(fixture.bin, ".diffx-desktop-versions/.install-lock");
  await mkdir(lock, { recursive: true });
  await assert.rejects(install(fixture), /another desktop install/);
  assert.deepEqual(await readdir(lock), []);
  assert.equal(await readFile(fixture.requests, "utf8"), "");
});

test("desktop help needs no downloads", async (t) => {
  const fixture = await createFixture(t);
  const { stdout } = await install(fixture, ["--help"]);
  assert.match(stdout, /experimental diffx-desktop/);
  assert.equal(await readFile(fixture.requests, "utf8"), "");
});

for (const args of [
  ["--unknown"],
  ["--version"],
  ["--version", "1.2.3/../bad"],
  ["--version", "1.2.3\nother"],
  ["--version", "v1.2.3-rc.1"],
  ["--bin-dir", ""],
]) {
  test(`desktop rejects arguments ${JSON.stringify(args)}`, async (t) => {
    const fixture = await createFixture(t);
    await assert.rejects(install(fixture, args), /diffx-desktop:/);
    assert.equal(await readFile(fixture.requests, "utf8"), "");
  });
}

for (const options of [{ os: "FreeBSD" }, { machine: "riscv64" }]) {
  test(`desktop rejects unsupported platform ${JSON.stringify(options)}`, async (t) => {
    const fixture = await createFixture(t, options);
    await assert.rejects(install(fixture), /unsupported/);
    assert.equal(await readFile(fixture.requests, "utf8"), "");
  });
}

test("desktop refuses launcher directories and symlinked private storage", async (t) => {
  const fixture = await createFixture(t);
  await mkdir(join(fixture.bin, "diffx-desktop"));
  await assert.rejects(install(fixture), /destination is a directory/);
  await rm(join(fixture.bin, "diffx-desktop"), { recursive: true });
  await symlink(fixture.source, join(fixture.bin, ".diffx-desktop-versions"));
  await assert.rejects(install(fixture), /must not be a symlink/);
});

function program(message: string): string {
  return `#!/bin/sh\nprintf '%s\\n' '${message}'\n`;
}

function dirnamePair(executable: string, fixture: Fixture): string {
  return executable.slice(0, -fixture.app.length - 1);
}

async function createFixture(
  t: TestContext,
  options: Options = {},
): Promise<Fixture> {
  const root = await mkdtemp(join(tmpdir(), "diffx-desktop-install-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const source = join(root, "source");
  const tools = join(root, "tools");
  const bin = join(root, "home space/.local/bin");
  for (const directory of [source, tools, bin])
    await mkdir(directory, { recursive: true });
  const darwin = options.os === "Darwin";
  const app = darwin
    ? "Diffx Desktop Experimental.app/Contents/MacOS/diffx-desktop"
    : "diffx-desktop/diffx-desktop";
  const sidecar = darwin
    ? "Diffx Desktop Experimental.app/Contents/Resources/diffx"
    : "diffx-desktop/diffx";
  await mkdir(dirname(join(source, app)), { recursive: true });
  await mkdir(dirname(join(source, sidecar)), { recursive: true });
  if (!options.missingApp)
    await writeFile(join(source, app), program("desktop old"));
  if (options.symlinkSidecar)
    await symlink("diffx-desktop", join(source, sidecar));
  else if (!options.missingSidecar)
    await writeFile(join(source, sidecar), program("server old"));
  await writeFile(join(bin, "diffx"), "user CLI");
  await writeFile(join(bin, "diffx-server"), "user server");
  const requests = join(root, "requests");
  const extractions = join(root, "extractions");
  await writeFile(requests, "");
  await writeFile(extractions, "");
  const target = `${darwin ? "darwin" : "linux"}_${["aarch64", "arm64"].includes(options.machine ?? "") ? "arm64" : "amd64"}`;
  const fixture: Fixture = {
    root,
    source,
    bin,
    app,
    sidecar,
    requests,
    extractions,
    target,
    archive: join(root, "archive.tar.gz"),
    manifest: join(root, "manifest"),
    env: { ...process.env, HOME: join(root, "home space"), PATH: tools },
  };
  await archive(fixture);
  for (const tool of [
    "tar",
    "gzip",
    "mkdir",
    "mktemp",
    "rm",
    "rmdir",
    "awk",
    "chmod",
    "cp",
    "mv",
    "ln",
    "readlink",
  ]) {
    const { stdout } = await execute("/bin/sh", [
      "-c",
      'command -v "$1"',
      "sh",
      tool,
    ]);
    if (tool === "tar") {
      await executable(
        join(tools, tool),
        `
const { appendFileSync } = require("node:fs");
const { spawnSync } = require("node:child_process");
if (process.argv[2] === "-xzf") appendFileSync(${JSON.stringify(extractions)}, "extract\\n");
const result = spawnSync(${JSON.stringify(stdout.trim())}, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);`,
      );
    } else if (tool === "mv")
      await executable(
        join(tools, tool),
        `
const { spawnSync } = require("node:child_process");
const args = process.argv.slice(2);
if (process.env.FAIL_ACTIVATION && args.at(-1).endsWith("/current")) process.exit(1);
if (${JSON.stringify(options.aliasFailure ?? "")} && args.at(-2).endsWith("/alias-" + ${JSON.stringify(options.aliasFailure ?? "")})) process.exit(1);
// Exercise Darwin's no-dereference rename semantics on a Linux test host.
const translated = args.map((arg) => arg === "-fh" && process.platform !== "darwin" ? "-fT" : arg === "-fT" && process.platform === "darwin" ? "-fh" : arg);
const result = spawnSync(${JSON.stringify(stdout.trim())}, translated, { stdio: "inherit" });
process.exit(result.status ?? 1);`,
      );
    else await symlink(stdout.trim(), join(tools, tool));
  }
  if (options.renameFailure) fixture.env.FAIL_ACTIVATION = "1";
  const hashTool = options.shasum ? "shasum" : "sha256sum";
  await executable(
    join(tools, hashTool),
    `
const { createHash } = require("node:crypto");
const { readFileSync } = require("node:fs");
process.stdout.write(createHash("sha256").update(readFileSync(0)).digest("hex") + "  -\\n");`,
  );
  await executable(
    join(tools, "uname"),
    `process.stdout.write((process.argv[2] === "-s" ? ${JSON.stringify(options.os ?? "Linux")} : ${JSON.stringify(options.machine ?? "x86_64")}) + "\\n");`,
  );
  await executable(
    join(tools, "curl"),
    `
const { appendFileSync, copyFileSync, writeFileSync } = require("node:fs");
const args = process.argv.slice(2);
const url = args.at(-1);
const output = args[args.indexOf("--output") + 1];
appendFileSync(${JSON.stringify(requests)}, url + "\\n");
if (${JSON.stringify(options.downloadFailure ?? "")} && url.includes(${JSON.stringify(options.downloadFailure ?? "")})) { writeFileSync(output, "partial"); process.exit(22); }
copyFileSync(url.endsWith("desktop-checksums.txt") ? ${JSON.stringify(fixture.manifest)} : ${JSON.stringify(fixture.archive)}, output);`,
  );
  return fixture;
}

async function archive(fixture: Fixture): Promise<void> {
  await execute("tar", [
    "-czf",
    fixture.archive,
    "-C",
    fixture.source,
    fixture.app.split("/")[0] ?? "",
  ]);
  await manifest(fixture);
}

async function manifest(fixture: Fixture): Promise<void> {
  const hash = createHash("sha256")
    .update(await readFile(fixture.archive))
    .digest("hex");
  await writeFile(
    fixture.manifest,
    ["edge", "1.2.3"]
      .map(
        (version) =>
          `${hash}  diffx-desktop_${version}_${fixture.target}.tar.gz\n`,
      )
      .join(""),
  );
}

async function executable(path: string, source: string): Promise<void> {
  await writeFile(path, `#!${process.execPath}\n${source}\n`);
  await chmod(path, 0o755);
}

function install(fixture: Fixture, args: string[] = []) {
  return execute("/bin/sh", [installer, ...args], {
    cwd: fixture.root,
    env: fixture.env,
  });
}

async function assertClean(
  fixture: Fixture,
  expectedPairs: number,
): Promise<void> {
  const versions = join(fixture.bin, ".diffx-desktop-versions");
  const entries = await readdir(versions);
  assert.equal(entries.includes(".install-lock"), false);
  const pairs = entries.filter((name) => name !== "current");
  assert.equal(pairs.length, expectedPairs);
  for (const pair of pairs) {
    assert.deepEqual((await readdir(join(versions, pair))).sort(), [
      "bin",
      "payload",
      "previous",
    ]);
  }
}

async function assertAliases(fixture: Fixture): Promise<void> {
  const current = join(fixture.bin, ".diffx-desktop-versions/current");
  for (const name of ["diffx", "diffx-desktop"]) {
    assert.equal(
      await readlink(join(fixture.bin, name)),
      `.diffx-desktop-versions/current/bin/${name}`,
    );
    const executable = await realpath(join(fixture.bin, name));
    assert.ok(executable.startsWith(`${await realpath(current)}/payload/`));
  }
}
