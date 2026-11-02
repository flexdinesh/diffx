import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";

const execFileAsync = promisify(execFile);
const taskPath = join(
  process.cwd(),
  ".mise",
  "tasks",
  "release",
  "homebrew",
  "pr",
);
const branch = "diffx-homebrew";

interface TapWorkspace {
  root: string;
  tapDir: string;
  binDir: string;
  statePath: string;
  stubScriptPath: string;
}

interface StubPullRequest {
  number: number;
  head: string;
  title: string;
  body: string;
  open: boolean;
}

interface StubState {
  prs: StubPullRequest[];
}

test("opens a tap pull request for a release", async (t) => {
  const workspace = await createWorkspace(t.after.bind(t));
  await writeFormula(workspace, "0.2.0");

  await runTask(workspace, "0.2.0");

  const { prs } = await readState(workspace.statePath);
  assert.equal(prs.length, 1);
  const [pullRequest] = prs;
  assert.ok(pullRequest);
  assert.equal(pullRequest.head, branch);
  assert.equal(pullRequest.title, "Update diffx to v0.2.0");
  assert.equal(await remoteFormula(workspace), formula("0.2.0").trimEnd());
});

test("updates the open pull request instead of superseding it", async (t) => {
  const workspace = await createWorkspace(t.after.bind(t));
  await writeFormula(workspace, "0.2.0");
  await runTask(workspace, "0.2.0");

  // A later release checks out main again and fetches only main, so the rolling
  // branch has no remote-tracking ref to lease against.
  await git(workspace.tapDir, ["checkout", "--quiet", "main"]);
  await git(workspace.tapDir, ["reset", "--quiet", "--hard", "origin/main"]);
  await git(workspace.tapDir, [
    "update-ref",
    "-d",
    `refs/remotes/origin/${branch}`,
  ]);
  await writeFormula(workspace, "0.2.1");

  await runTask(workspace, "0.2.1");

  const { prs } = await readState(workspace.statePath);
  assert.equal(prs.length, 1, "a later release must reuse the open request");
  const [pullRequest] = prs;
  assert.ok(pullRequest);
  assert.equal(pullRequest.title, "Update diffx to v0.2.1");
  assert.match(pullRequest.body, /v0\.2\.1/);
  assert.equal(await remoteFormula(workspace), formula("0.2.1").trimEnd());
});

test("leaves the tap alone when the formula is already current", async (t) => {
  const workspace = await createWorkspace(t.after.bind(t));

  await runTask(workspace, "0.1.0");

  const { prs } = await readState(workspace.statePath);
  assert.equal(prs.length, 0);
  assert.equal(
    await git(workspace.tapDir, [
      "ls-remote",
      "--heads",
      "origin",
      `refs/heads/${branch}`,
    ]),
    "",
  );
});

async function createWorkspace(
  registerCleanup: (cleanup: () => Promise<void>) => void,
): Promise<TapWorkspace> {
  const root = await mkdtemp(join(tmpdir(), "diffx-homebrew-pr-"));
  registerCleanup(() => rm(root, { recursive: true, force: true }));

  const seedDir = join(root, "seed");
  const tapDir = join(root, "homebrew-tap");
  const binDir = join(root, "bin");
  const statePath = join(root, "gh-state.json");
  const stubScriptPath = join(binDir, "stub.js");

  await mkdir(join(seedDir, "Formula"), { recursive: true });
  await mkdir(binDir, { recursive: true });
  await writeFile(join(seedDir, "Formula", "diffx.rb"), formula("0.1.0"));
  await git(seedDir, ["init", "--quiet", "--initial-branch=main"]);
  await git(seedDir, ["add", "-A"]);
  await git(seedDir, ["commit", "--quiet", "-m", "Add diffx formula"]);
  await git(root, ["clone", "--quiet", "--bare", "seed", "origin.git"]);
  await git(root, ["clone", "--quiet", "origin.git", "homebrew-tap"]);

  await writeFile(join(binDir, "gh"), ghStub);
  await chmod(join(binDir, "gh"), 0o755);
  await writeFile(stubScriptPath, ghStubScript);
  await writeFile(statePath, `${JSON.stringify({ prs: [] }, null, 2)}\n`);

  return { root, tapDir, binDir, statePath, stubScriptPath };
}

async function runTask(
  workspace: TapWorkspace,
  version: string,
): Promise<void> {
  await execFileAsync("bash", [taskPath], {
    cwd: workspace.root,
    env: {
      ...process.env,
      PATH: `${workspace.binDir}:${process.env.PATH ?? ""}`,
      GH_STUB_STATE: workspace.statePath,
      GH_STUB_SCRIPT: workspace.stubScriptPath,
      RELEASE_VERSION: version,
      RELEASE_TAG: `v${version}`,
      GIT_CONFIG_GLOBAL: "/dev/null",
      GIT_CONFIG_SYSTEM: "/dev/null",
      GIT_TERMINAL_PROMPT: "0",
    },
  });
}

