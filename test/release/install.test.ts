import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
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
const repository = process.cwd();
const releases = "https://github.com/flexdinesh/diffx/releases";
const helperURL =
  "https://raw.githubusercontent.com/flexdinesh/diffx/main/scripts/install-common.sh";
const binary = "#!/bin/sh\nprintf 'fixture diffx\\n'\n";

interface Options {
  os?: string;
  machine?: string;
  hashTool?: "sha256sum" | "shasum" | "none";
  missingTool?: string;
  helperFailure?: "partial" | "empty";
  downloadFailure?: string;
  latestURL?: string;
  missingBinary?: boolean;
  symlinkBinary?: boolean;
  renameFailure?: boolean;
}

interface Workspace {
  root: string;
  home: string;
  tools: string;
  temporary: string;
  binDir: string;
  manifest: string;
  archive: string;
  log: string;
  extractions: string;
  env: NodeJS.ProcessEnv;
}

for (const channel of ["stable", "edge"]) {
  test(`${channel}: help describes channel without release downloads`, async (t) => {
    const workspace = await createWorkspace(t);
    const { stdout } = await runInstaller(workspace, channel, ["--help"]);
    assert.match(stdout, /Usage: install/);
    assert.match(stdout, /--bin-dir DIR/);
    assert.match(stdout, /Installs only diffx/);
    assert.match(
      stdout,
      channel === "stable" ? /latest stable/ : /rolling edge/,
    );
    assert.deepEqual(await requests(workspace), [helperURL]);
    await assertClean(workspace);
  });

  test(`${channel}: installs only diffx and replaces atomically`, async (t) => {
    const workspace = await createWorkspace(t);
    await mkdir(workspace.binDir, { recursive: true });
    const installed = join(workspace.binDir, "diffx");
    const oldReader = join(workspace.root, "old-reader");
    await writeFile(installed, "previous binary");
    await link(installed, oldReader);
    const { stdout } = await runInstaller(workspace, channel);
    assert.equal(await readFile(installed, "utf8"), binary);
    // A process holding the previous inode must keep seeing the old contents.
    assert.equal(await readFile(oldReader, "utf8"), "previous binary");
    assert.equal((await stat(installed)).mode & 0o777, 0o755);
    assert.match(stdout, /Add .* to your PATH/);
    assert.deepEqual(await readdir(workspace.binDir), ["diffx"]);
    const urls = await requests(workspace);
    const tag = channel === "stable" ? "v1.2.3" : "edge";
    const version = channel === "stable" ? "1.2.3" : "edge";
    assert.ok(urls.includes(`${releases}/download/${tag}/checksums.txt`));
    assert.ok(
      urls.includes(
        `${releases}/download/${tag}/diffx_${version}_linux_amd64.tar.gz`,
      ),
    );
    if (channel === "edge") {
      assert.ok(urls.every((url) => !url.includes("/latest/")));
    } else {
      assert.equal(urls.filter((url) => url.includes("/latest/")).length, 1);
    }
    await assertClean(workspace);
  });

  for (const failure of ["partial", "empty"]) {
    test(`${channel}: refuses ${failure} helper downloads`, async (t) => {
      assert.ok(failure === "partial" || failure === "empty");
      const workspace = await createWorkspace(t, { helperFailure: failure });
      await assert.rejects(
        runInstaller(workspace, channel),
        /installer helper/,
      );
      assert.ok(!(await readdir(workspace.home)).includes("helper-ran"));
      assert.deepEqual(await requests(workspace), [helperURL]);
      await assertClean(workspace);
    });
  }
}

for (const version of ["v1.2.3", "1.2.3"]) {
  test(`stable: pins explicit version ${version}`, async (t) => {
    const workspace = await createWorkspace(t);
    await runInstaller(workspace, "stable", ["--version", version]);
    assert.deepEqual(await requests(workspace), [
      helperURL,
      `${releases}/download/v1.2.3/checksums.txt`,
      `${releases}/download/v1.2.3/diffx_1.2.3_linux_amd64.tar.gz`,
    ]);
    await assertClean(workspace);
  });
}

for (const { os, machine, target } of [
  { os: "Linux", machine: "x86_64", target: "linux_amd64" },
  { os: "Linux", machine: "aarch64", target: "linux_arm64" },
  { os: "Darwin", machine: "x86_64", target: "darwin_amd64" },
  { os: "Darwin", machine: "arm64", target: "darwin_arm64" },
]) {
  test(`selects archive for ${os} ${machine}`, async (t) => {
    const workspace = await createWorkspace(t, { os, machine });
    await runInstaller(workspace, "stable");
    assert.ok(
      (await requests(workspace)).includes(
        `${releases}/download/v1.2.3/diffx_1.2.3_${target}.tar.gz`,
      ),
    );
    assert.equal(
      await readFile(join(workspace.binDir, "diffx"), "utf8"),
      binary,
    );
    await assertClean(workspace);
  });
}

