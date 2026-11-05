import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmod,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { type TestContext } from "node:test";
import { promisify } from "node:util";

const execute = promisify(execFile);
const repository = process.cwd();
const source = "a".repeat(40);
const targets = ["darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"];

interface Fixture {
  root: string;
  out: string;
  calls: string;
  env: NodeJS.ProcessEnv;
}

for (const target of targets) {
  test(`desktop package ${target}: complete platform layout, version pair and licenses`, async (t) => {
    const fixture = await createFixture(t);
    await packageDesktop(fixture, { DIFFX_DESKTOP_TARGET: target });
    await verifyDesktop(fixture, { DIFFX_DESKTOP_TARGET: target });
    const name = `diffx-desktop_1.2.3_${target.replace("/", "_")}.tar.gz`;
    const archive = join(fixture.out, name);
    const digest = createHash("sha256")
      .update(await readFile(archive))
      .digest("hex");
    assert.equal(
      await readFile(`${archive}.sha256`, "utf8"),
      `${digest}  ${name}\n`,
    );
    const extracted = join(fixture.root, "extracted");
    await mkdir(extracted);
    await execute("tar", ["-xzf", archive, "-C", extracted]);
    const darwin = target.startsWith("darwin/");
    const payload = join(
      extracted,
      darwin ? "Diffx Desktop.app" : "diffx-desktop",
    );
    const app = join(
      payload,
      darwin ? "Contents/MacOS/diffx-desktop" : "diffx-desktop",
    );
    const resources = darwin ? join(payload, "Contents/Resources") : payload;
    assert.equal((await execute(app)).stdout, "diffx-desktop 1.2.3\n");
    assert.equal(
      (await execute(join(resources, "diffx"))).stdout,
      "diffx 1.2.3\n",
    );
    assert.equal(
      await readFile(join(resources, "LICENSE"), "utf8"),
      "fixture MIT\n",
    );
    assert.match(
      await readFile(join(resources, "README.txt"), "utf8"),
      /both diffx and diffx-desktop on PATH/,
    );
    assert.equal(
      await readFile(
        join(resources, "licenses/github.com_egoist_mygo/LICENSE"),
        "utf8",
      ),
      "fixture MyGo license\n",
    );
    assert.equal(
      await readFile(
        join(
          resources,
          "licenses/github.com_egoist_mygo/internal/shader/LICENSE",
        ),
        "utf8",
      ),
      "fixture shader license\n",
    );
    assert.match(
      await readFile(join(resources, "BUILD-INFO.txt"), "utf8"),
      new RegExp(`Source: ${source}`),
    );
    const calls = await readFile(fixture.calls, "utf8");
    assert.match(calls, /mygo_noinspector/);
    assert.doesNotMatch(calls, /desktopautomation/);
    assert.match(calls, /CGO_ENABLED=0/);
    assert.match(calls, /GOFLAGS=-p=2 -mod=readonly/);
    assert.match(
      await readFile(join(resources, "licenses/web/dependencies.json"), "utf8"),
      /fixture-web/,
    );
    assert.equal(
      await readFile(
        join(resources, "licenses/web/fixture-web@1.0.0/LICENSE"),
        "utf8",
      ),
      "fixture web license\n",
    );
    if (darwin) {
      assert.match(
        await readFile(join(payload, "Contents/Info.plist"), "utf8"),
        /<string>1.2.3<\/string>/,
      );
      assert.match(
        await readFile(join(payload, "Contents/MacOS/launch"), "utf8"),
        /--desktop-launch/,
      );
    } else
      assert.match(
        await readFile(join(payload, "diffx-desktop.desktop"), "utf8"),
        /Exec=diffx-desktop --desktop-launch/,
      );
    assert.deepEqual((await readdir(fixture.out)).sort(), [
      name,
      `${name}.sha256`,
    ]);
  });
}

for (const failure of ["app-version", "cli-version", "cli-source"]) {
  test(`desktop cross-target verifier refuses mismatched ${failure}`, async (t) => {
    const fixture = await createFixture(t);
    const target = "darwin/arm64";
    await packageDesktop(fixture, {
      DIFFX_DESKTOP_TARGET: target,
      STUB_METADATA_FAILURE: failure,
    });
    await assert.rejects(
      verifyDesktop(fixture, { DIFFX_DESKTOP_TARGET: target }),
      /binary metadata mismatch/,
    );
  });
}

