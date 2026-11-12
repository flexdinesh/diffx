import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  chmod,
  link,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { type TestContext } from "node:test";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const installer = join(process.cwd(), "install-dev.sh");
const repo = "https://github.com/flexdinesh/diffx.git";
const binary = "#!/bin/sh\nprintf 'source diffx\\n'\n";
const mainSHA = "a".repeat(40);
const branchSHA = "b".repeat(40);
const commitSHA = "c0ffee".padEnd(40, "1");
const legacySHA = "d".repeat(40);

interface Options {
  missingTool?: "git" | "mise";
  fetchFailure?: boolean;
  buildFailure?: boolean;
}

interface Workspace {
  root: string;
  home: string;
  binDir: string;
  cache: string;
  log: string;
  env: NodeJS.ProcessEnv;
}

interface Call {
  tool: string;
  args: string[];
  cwd: string;
  trusted: string | undefined;
}

test("help needs no git or mise", async (t) => {
  const workspace = await createWorkspace(t);
  const { stdout } = await run(workspace, ["--help"]);
  assert.match(stdout, /Usage: install-dev.sh \[--ref BRANCH\|SHA\]/);
  assert.match(stdout, /default: main/);
  assert.match(stdout, /--clean/);
  assert.deepEqual(await calls(workspace), []);
});

test("default builds main in the cached checkout and replaces atomically", async (t) => {
  const workspace = await createWorkspace(t);
  await mkdir(workspace.binDir, { recursive: true });
  const installed = join(workspace.binDir, "diffx");
  const oldReader = join(workspace.root, "old-reader");
  await writeFile(installed, "previous binary");
  await link(installed, oldReader);
  const { stdout } = await run(workspace);
  assert.equal(await readFile(installed, "utf8"), binary);
  // A process holding the previous inode must keep seeing the old contents.
  assert.equal(await readFile(oldReader, "utf8"), "previous binary");
  assert.equal((await stat(installed)).mode & 0o777, 0o755);
  assert.match(stdout, /Installed diffx dev aaaaaaa \(main\) to /);
  assert.match(stdout, /Add .* to your PATH/);
  const src = join(workspace.cache, "src");
  const log = await calls(workspace);
  const git = log.filter((call) => call.tool === "git");
  assert.deepEqual(git.find((call) => call.args[0] === "clone")?.args, [
    "clone",
    "--quiet",
    "--filter=blob:none",
    "--no-checkout",
    repo,
    src,
  ]);
  assert.ok(
    git.some(
      (call) =>
        call.args.join(" ") ===
        `-C ${src} checkout --quiet --detach --force ${mainSHA}`,
    ),
  );
  const mise = log.filter((call) => call.tool === "mise");
  assert.equal(mise.length, 2);
  assert.deepEqual(mise[0]?.args, ["install", "--locked"]);
  const build = mise[1]?.args ?? [];
  assert.deepEqual(build.slice(0, 3), ["run", "install:source", "--"]);
  assert.match(
    build[3] ?? "",
    new RegExp(`^${workspace.binDir}/\\.diffx-install\\.[^/]+/diffx$`),
  );
  for (const call of mise) {
    assert.equal(call.cwd, src);
    assert.equal(call.trusted, src);
  }
  await assertClean(workspace);
});

test("reuses the cached checkout on later installs", async (t) => {
  const workspace = await createWorkspace(t);
  await run(workspace);
  await writeFile(workspace.log, "");
  await run(workspace);
  const git = (await calls(workspace)).filter((call) => call.tool === "git");
  assert.ok(!git.some((call) => call.args[0] === "clone"));
  assert.ok(git.some((call) => call.args.includes("fetch")));
  await assertClean(workspace);
});

for (const { ref, sha } of [
  { ref: "feature/x", sha: branchSHA },
  { ref: "c0ffee1", sha: commitSHA },
  { ref: commitSHA, sha: commitSHA },
]) {
  test(`installs --ref ${ref}`, async (t) => {
    const workspace = await createWorkspace(t);
    const { stdout } = await run(workspace, ["--ref", ref]);
    assert.match(
      stdout,
      new RegExp(`Installed diffx dev ${sha.slice(0, 7)} \\(${ref}\\)`),
    );
    assert.ok(
      (await calls(workspace)).some(
        (call) =>
          call.tool === "git" && call.args.join(" ").endsWith(`--force ${sha}`),
      ),
    );
    await assertClean(workspace);
  });
}