test("supports relative --bin-dir with spaces and shasum fallback", async (t) => {
  const workspace = await createWorkspace(t, { hashTool: "shasum" });
  const customDirectory = join(workspace.root, "custom bin");
  workspace.binDir = customDirectory;
  workspace.env.PATH = `${workspace.tools}:${customDirectory}`;
  const { stdout } = await runInstaller(workspace, "stable", [
    "--bin-dir",
    "custom bin",
  ]);
  assert.equal(await readFile(join(customDirectory, "diffx"), "utf8"), binary);
  assert.doesNotMatch(stdout, /Add .* to your PATH/);
  await assertClean(workspace);
});

test("supports explicit destination without HOME", async (t) => {
  const workspace = await createWorkspace(t);
  delete workspace.env.HOME;
  await runInstaller(workspace, "stable", ["--bin-dir", workspace.binDir]);
  assert.equal(await readFile(join(workspace.binDir, "diffx"), "utf8"), binary);
  await assertClean(workspace);
});

for (const args of [
  ["--unknown"],
  ["--bin-dir"],
  ["--bin-dir", ""],
  ["--version"],
  ["--version", ""],
  ["--version", "edge"],
  ["--version", "v1.2.3-rc.1"],
  ["--version", "1.2.3/../../other"],
  ["--version", "1.2.3\nother"],
]) {
  test(`rejects invalid arguments ${JSON.stringify(args)}`, async (t) => {
    const workspace = await createWorkspace(t);
    await assert.rejects(runInstaller(workspace, "stable", args), /diffx:/);
    assert.deepEqual(await requests(workspace), [helperURL]);
    await assertClean(workspace);
  });
}

test("edge rejects --version", async (t) => {
  const workspace = await createWorkspace(t);
  await assert.rejects(
    runInstaller(workspace, "edge", ["--version", "1.2.3"]),
    /--version is supported only by install.sh/,
  );
  assert.deepEqual(await requests(workspace), [helperURL]);
});

for (const missingTool of ["curl", "tar", "mktemp", "awk", "mv"]) {
  test(`reports missing ${missingTool}`, async (t) => {
    const workspace = await createWorkspace(t, { missingTool });
    await assert.rejects(
      runInstaller(workspace, "stable"),
      new RegExp(`required tool not found: ${missingTool}`),
    );
    await assertClean(workspace);
  });
}

test("requires a SHA-256 utility", async (t) => {
  const workspace = await createWorkspace(t, { hashTool: "none" });
  await assert.rejects(
    runInstaller(workspace, "stable"),
    /sha256sum or shasum/,
  );
  assert.deepEqual(await requests(workspace), [helperURL]);
  await assertClean(workspace);
});

for (const options of [{ os: "FreeBSD" }, { machine: "riscv64" }]) {
  test(`rejects unsupported platform ${JSON.stringify(options)}`, async (t) => {
    const workspace = await createWorkspace(t, options);
    await assert.rejects(runInstaller(workspace, "stable"), /unsupported/);
    assert.deepEqual(await requests(workspace), [helperURL]);
    await assertClean(workspace);
  });
}

for (const latestURL of [
  "",
  `${releases}/download/edge/checksums.txt`,
  `${releases}/download/v1.2.3-rc.1/checksums.txt`,
  "https://untrusted.example/v1.2.3/checksums.txt",
]) {
  test(`rejects invalid latest redirect ${JSON.stringify(latestURL)}`, async (t) => {
    const workspace = await createWorkspace(t, { latestURL });
    await assert.rejects(
      runInstaller(workspace, "stable"),
      /latest stable release/,
    );
    assert.equal((await requests(workspace)).length, 2);
    await assertClean(workspace);
  });
}