for (const failure of [
  "desktop-build",
  "sidecar-build",
  "automation",
  "inspector",
  "desktop-assets",
  "server-assets",
  "license",
]) {
  test(`desktop packaging ${failure}: preserves existing archive, cleans staging`, async (t) => {
    const fixture = await createFixture(t);
    await mkdir(fixture.out, { recursive: true });
    const archive = join(fixture.out, "diffx-desktop_1.2.3_linux_amd64.tar.gz");
    await writeFile(archive, "previous archive");
    if (failure === "license")
      await rm(join(fixture.root, "module"), { recursive: true });
    await assert.rejects(packageDesktop(fixture, { STUB_FAILURE: failure }));
    assert.equal(await readFile(archive, "utf8"), "previous archive");
    assert.deepEqual(await readdir(fixture.out), [
      "diffx-desktop_1.2.3_linux_amd64.tar.gz",
    ]);
  });
}

for (const env of [
  { DIFFX_SOURCE_SHA: "b".repeat(40) },
  { DIFFX_DESKTOP_TARGET: "windows/amd64" },
  { DIFFX_RELEASE_VERSION: "1.2.3/../../bad" },
  { DIFFX_RELEASE_VERSION: "dev" },
]) {
  test(`desktop packaging rejects invalid input ${JSON.stringify(env)}`, async (t) => {
    const fixture = await createFixture(t);
    await assert.rejects(packageDesktop(fixture, env), /desktop package:/);
    assert.equal(await readFile(fixture.calls, "utf8"), "");
  });
}

test("desktop release task rejects dirty source before build", async (t) => {
  const fixture = await createFixture(t);
  await assert.rejects(
    execute(
      "/bin/sh",
      [join(repository, ".mise/tasks/release/desktop-package")],
      {
        cwd: fixture.root,
        env: { ...fixture.env, STUB_DIRTY: "1" },
      },
    ),
    /source must be clean/,
  );
  assert.equal(await readFile(fixture.calls, "utf8"), "");
});

test("desktop publisher uploads four archives, then independent checksums", async (t) => {
  const fixture = await publishFixture(t);
  await publish(fixture);
  const calls = (await readFile(fixture.calls, "utf8"))
    .trim()
    .split("\n")
    .map((line): unknown => JSON.parse(line));
  assert.equal(calls.length, 3);
  assert.deepEqual(calls.at(-1), [
    "release",
    "upload",
    "v1.2.3",
    "apps/desktop/build/release/desktop-checksums.txt",
  ]);
  const manifest = await readFile(
    join(fixture.out, "desktop-checksums.txt"),
    "utf8",
  );
  assert.equal(manifest.trim().split("\n").length, 4);
  assert.doesNotMatch(
    await readFile(fixture.calls, "utf8"),
    /checksums\.txt.*--clobber|release.*create|--latest/,
  );
});

test("desktop publisher preserves complete stable assets and refuses partial assets", async (t) => {
  const fixture = await publishFixture(t);
  const assets = [
    "desktop-checksums.txt",
    ...targets.map(
      (target) => `diffx-desktop_1.2.3_${target.replace("/", "_")}.tar.gz`,
    ),
  ].join("\n");
  const { stdout } = await publish(fixture, { STUB_ASSETS: assets });
  assert.match(stdout, /preserving stable assets/);
  assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
  await assert.rejects(
    publish(fixture, { STUB_ASSETS: "desktop-checksums.txt" }),
    /partial stable desktop publication/,
  );
  assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
});

test("desktop publisher refuses corrupted archives before any upload", async (t) => {
  const fixture = await publishFixture(t);
  await writeFile(
    join(fixture.out, "diffx-desktop_1.2.3_linux_amd64.tar.gz"),
    "corrupt",
  );
  await assert.rejects(publish(fixture), /checksum mismatch/);
  assert.equal(await readFile(fixture.calls, "utf8"), "");
});

for (const variable of [
  "STUB_HEAD",
  "STUB_TAG",
  "STUB_MAIN",
  "STUB_REMOTE_TAG",
]) {
  test(`desktop edge publisher refuses stale ${variable}`, async (t) => {
    const fixture = await publishFixture(t, "edge");
    await assert.rejects(
      publish(fixture, { RELEASE_TAG: "edge", [variable]: "b".repeat(40) }),
    );
    assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
  });
}

test("desktop edge publisher clobbers only desktop assets and publishes manifest last", async (t) => {
  const fixture = await publishFixture(t, "edge");
  await publish(fixture, { RELEASE_TAG: "edge" });
  const calls = (await readFile(fixture.calls, "utf8"))
    .trim()
    .split("\n")
    .map((line): unknown => JSON.parse(line));
  assert.deepEqual(calls.at(-1), [
    "release",
    "upload",
    "edge",
    "apps/desktop/build/release/desktop-checksums.txt",
    "--clobber",
  ]);
  assert.equal(calls.length, 3);
  const manifest = await readFile(
    join(fixture.out, "desktop-checksums.txt"),
    "utf8",
  );
  assert.equal(manifest.trim().split("\n").length, 3);
  assert.doesNotMatch(manifest, /darwin_amd64/);
});

