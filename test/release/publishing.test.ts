import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  chmod,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import type { TestContext } from "node:test";
import { promisify } from "node:util";

const execute = promisify(execFile);
const tasks = join(process.cwd(), ".mise/tasks/release");
const selector = join(process.cwd(), "tools/release-version/main.go");
const targets = ["darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"];

interface Release {
  tag: string;
  draft: boolean;
  prerelease: boolean;
  latest: boolean;
  notes: string;
  assets: string[];
}

interface State {
  releases: Release[];
  builds: { version: string; source: string }[];
}

interface Workspace {
  root: string;
  source: string;
  head: string;
  state: string;
  env: NodeJS.ProcessEnv;
}

for (const legacy of [false, true]) {
  test(`stable rerun recognizes ${legacy ? "historical asset" : "direct annotated"} tags`, async (t) => {
    const workspace = await createWorkspace(t);
    await tagRelease(workspace, legacy);
    const output = await selectVersion(workspace);
    assert.equal(output.get("tag"), "v0.1.0");
    assert.equal(output.get("tag_exists"), "true");
    assert.equal(output.get("release_exists"), "true");
    assert.equal(output.get("source_sha"), workspace.head);
    await runTask(workspace, "publish");
    assert.deepEqual((await readState(workspace)).builds, []);
  });

  test(`stable publishes ${legacy ? "historical asset" : "direct"} tag from main source`, async (t) => {
    const workspace = await createWorkspace(t);
    await tagRelease(workspace, legacy);
    await writeFile(
      workspace.state,
      JSON.stringify({ releases: [], builds: [] }),
    );
    const output = await selectVersion(workspace);
    assert.equal(output.get("release_exists"), "false");
    await runTask(workspace, "publish");
    const state = await readState(workspace);
    assert.deepEqual(state.builds, [
      { version: "0.1.0", source: workspace.head },
    ]);
    const [release] = state.releases;
    assert.ok(release);
    assert.equal(release.tag, "v0.1.0");
    assert.equal(release.prerelease, false);
    assert.equal(release.latest, true);
    assert.equal(release.notes, `Source commit: ${workspace.head}`);
    assert.deepEqual(release.assets.sort(), expectedAssets("0.1.0").sort());
    assert.equal(await git(workspace, ["rev-parse", "HEAD"]), workspace.head);
  });
}

test("stable tags main directly without committing workspace assets", async (t) => {
  const workspace = await createWorkspace(t);
  await runTask(workspace, "tag");
  assert.equal(await remoteRef(workspace, "refs/tags/v0.1.0"), workspace.head);
  assert.equal(
    await git(workspace, [
      "ls-tree",
      "--name-only",
      "v0.1.0",
      "internal/webui/dist",
    ]),
    "",
  );
});

test("stable refuses a tag belonging to another source commit", async (t) => {
  const workspace = await createWorkspace(t);
  await tagRelease(workspace, false);
  await advanceMain(workspace);
  await assert.rejects(
    runTask(workspace, "publish"),
    /does not match the selected source release/,
  );
  assert.deepEqual((await readState(workspace)).builds, []);
});

test("stable resumes a draft release with complete assets", async (t) => {
  const workspace = await createWorkspace(t);
  await tagRelease(workspace, false);
  const state = await readState(workspace);
  const [draft] = state.releases;
  assert.ok(draft);
  draft.draft = true;
  draft.assets = ["checksums.txt"];
  await writeFile(workspace.state, JSON.stringify(state));
  assert.equal((await selectVersion(workspace)).get("release_exists"), "false");
  await runTask(workspace, "publish");
  const [published] = (await readState(workspace)).releases;
  assert.ok(published);
  assert.equal(published.draft, false);
  assert.deepEqual(published.assets.sort(), expectedAssets("0.1.0").sort());
});

test("release discovery errors fail selection rather than inventing an unpublished release", async (t) => {
  const workspace = await createWorkspace(t);
  await tagRelease(workspace, false);
  await assert.rejects(
    runTask(workspace, "select-version", { STUB_API_FAIL: "1" }),
    /API unavailable/,
  );
});

test("packaging refuses changed source and mismatched provenance", async (t) => {
  const workspace = await createWorkspace(t);
  await assert.rejects(
    runTask(workspace, "package", {
      DIFFX_SOURCE_SHA: await childCommit(workspace),
    }),
    /checkout does not match/,
  );
  await writeFile(join(workspace.source, "README.md"), "uncommitted source\n");
  await assert.rejects(runTask(workspace, "package"), /source must be clean/);
  assert.deepEqual((await readState(workspace)).builds, []);
});

function expectedAssets(version: string): string[] {
  return [
    ...targets.map((target) => `diffx_${version}_${target}.tar.gz`),
    "checksums.txt",
  ];
}