for (const channel of ["stable", "edge"]) {
  for (const failure of [
    "download-manifest",
    "download-archive",
    "checksum",
    "missing-manifest-entry",
    "duplicate-manifest-entry",
    "malformed-manifest-entry",
    "missing-binary",
    "symlink-binary",
    "rename",
  ]) {
    test(`${channel}: ${failure} preserves existing install and cleans up`, async (t) => {
      const options: Options = {
        missingBinary: failure === "missing-binary",
        symlinkBinary: failure === "symlink-binary",
        renameFailure: failure === "rename",
      };
      if (failure === "download-manifest")
        options.downloadFailure = "checksums.txt";
      if (failure === "download-archive") options.downloadFailure = ".tar.gz";
      const workspace = await createWorkspace(t, options);
      const manifest = await readFile(workspace.manifest, "utf8");
      if (failure === "checksum") {
        await writeFile(workspace.archive, "corrupted archive");
      } else if (failure === "missing-manifest-entry") {
        await writeFile(
          workspace.manifest,
          `${"a".repeat(64)}  unrelated.tar.gz\n`,
        );
      } else if (failure === "duplicate-manifest-entry") {
        await writeFile(workspace.manifest, manifest + manifest);
      } else if (failure === "malformed-manifest-entry") {
        await writeFile(
          workspace.manifest,
          manifest.replace(/[a-f0-9]{64}/g, "bad"),
        );
      }
      await mkdir(workspace.binDir, { recursive: true });
      const installed = join(workspace.binDir, "diffx");
      await writeFile(installed, "previous binary");
      const expected =
        failure === "checksum"
          ? /SHA-256 mismatch/
          : failure.includes("manifest-entry")
            ? /missing, duplicate or invalid checksum/
            : /diffx:/;
      await assert.rejects(runInstaller(workspace, channel), expected);
      if (
        failure === "checksum" ||
        failure.startsWith("download-") ||
        failure.includes("manifest-entry")
      ) {
        assert.equal(await readFile(workspace.extractions, "utf8"), "");
      }
      assert.equal(await readFile(installed, "utf8"), "previous binary");
      assert.deepEqual(await readdir(workspace.binDir), ["diffx"]);
      await assertClean(workspace);
    });
  }
}

test("stable resolution failure preserves install", async (t) => {
  const workspace = await createWorkspace(t, { downloadFailure: "/latest/" });
  await mkdir(workspace.binDir, { recursive: true });
  await writeFile(join(workspace.binDir, "diffx"), "previous binary");
  await assert.rejects(runInstaller(workspace, "stable"), /could not resolve/);
  assert.equal(
    await readFile(join(workspace.binDir, "diffx"), "utf8"),
    "previous binary",
  );
  await assertClean(workspace);
});

test("refuses a directory at the binary destination", async (t) => {
  const workspace = await createWorkspace(t);
  await mkdir(join(workspace.binDir, "diffx"), { recursive: true });
  await assert.rejects(
    runInstaller(workspace, "stable"),
    /destination is a directory/,
  );
  assert.deepEqual(await readdir(join(workspace.binDir, "diffx")), []);
  await assertClean(workspace);
});