test("desktop stable publisher requires the macOS Intel archive", async (t) => {
  const fixture = await publishFixture(t);
  await rm(join(fixture.out, "diffx-desktop_1.2.3_darwin_amd64.tar.gz"));
  await assert.rejects(publish(fixture), /missing .*darwin_amd64/);
  assert.equal(await readFile(fixture.calls, "utf8"), "");
});

async function publishFixture(
  t: TestContext,
  version = "1.2.3",
): Promise<Fixture> {
  const fixture = await createFixture(t);
  await mkdir(fixture.out, { recursive: true });
  for (const target of targets) {
    if (version === "edge" && target === "darwin/amd64") continue;
    const name = `diffx-desktop_${version}_${target.replace("/", "_")}.tar.gz`;
    await writeFile(join(fixture.out, name), "archive payload");
    const digest = createHash("sha256").update("archive payload").digest("hex");
    await writeFile(
      join(fixture.out, `${name}.sha256`),
      `${digest}  ${name}\n`,
    );
  }
  return fixture;
}

function packageDesktop(fixture: Fixture, env: NodeJS.ProcessEnv = {}) {
  return execute(
    "/bin/sh",
    [join(fixture.root, "scripts/desktop-package.sh")],
    {
      cwd: fixture.root,
      env: { ...fixture.env, ...env },
    },
  );
}

function verifyDesktop(fixture: Fixture, env: NodeJS.ProcessEnv = {}) {
  return execute(
    "/bin/sh",
    [join(fixture.root, "scripts/desktop-verify-package.sh")],
    { cwd: fixture.root, env: { ...fixture.env, ...env } },
  );
}

function publish(fixture: Fixture, env: NodeJS.ProcessEnv = {}) {
  return execute(
    "/bin/sh",
    [join(repository, ".mise/tasks/release/desktop-publish")],
    {
      cwd: fixture.root,
      env: { ...fixture.env, RELEASE_TAG: "v1.2.3", ...env },
    },
  );
}

async function createFixture(t: TestContext): Promise<Fixture> {
  const root = await mkdtemp(join(tmpdir(), "diffx-desktop-package-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const directory of [
    "scripts",
    "web-module",
    "apps/desktop/internal/assets/dist/assets",
    "internal/webui/dist/assets",
    "docs",
    "tools",
    "module/internal/shader",
    "goroot",
  ])
    await mkdir(join(root, directory), { recursive: true });
  await cp(
    join(repository, "scripts/desktop-package.sh"),
    join(root, "scripts/desktop-package.sh"),
  );
  await cp(
    join(repository, "scripts/desktop-verify-package.sh"),
    join(root, "scripts/desktop-verify-package.sh"),
  );
  for (const name of [
    "desktop-assets.mjs",
    "desktop-web-licenses.mjs",
    "desktop-build-metadata.mjs",
  ])
    await cp(join(repository, "scripts", name), join(root, "scripts", name));
  await cp(
    join(repository, "apps/desktop/resources"),
    join(root, "apps/desktop/resources"),
    { recursive: true },
  );
  await writeFile(join(root, "LICENSE"), "fixture MIT\n");
  await writeFile(join(root, "docs/release.md"), "fixture web licensing\n");
  await writeFile(join(root, "web-module/LICENSE"), "fixture web license\n");
  for (const directory of [
    "apps/desktop/internal/assets/dist",
    "internal/webui/dist",
  ]) {
    await writeFile(join(root, directory, "index.html"), "# fixture index\n");
    await writeFile(
      join(root, directory, "assets/main.js"),
      "# fixture script\n",
    );
  }
  await writeFile(join(root, "module/LICENSE"), "fixture MyGo license\n");
  await writeFile(
    join(root, "module/internal/shader/LICENSE"),
    "fixture shader license\n",
  );
  for (const name of ["LICENSE", "PATENTS"])
    await writeFile(join(root, "goroot", name), "fixture Go license\n");
  const calls = join(root, "calls");
  await writeFile(calls, "");
  const env = {
    ...process.env,
    PATH: `${join(root, "tools")}:${process.env.PATH ?? ""}`,
    STUB_ROOT: root,
    STUB_SOURCE: source,
    STUB_CALLS: calls,
    DIFFX_SOURCE_SHA: source,
    DIFFX_RELEASE_VERSION: "1.2.3",
    DIFFX_DESKTOP_TARGET: "linux/amd64",
    GOFLAGS: "-p=2",
  };
  for (const [name, script] of [
    ["go", goStub],
    ["git", gitStub],
    ["gh", ghStub],
    ["pnpm", pnpmStub],
  ]) {
    if (!name || !script) throw new Error("missing stub");
    const path = join(root, "tools", name);
    await writeFile(path, `#!${process.execPath}\n${script}`);
    await chmod(path, 0o755);
  }
  return { root, calls, env, out: join(root, "apps/desktop/build/release") };
}