async function writeFormula(
  workspace: TapWorkspace,
  version: string,
): Promise<void> {
  await writeFile(
    join(workspace.tapDir, "Formula", "diffx.rb"),
    formula(version),
  );
}

async function remoteFormula(workspace: TapWorkspace): Promise<string> {
  await git(workspace.tapDir, ["fetch", "--quiet", "origin"]);
  return git(workspace.tapDir, ["show", `origin/${branch}:Formula/diffx.rb`]);
}

async function git(cwd: string, args: string[]): Promise<string> {
  const { stdout } = await execFileAsync(
    "git",
    [
      "-c",
      "commit.gpgsign=false",
      "-c",
      "user.name=diffx release test",
      "-c",
      "user.email=release@diffx.test",
      ...args,
    ],
    {
      cwd,
      env: {
        ...process.env,
        GIT_CONFIG_GLOBAL: "/dev/null",
        GIT_CONFIG_SYSTEM: "/dev/null",
        GIT_TERMINAL_PROMPT: "0",
      },
    },
  );
  return stdout.trimEnd();
}

async function readState(path: string): Promise<StubState> {
  return parseState(await readFile(path, "utf8"));
}

function parseState(text: string): StubState {
  const parsed: unknown = JSON.parse(text);
  if (!isRecord(parsed) || !Array.isArray(parsed.prs)) {
    throw new Error(`invalid stub gh state: ${text}`);
  }
  return { prs: parsed.prs.map(parsePullRequest) };
}

function parsePullRequest(value: unknown): StubPullRequest {
  if (!isRecord(value)) {
    throw new Error(`invalid stub pull request: ${JSON.stringify(value)}`);
  }
  const { number, head, title, body, open } = value;
  if (
    typeof number !== "number" ||
    typeof head !== "string" ||
    typeof title !== "string" ||
    typeof body !== "string" ||
    typeof open !== "boolean"
  ) {
    throw new Error(`invalid stub pull request: ${JSON.stringify(value)}`);
  }
  return { number, head, title, body, open };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function formula(version: string): string {
  const sha256 = "a".repeat(64);
  return `class Diffx < Formula
  desc "Review local Git changes in your browser"
  homepage "https://github.com/flexdinesh/diffx"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/flexdinesh/diffx/releases/download/v${version}/diffx_${version}_darwin_amd64.tar.gz"
      sha256 "${sha256}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/flexdinesh/diffx/releases/download/v${version}/diffx_${version}_linux_arm64.tar.gz"
      sha256 "${sha256}"
    end
  end

  def install
    bin.install "diffx"
  end

  test do
    assert_match "diffx #{version}", shell_output("#{bin}/diffx --version")
  end
end
`;
}

const ghStub = `#!/usr/bin/env bash
exec node "\${GH_STUB_SCRIPT:?}" "$@"
`;

// Records the pull requests the task asks for. Supports only the gh surface
// .mise/tasks/release/homebrew/pr uses.
const ghStubScript = `const { readFileSync, writeFileSync } = require("node:fs");

const statePath = process.env.GH_STUB_STATE;
const state = JSON.parse(readFileSync(statePath, "utf8"));
const args = process.argv.slice(2);

function flag(name) {
  const index = args.indexOf(name);
  return index === -1 ? "" : (args[index + 1] ?? "");
}

function body() {
  return readFileSync(flag("--body-file"), "utf8");
}

function save() {
  writeFileSync(statePath, JSON.stringify(state, null, 2));
}

if (args[0] !== "pr") {
  console.error("stub gh does not implement: " + args.join(" "));
  process.exit(1);
}

if (args[1] === "list") {
  // Mirrors --json number --jq '.[0].number // ""'.
  const pullRequest = state.prs.find(
    (entry) => entry.open && entry.head === flag("--head"),
  );
  if (pullRequest) {
    process.stdout.write(String(pullRequest.number));
  }
} else if (args[1] === "create") {
  const number = state.prs.length + 1;
  state.prs.push({
    number,
    head: flag("--head"),
    title: flag("--title"),
    body: body(),
    open: true,
  });
  save();
  process.stdout.write("https://github.com/flexdinesh/homebrew-tap/pull/" + number + "\\n");
} else if (args[1] === "edit") {
  const pullRequest = state.prs.find((entry) => entry.number === Number(args[2]));
  pullRequest.title = flag("--title");
  pullRequest.body = body();
  save();
} else {
  console.error("stub gh does not implement: " + args.join(" "));
  process.exit(1);
}
`;