async function createWorkspace(
  t: TestContext,
  options: Options = {},
): Promise<Workspace> {
  const root = await mkdtemp(join(tmpdir(), "diffx-installer-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const home = join(root, "home space");
  const tools = join(root, "tools");
  const temporary = join(root, "temporary");
  const source = join(root, "source");
  for (const directory of [home, tools, temporary, source]) {
    await mkdir(directory);
  }
  const archive = join(root, "release.tar.gz");
  const manifest = join(root, "checksums.txt");
  const log = join(root, "requests.txt");
  const extractions = join(root, "extractions.txt");
  await writeFile(log, "");
  await writeFile(extractions, "");
  await writeFile(join(source, "diffx-server"), "server must not be installed");
  if (options.symlinkBinary) {
    await symlink("diffx-server", join(source, "diffx"));
  } else if (!options.missingBinary) {
    await writeFile(join(source, "diffx"), binary);
  }
  // The real publisher emits root member names, without a './' prefix.
  await execFileAsync("tar", [
    "-czf",
    archive,
    "-C",
    source,
    ...(options.missingBinary ? [] : ["diffx"]),
    "diffx-server",
  ]);
  const hash = createHash("sha256")
    .update(await readFile(archive))
    .digest("hex");
  const os = options.os === "Darwin" ? "darwin" : "linux";
  const arch =
    options.machine === "arm64" || options.machine === "aarch64"
      ? "arm64"
      : "amd64";
  await writeFile(
    manifest,
    ["1.2.3", "edge"]
      .map((version) => `${hash}  diffx_${version}_${os}_${arch}.tar.gz\n`)
      .join(""),
  );
  const toolNames = [
    "sh",
    "tar",
    "gzip",
    "mkdir",
    "mktemp",
    "rm",
    "awk",
    "chmod",
    "mv",
  ];
  for (const tool of toolNames) {
    if (
      tool === options.missingTool ||
      (tool === "mv" && options.renameFailure)
    )
      continue;
    const { stdout } = await execFileAsync("/bin/sh", [
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
appendFileSync(${JSON.stringify(extractions)}, "extract\\n");
const result = spawnSync(${JSON.stringify(stdout.trim())}, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);
`,
      );
    } else {
      await symlink(stdout.trim(), join(tools, tool));
    }
  }
  // Emulate both hashing CLIs so the fixtures also run on hosts with only one.
  const hashTool = options.hashTool ?? "sha256sum";
  if (hashTool !== "none") {
    await executable(
      join(tools, hashTool),
      `
const { createHash } = require("node:crypto");
const { readFileSync } = require("node:fs");
if (process.argv.slice(2).join(" ") !== ${JSON.stringify(hashTool === "shasum" ? "-a 256" : "")}) process.exit(2);
process.stdout.write(createHash("sha256").update(readFileSync(0)).digest("hex") + "  -\\n");
`,
    );
  }
  const fixturePath = join(root, "fixture.json");
  await writeFile(
    fixturePath,
    JSON.stringify({
      ...options,
      archive,
      manifest,
      log,
      helper: join(repository, "scripts/install-common.sh"),
    }),
  );
  if (options.missingTool !== "curl") {
    await executable(join(tools, "curl"), curlFixture);
  }
  await executable(
    join(tools, "uname"),
    `
const option = process.argv[2];
process.stdout.write((option === "-s" ? ${JSON.stringify(options.os ?? "Linux")} : ${JSON.stringify(options.machine ?? "x86_64")}) + "\\n");
`,
  );
  if (options.renameFailure) {
    await executable(join(tools, "mv"), "process.exit(1);\n");
  }
  return {
    root,
    home,
    tools,
    temporary,
    binDir: join(home, ".local/bin"),
    manifest,
    archive,
    log,
    extractions,
    env: {
      ...process.env,
      HOME: home,
      TMPDIR: temporary,
      PATH: tools,
      INSTALL_FIXTURE: fixturePath,
    },
  };
}

function runInstaller(
  workspace: Workspace,
  channel: string,
  args: string[] = [],
) {
  return execFileAsync(
    "/bin/sh",
    [
      join(repository, channel === "edge" ? "install-edge.sh" : "install.sh"),
      ...args,
    ],
    { cwd: workspace.root, env: workspace.env },
  );
}

async function executable(path: string, script: string): Promise<void> {
  await writeFile(path, `#!${process.execPath}\n${script}`);
  await chmod(path, 0o755);
}

async function requests(workspace: Workspace): Promise<string[]> {
  const log = await readFile(workspace.log, "utf8");
  return log.trim() ? log.trim().split("\n") : [];
}

async function assertClean(workspace: Workspace): Promise<void> {
  assert.deepEqual(await readdir(workspace.temporary), []);
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
  // No shell profiles or unrelated files may appear in the user's home.
  assert.ok((await readdir(workspace.home)).every((name) => name === ".local"));
}

const curlFixture = `
const { appendFileSync, copyFileSync, readFileSync, writeFileSync } = require("node:fs");
const fixture = JSON.parse(readFileSync(process.env.INSTALL_FIXTURE, "utf8"));
const args = process.argv.slice(2);
const url = args.at(-1);
const output = args[args.indexOf("--output") + 1];
appendFileSync(fixture.log, url + "\\n");
if (fixture.downloadFailure && url.includes(fixture.downloadFailure)) {
  if (output !== "/dev/null") writeFileSync(output, "partial download");
  process.exit(22);
}
if (url === ${JSON.stringify(helperURL)}) {
  if (fixture.helperFailure) {
    writeFileSync(output, fixture.helperFailure === "empty" ? "" : 'printf ran > "$HOME/helper-ran"\\n');
    process.exit(fixture.helperFailure === "partial" ? 22 : 0);
  }
  copyFileSync(fixture.helper, output);
} else if (url === ${JSON.stringify(`${releases}/latest/download/checksums.txt`)}) {
  if (!args.includes("--write-out") || args.includes("--location")) process.exit(2);
  process.stdout.write(fixture.latestURL ?? ${JSON.stringify(`${releases}/download/v1.2.3/checksums.txt`)});
} else if (url.endsWith("/checksums.txt")) {
  copyFileSync(fixture.manifest, output);
} else if (url.endsWith(".tar.gz")) {
  copyFileSync(fixture.archive, output);
} else {
  process.stderr.write("Unexpected URL: " + url);
  process.exit(2);
}
`;