async function createWorkspace(t: TestContext): Promise<Workspace> {
  const root = await mkdtemp(join(tmpdir(), "diffx-publishing-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const source = join(root, "source");
  const bin = join(root, "bin");
  const state = join(root, "state.json");
  await mkdir(join(source, "tools/release-version"), { recursive: true });
  await mkdir(bin);
  await copyFile(selector, join(source, "tools/release-version/main.go"));
  await writeFile(
    join(source, "go.mod"),
    "module publishing.test\n\ngo 1.26\n",
  );
  await writeFile(join(source, ".release-version"), "0.1\n");
  await writeFile(join(source, ".gitignore"), "dist/\ninternal/webui/dist/\n");
  await writeFile(join(source, "README.md"), "main source\n");
  await writeFile(join(root, "stub.cjs"), stub);
  for (const tool of ["gh", "goreleaser", "mise"]) {
    const path = join(bin, tool);
    await writeFile(
      path,
      `#!/usr/bin/env bash\nexec "$STUB_NODE" "$STUB_SCRIPT" ${tool} "$@"\n`,
    );
    await chmod(path, 0o755);
  }
  await writeFile(
    state,
    JSON.stringify({
      releases: [
        {
          tag: "v0.1.0",
          draft: false,
          prerelease: false,
          latest: true,
          notes: "stable",
          assets: [],
        },
      ],
      builds: [],
    }),
  );
  const env = {
    ...process.env,
    PATH: `${bin}:${process.env.PATH ?? ""}`,
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_TERMINAL_PROMPT: "0",
    GIT_AUTHOR_NAME: "release test",
    GIT_COMMITTER_NAME: "release test",
    GIT_AUTHOR_EMAIL: "release@diffx.test",
    GIT_COMMITTER_EMAIL: "release@diffx.test",
    GITHUB_REPOSITORY: "fixture/diffx",
    GITHUB_OUTPUT: join(root, "output"),
    STUB_NODE: process.execPath,
    STUB_SCRIPT: join(root, "stub.cjs"),
    STUB_STATE: state,
    STUB_PACKAGE_TASK: join(tasks, "package"),
  };
  const workspace: Workspace = { root, source, state, env, head: "" };
  await git(workspace, ["init", "--quiet", "--initial-branch=main"]);
  await git(workspace, ["add", "."]);
  await git(workspace, ["commit", "--quiet", "-m", "source"]);
  workspace.head = await git(workspace, ["rev-parse", "HEAD"]);
  await git(workspace, [
    "clone",
    "--quiet",
    "--bare",
    ".",
    join(root, "origin.git"),
  ]);
  await git(workspace, ["remote", "add", "origin", join(root, "origin.git")]);
  // Workspace-only UI inputs are ignored and remain available to packaging.
  await mkdir(join(source, "internal/webui/dist"), { recursive: true });
  await writeFile(join(source, "internal/webui/dist/index.html"), "built UI\n");
  return workspace;
}

async function git(workspace: Workspace, args: string[]): Promise<string> {
  const { stdout } = await execute(
    "git",
    ["-c", "commit.gpgsign=false", ...args],
    { cwd: workspace.source, env: workspace.env },
  );
  return stdout.trim();
}

async function remoteRef(workspace: Workspace, ref: string): Promise<string> {
  return (
    (await git(workspace, ["ls-remote", "origin", ref])).split("\t")[0] ?? ""
  );
}

async function childCommit(workspace: Workspace): Promise<string> {
  return git(workspace, [
    "commit-tree",
    "HEAD^{tree}",
    "-p",
    "HEAD",
    "-m",
    "next source",
  ]);
}

async function advanceMain(workspace: Workspace): Promise<string> {
  await writeFile(join(workspace.source, "README.md"), "next main source\n");
  await git(workspace, ["add", "README.md"]);
  await git(workspace, ["commit", "--quiet", "-m", "next source"]);
  await git(workspace, ["push", "--quiet", "origin", "HEAD:refs/heads/main"]);
  return git(workspace, ["rev-parse", "HEAD"]);
}

async function tagRelease(
  workspace: Workspace,
  legacy: boolean,
): Promise<void> {
  if (legacy) {
    await git(workspace, ["add", "--force", "internal/webui/dist"]);
    const tree = await git(workspace, ["write-tree"]);
    const commit = await git(workspace, [
      "commit-tree",
      tree,
      "-p",
      "HEAD",
      "-m",
      "release assets",
    ]);
    await git(workspace, ["reset", "--quiet"]);
    await git(workspace, ["tag", "v0.1.0", commit]);
  } else {
    await git(workspace, [
      "-c",
      "tag.gpgsign=false",
      "tag",
      "--annotate",
      "v0.1.0",
      "-m",
      "release",
    ]);
  }
  await git(workspace, ["push", "--quiet", "origin", "refs/tags/v0.1.0"]);
}

async function runTask(
  workspace: Workspace,
  task: string,
  env: NodeJS.ProcessEnv = {},
) {
  return execute("bash", [join(tasks, task)], {
    cwd: workspace.source,
    env: {
      ...workspace.env,
      GITHUB_SHA: workspace.head,
      RELEASE_TAG: "v0.1.0",
      DIFFX_SOURCE_SHA: workspace.head,
      DIFFX_RELEASE_VERSION: "0.1.0",
      ...env,
    },
  });
}

async function selectVersion(
  workspace: Workspace,
): Promise<Map<string, string>> {
  await runTask(workspace, "select-version");
  const text = await readFile(join(workspace.root, "output"), "utf8");
  return new Map(
    text
      .trim()
      .split("\n")
      .map((line) => {
        const separator = line.indexOf("=");
        return [line.slice(0, separator), line.slice(separator + 1)];
      }),
  );
}

async function readState(workspace: Workspace): Promise<State> {
  const value: unknown = JSON.parse(await readFile(workspace.state, "utf8"));
  if (
    !isRecord(value) ||
    !Array.isArray(value.releases) ||
    !Array.isArray(value.builds)
  ) {
    throw new Error("invalid publishing stub state");
  }
  return {
    releases: value.releases.map((release: unknown): Release => {
      if (
        !isRecord(release) ||
        typeof release.tag !== "string" ||
        typeof release.notes !== "string" ||
        typeof release.draft !== "boolean" ||
        typeof release.prerelease !== "boolean" ||
        typeof release.latest !== "boolean" ||
        !Array.isArray(release.assets) ||
        !release.assets.every((asset: unknown) => typeof asset === "string")
      ) {
        throw new Error("invalid publishing stub release");
      }
      return {
        tag: release.tag,
        notes: release.notes,
        draft: release.draft,
        prerelease: release.prerelease,
        latest: release.latest,
        assets: release.assets,
      };
    }),
    builds: value.builds.map((build: unknown) => {
      if (
        !isRecord(build) ||
        typeof build.source !== "string" ||
        typeof build.version !== "string"
      ) {
        throw new Error("invalid publishing stub build");
      }
      return { version: build.version, source: build.source };
    }),
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// GoReleaser and GitHub are replaced at their process boundaries. Git operations
// use real temporary repositories and never contact an external remote.
const stub = `
const { readFileSync, writeFileSync, mkdirSync } = require("node:fs");
const { basename } = require("node:path");
const { execFileSync } = require("node:child_process");
const [tool, ...args] = process.argv.slice(2);
const state = JSON.parse(readFileSync(process.env.STUB_STATE, "utf8"));
function save() { writeFileSync(process.env.STUB_STATE, JSON.stringify(state)); }
function flag(name) { const index = args.indexOf(name); return index < 0 ? "" : args[index + 1]; }
function fail(message) { console.error(message); process.exit(1); }
if (tool === "mise") {
  if (args.join(" ") !== "run release:package") fail("unsupported mise command");
  execFileSync("bash", [process.env.STUB_PACKAGE_TASK], { stdio: "inherit" });
} else if (tool === "goreleaser") {
  if (args.join(" ") !== "release --snapshot --clean") fail("packaging must not publish");
  if (process.env.STUB_BUILD_FAIL) fail("compilation failed");
  const version = process.env.DIFFX_RELEASE_VERSION;
  const source = process.env.DIFFX_SOURCE_SHA;
  state.builds.push({ version, source });
  mkdirSync("dist", { recursive: true });
  const targets = ["darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"];
  for (const target of targets) writeFileSync("dist/diffx_" + version + "_" + target + ".tar.gz", source);
  writeFileSync("dist/checksums.txt", "checksums for " + source);
  save();
} else if (tool === "gh" && args[0] === "api") {
  if (process.env.STUB_API_FAIL) fail("API unavailable");
  const publishedOnly = flag("--jq").includes(".draft == false");
  console.log(state.releases.filter((release) => !publishedOnly || !release.draft).map((release) => release.tag).join("\\n"));
} else if (tool === "gh" && args[0] === "release") {
  const operation = args[1];
  const tag = args[2];
  let release = state.releases.find((entry) => entry.tag === tag);
  if (operation === "create") {
    if (release) fail("release already exists");
    release = { tag, draft: false, prerelease: false, latest: false, notes: "", assets: [] };
    state.releases.push(release);
  }
  if (!release) fail("release missing");
  if (operation === "create" || operation === "upload") {
    for (const path of args.slice(3)) {
      if (path.startsWith("--")) break;
      readFileSync(path);
      if (!release.assets.includes(basename(path))) release.assets.push(basename(path));
    }
  }
  if (operation === "create" || operation === "edit") {
    release.notes = flag("--notes");
    release.prerelease = args.includes("--prerelease");
    release.draft = false;
    if (args.includes("--latest")) {
      for (const entry of state.releases) entry.latest = false;
      release.latest = true;
    } else if (args.includes("--latest=false")) release.latest = false;
  }
  save();
} else fail("unsupported stub command: " + tool + " " + args.join(" "));
`;