test("unknown ref fails before building and preserves the install", async (t) => {
  const workspace = await createWorkspace(t);
  await mkdir(workspace.binDir, { recursive: true });
  await writeFile(join(workspace.binDir, "diffx"), "previous binary");
  await assert.rejects(
    run(workspace, ["--ref", "missing"]),
    /unknown ref: missing/,
  );
  // A short hex-like branch name is not looked up as a commit.
  await assert.rejects(run(workspace, ["--ref", "abc"]), /unknown ref: abc/);
  assert.equal(
    await readFile(join(workspace.binDir, "diffx"), "utf8"),
    "previous binary",
  );
  assert.ok(!(await calls(workspace)).some((call) => call.tool === "mise"));
  await assertClean(workspace);
});

for (const args of [
  ["--unknown"],
  ["--ref"],
  ["--ref", ""],
  ["--ref", "-x"],
  ["--ref", "a..b"],
  ["--ref", "@{-1}"],
  ["--ref", "bad ref"],
  ["--bin-dir"],
  ["--bin-dir", ""],
]) {
  test(`rejects invalid arguments ${JSON.stringify(args)}`, async (t) => {
    const workspace = await createWorkspace(t);
    await assert.rejects(run(workspace, args), /diffx:/);
    assert.ok(
      (await calls(workspace)).every(
        (call) => call.tool === "git" && call.args[0] === "check-ref-format",
      ),
    );
    await assertClean(workspace);
  });
}

test("requires mise with an install hint", async (t) => {
  const workspace = await createWorkspace(t, { missingTool: "mise" });
  await assert.rejects(
    run(workspace),
    /mise is required to build from source; install it from https:\/\/mise\.jdx\.dev/,
  );
  assert.deepEqual(await calls(workspace), []);
});

test("requires git", async (t) => {
  const workspace = await createWorkspace(t, { missingTool: "git" });
  await assert.rejects(run(workspace), /required tool not found: git/);
});

test("refuses a cache cloned from another repository until --clean", async (t) => {
  const workspace = await createWorkspace(t);
  const src = join(workspace.cache, "src");
  await mkdir(join(src, ".git"), { recursive: true });
  await writeFile(join(src, ".git/origin"), "https://example.com/fork.git");
  await assert.rejects(run(workspace), /is not a clone of .*--clean/);
  assert.ok(
    !(await calls(workspace)).some((call) => call.args.includes("fetch")),
  );
  await run(workspace, ["--clean"]);
  assert.equal(await readFile(join(src, ".git/origin"), "utf8"), repo);
  await assertClean(workspace);
});

test("refuses a non-git cache until --clean", async (t) => {
  const workspace = await createWorkspace(t);
  const src = join(workspace.cache, "src");
  await mkdir(src, { recursive: true });
  await writeFile(join(src, "stray"), "");
  await assert.rejects(
    run(workspace),
    /is not a git clone; rerun with --clean/,
  );
  await run(workspace, ["--clean"]);
  assert.equal(await readFile(join(workspace.binDir, "diffx"), "utf8"), binary);
  await assertClean(workspace);
});

for (const options of [{ buildFailure: true }, { fetchFailure: true }]) {
  test(`${JSON.stringify(options)} preserves the install and cleans up`, async (t) => {
    const workspace = await createWorkspace(t, options);
    await mkdir(workspace.binDir, { recursive: true });
    await writeFile(join(workspace.binDir, "diffx"), "previous binary");
    await assert.rejects(
      run(workspace),
      options.buildFailure
        ? /build failed for main \(aaaaaaa\); existing install preserved/
        : /could not fetch/,
    );
    assert.equal(
      await readFile(join(workspace.binDir, "diffx"), "utf8"),
      "previous binary",
    );
    assert.deepEqual(await readdir(workspace.binDir), ["diffx"]);
    await assertClean(workspace);
  });
}