const gitStub = `
const args = process.argv.slice(2);
const env = process.env;
if (args[0] === "status") console.log(env.STUB_DIRTY ? " M changed" : "");
else if (args[0] === "fetch") process.exit(0);
else if (args[0] === "ls-remote") console.log((env.STUB_REMOTE_TAG || env.STUB_SOURCE) + "\\trefs/tags/edge");
else if (args[0] === "rev-parse") console.log(args[1] === "HEAD" ? env.STUB_HEAD || env.STUB_SOURCE : args[1].includes("origin/main") ? env.STUB_MAIN || env.STUB_SOURCE : env.STUB_TAG || env.STUB_SOURCE);
else process.exit(2);
`;

const goStub = `
const { appendFileSync, readFileSync, writeFileSync, chmodSync } = require("node:fs");
const env = process.env;
const all = process.argv.slice(2);
const desktop = all[0] === "-C";
const args = desktop ? all.slice(2) : all;
if (args[0] === "env") { console.log(args[1] === "GOHOSTOS" ? "linux" : args[1] === "GOHOSTARCH" ? "amd64" : env.STUB_ROOT + "/goroot"); process.exit(0); }
appendFileSync(env.STUB_CALLS, JSON.stringify(all) + " CGO_ENABLED=" + env.CGO_ENABLED + " GOFLAGS=" + env.GOFLAGS + "\\n");
if (args[0] === "list") {
  if (args.includes("{{.ImportPath}}")) console.log(["automation", "inspector"].includes(env.STUB_FAILURE) ? "github.com/egoist/mygo/plugins/" + env.STUB_FAILURE : "github.com/egoist/mygo/webview");
  else console.log("github.com/egoist/mygo|" + env.STUB_ROOT + "/module");
} else if (args[0] === "version") {
  const output = args.at(-1);
  const app = output.endsWith("/diffx-desktop");
  const [os, arch] = env.DIFFX_DESKTOP_TARGET.split("/");
  // Real Go omits -ldflags for -trimpath builds.
  console.log('\\tbuild\\t-trimpath=true\\n\\tbuild\\t-tags=mygo_noinspector\\n\\tbuild\\tGOOS=' + os + '\\n\\tbuild\\tGOARCH=' + arch);
  if (app) console.log("\\tdep\\tgithub.com/egoist/mygo\\tv0.7.4");
}
else if (args[0] === "build") {
  if (env.STUB_FAILURE === (desktop ? "desktop-build" : "sidecar-build")) { console.error("build failed"); process.exit(1); }
  let flags = args[args.indexOf("-ldflags") + 1];
  if (env.STUB_METADATA_FAILURE === (desktop ? "app-version" : "cli-version")) flags = flags.replace("|1.2.3|", "|1.2.30|");
  if (!desktop && env.STUB_METADATA_FAILURE === "cli-source") flags = flags.replace("|" + env.STUB_SOURCE + "|end", "|" + "b".repeat(40) + "|end");
  const version = flags.match(/Version=([^ ]+)/)?.[1];
  if (!version) process.exit(2);
  const output = args[args.indexOf("-o") + 1];
  writeFileSync(output, "#!/bin/sh\\nprintf '%s\\\\n' '" + (desktop ? "diffx-desktop" : "diffx") + " " + version + "'\\n");
  appendFileSync(output, "# fixture-build-flags " + flags + "\\n");
  if (env.STUB_FAILURE !== (desktop ? "desktop-assets" : "server-assets")) appendFileSync(output, "# fixture index\\n# fixture script\\n");
  chmodSync(output, 0o755);
} else process.exit(2);
`;

const pnpmStub = `
console.log(JSON.stringify([{ dependencies: { "fixture-web": {
  version: "1.0.0", path: process.env.STUB_ROOT + "/web-module",
} } }]));
`;

const ghStub = `
const { appendFileSync, readFileSync } = require("node:fs");
const args = process.argv.slice(2);
appendFileSync(process.env.STUB_CALLS, JSON.stringify(args) + "\\n");
if (args[0] === "release" && args[1] === "view") console.log(process.env.STUB_ASSETS || "");
else if (args[0] === "release" && args[1] === "upload") {
  for (const path of args.slice(3).filter((value) => !value.startsWith("--"))) readFileSync(path);
} else process.exit(2);
`;