test("refuses refs that predate the install:source task", async (t) => {
  const workspace = await createWorkspace(t);
  await assert.rejects(
    run(workspace, ["--ref", "legacy"]),
    /legacy \(ddddddd\) predates install-dev.sh: it has no install:source task/,
  );
  assert.ok(!(await calls(workspace)).some((call) => call.tool === "mise"));
  await assertClean(workspace);
});

test("refuses concurrent installs without removing their lock", async (t) => {
  const workspace = await createWorkspace(t);
  const lock = join(workspace.cache, "install-dev.lock");
  await mkdir(lock, { recursive: true });
  await assert.rejects(run(workspace), /another source install is in progress/);
  assert.deepEqual(await readdir(lock), []);
  assert.ok(
    (await calls(workspace)).every(
      (call) => call.args[0] === "check-ref-format",
    ),
  );
});

test("honors XDG_CACHE_HOME and relative --bin-dir on PATH", async (t) => {
  const workspace = await createWorkspace(t);
  workspace.cache = join(workspace.root, "xdg/diffx");
  workspace.env.XDG_CACHE_HOME = join(workspace.root, "xdg");
  workspace.binDir = join(workspace.root, "custom bin");
  workspace.env.PATH = `${workspace.env.PATH ?? ""}:${workspace.binDir}`;
  const { stdout } = await run(workspace, ["--bin-dir", "custom bin"]);
  assert.equal(await readFile(join(workspace.binDir, "diffx"), "utf8"), binary);
  assert.doesNotMatch(stdout, /Add .* to your PATH/);
  assert.ok((await readdir(join(workspace.cache, "src"))).includes(".git"));
  await assertClean(workspace);
});

test("refuses a directory at the binary destination", async (t) => {
  const workspace = await createWorkspace(t);
  await mkdir(join(workspace.binDir, "diffx"), { recursive: true });
  await assert.rejects(run(workspace), /destination is a directory/);
  assert.ok(!(await calls(workspace)).some((call) => call.args[0] === "clone"));
});

async function createWorkspace(
  t: TestContext,
  options: Options = {},
): Promise<Workspace> {
  const root = await mkdtemp(join(tmpdir(), "diffx-install-dev-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const home = join(root, "home space");
  const tools = join(root, "tools");
  await mkdir(home);
  await mkdir(tools);
  const log = join(root, "calls.jsonl");
  await writeFile(log, "");
  for (const tool of ["sh", "mkdir", "mktemp", "rm", "rmdir", "chmod", "mv"]) {
    const { stdout } = await execFileAsync("/bin/sh", [
      "-c",
      'command -v "$1"',
      "sh",
      tool,
    ]);
    await symlink(stdout.trim(), join(tools, tool));
  }
  const { stdout: realGit } = await execFileAsync("/bin/sh", [
    "-c",
    "command -v git",
  ]);
  const fixture = join(root, "fixture.json");
  await writeFile(
    fixture,
    JSON.stringify({
      ...options,
      log,
      binary,
      realGit: realGit.trim(),
      branches: { main: mainSHA, "feature/x": branchSHA, legacy: legacySHA },
      commits: [mainSHA, branchSHA, commitSHA, legacySHA],
      legacy: [legacySHA],
    }),
  );
  if (options.missingTool !== "git")
    await executable(join(tools, "git"), gitStub);
  if (options.missingTool !== "mise")
    await executable(join(tools, "mise"), miseStub);
  return {
    root,
    home,
    binDir: join(home, ".local/bin"),
    cache: join(home, ".cache/diffx"),
    log,
    env: {
      ...process.env,
      HOME: home,
      PATH: tools,
      XDG_CACHE_HOME: "",
      INSTALL_DEV_FIXTURE: fixture,
    },
  };
}

function run(workspace: Workspace, args: string[] = []) {
  return execFileAsync("/bin/sh", [installer, ...args], {
    cwd: workspace.root,
    env: workspace.env,
  });
}

async function executable(path: string, script: string): Promise<void> {
  await writeFile(path, `#!${process.execPath}\n${script}`);
  await chmod(path, 0o755);
}

async function calls(workspace: Workspace): Promise<Call[]> {
  const text = (await readFile(workspace.log, "utf8")).trim();
  if (!text) return [];
  return text.split("\n").map((line): Call => {
    const value: unknown = JSON.parse(line);
    if (
      typeof value !== "object" ||
      value === null ||
      !("tool" in value) ||
      !("args" in value) ||
      !("cwd" in value) ||
      typeof value.tool !== "string" ||
      typeof value.cwd !== "string" ||
      !Array.isArray(value.args) ||
      !value.args.every((arg: unknown) => typeof arg === "string")
    ) {
      throw new Error("invalid call log entry");
    }
    const trusted =
      "trusted" in value && typeof value.trusted === "string"
        ? value.trusted
        : undefined;
    return { tool: value.tool, args: value.args, cwd: value.cwd, trusted };
  });
}

async function assertClean(workspace: Workspace): Promise<void> {
  try {
    assert.ok(
      (await readdir(workspace.binDir)).every(
        (name) => !name.startsWith(".diffx-install."),
      ),
    );
  } catch (error) {
    if (
      !(error instanceof Error) ||
      !("code" in error) ||
      error.code !== "ENOENT"
    )
      throw error;
  }
  try {
    assert.ok(!(await readdir(workspace.cache)).includes("install-dev.lock"));
  } catch (error) {
    if (
      !(error instanceof Error) ||
      !("code" in error) ||
      error.code !== "ENOENT"
    )
      throw error;
  }
}

// Git and mise are replaced at their process boundaries; check-ref-format uses
// real git so ref validation matches production.
const fixtureHelpers = `
const { appendFileSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const fixture = JSON.parse(readFileSync(process.env.INSTALL_DEV_FIXTURE, "utf8"));
function record(tool, args, extra = {}) {
  appendFileSync(fixture.log, JSON.stringify({ tool, args, cwd: process.cwd(), ...extra }) + "\\n");
}
`;

const gitStub = `${fixtureHelpers}
const { spawnSync } = require("node:child_process");
const args = process.argv.slice(2);
record("git", args);
if (args[0] === "check-ref-format") {
  const result = spawnSync(fixture.realGit, args, { stdio: "inherit" });
  process.exit(result.status ?? 1);
}
if (args[0] === "clone") {
  const [url, dir] = args.slice(-2);
  mkdirSync(join(dir, ".git"), { recursive: true });
  writeFileSync(join(dir, ".git/origin"), url);
  process.exit(0);
}
if (args[0] !== "-C") process.exit(2);
const dir = args[1];
const [command, ...rest] = args.slice(2);
if (command === "remote") {
  if (!existsSync(join(dir, ".git/origin"))) process.exit(2);
  process.stdout.write(readFileSync(join(dir, ".git/origin"), "utf8") + "\\n");
} else if (command === "fetch") {
  process.exit(fixture.fetchFailure ? 1 : 0);
} else if (command === "rev-parse") {
  const target = rest.at(-1);
  if (rest[0] === "--short=7") {
    process.stdout.write(target.slice(0, 7) + "\\n");
    process.exit(0);
  }
  const name = target.replace(/\\^\\{commit\\}$/, "");
  const branch = name.startsWith("refs/remotes/origin/") ? fixture.branches[name.slice(20)] : undefined;
  const commit = name.startsWith("refs/") ? undefined : fixture.commits.find((sha) => sha.startsWith(name.toLowerCase()));
  const resolved = branch ?? commit;
  if (!resolved) process.exit(1);
  process.stdout.write(resolved + "\\n");
} else if (command === "checkout") {
  const sha = rest.at(-1);
  const task = join(dir, ".mise/tasks/install/source");
  if (fixture.legacy.includes(sha)) rmSync(task, { force: true });
  else { mkdirSync(join(dir, ".mise/tasks/install"), { recursive: true }); writeFileSync(task, ""); }
} else if (command !== "reset" && command !== "clean") process.exit(2);
`;

const miseStub = `${fixtureHelpers}
const args = process.argv.slice(2);
record("mise", args, { trusted: process.env.MISE_TRUSTED_CONFIG_PATHS });
if (args.join(" ") === "install --locked") process.exit(0);
if (args[0] === "run" && args[1] === "install:source" && args[2] === "--" && args.length === 4) {
  if (fixture.buildFailure) process.exit(1);
  writeFileSync(args[3], fixture.binary);
  process.exit(0);
}
process.exit(2);
`;
