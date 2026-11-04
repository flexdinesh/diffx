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
  test(`native package ${target}: complete platform layout, version pair and licenses`, async (t) => {
    const fixture = await createFixture(t);
    await packageNative(fixture, { DIFFX_NATIVE_TARGET: target });
    const name = `diffx-native_1.2.3_${target.replace("/", "_")}.tar.gz`;
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
      darwin ? "Diffx Native Experimental.app" : "diffx-native",
    );
    const app = join(
      payload,
      darwin ? "Contents/MacOS/diffx-native" : "diffx-native",
    );
    const resources = darwin ? join(payload, "Contents/Resources") : payload;
    assert.equal((await execute(app)).stdout, "diffx-native 1.2.3\n");
    assert.equal(
      (await execute(join(resources, "diffx"))).stdout,
      "diffx 1.2.3\n",
    );
    assert.equal(
      await readFile(join(resources, "LICENSE"), "utf8"),
      "fixture MIT\n",
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
    assert.doesNotMatch(calls, /nativeautomation/);
    assert.match(calls, /CGO_ENABLED=0/);
    assert.match(calls, /GOFLAGS=-p=2 -mod=readonly/);
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
        await readFile(join(payload, "diffx-native.desktop"), "utf8"),
        /Exec=diffx-native --desktop-launch/,
      );
    assert.deepEqual((await readdir(fixture.out)).sort(), [
      name,
      `${name}.sha256`,
    ]);
  });
}

for (const failure of [
  "native-build",
  "sidecar-build",
  "automation",
  "license",
]) {
  test(`native packaging ${failure}: preserves existing archive, cleans staging`, async (t) => {
    const fixture = await createFixture(t);
    await mkdir(fixture.out, { recursive: true });
    const archive = join(fixture.out, "diffx-native_1.2.3_linux_amd64.tar.gz");
    await writeFile(archive, "previous archive");
    if (failure === "license")
      await rm(join(fixture.root, "module"), { recursive: true });
    await assert.rejects(packageNative(fixture, { STUB_FAILURE: failure }));
    assert.equal(await readFile(archive, "utf8"), "previous archive");
    assert.deepEqual(await readdir(fixture.out), [
      "diffx-native_1.2.3_linux_amd64.tar.gz",
    ]);
  });
}

for (const env of [
  { DIFFX_SOURCE_SHA: "b".repeat(40) },
  { DIFFX_NATIVE_TARGET: "windows/amd64" },
  { DIFFX_RELEASE_VERSION: "1.2.3/../../bad" },
  { DIFFX_RELEASE_VERSION: "dev" },
]) {
  test(`native packaging rejects invalid input ${JSON.stringify(env)}`, async (t) => {
    const fixture = await createFixture(t);
    await assert.rejects(packageNative(fixture, env), /native package:/);
    assert.equal(await readFile(fixture.calls, "utf8"), "");
  });
}

test("native release task rejects dirty source before build", async (t) => {
  const fixture = await createFixture(t);
  await assert.rejects(
    execute(
      "/bin/sh",
      [join(repository, ".mise/tasks/release/native-package")],
      {
        cwd: fixture.root,
        env: { ...fixture.env, STUB_DIRTY: "1" },
      },
    ),
    /source must be clean/,
  );
  assert.equal(await readFile(fixture.calls, "utf8"), "");
});

test("native publisher uploads four archives, then independent checksums", async (t) => {
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
    "apps/desktop/build/release/native-checksums.txt",
  ]);
  const manifest = await readFile(
    join(fixture.out, "native-checksums.txt"),
    "utf8",
  );
  assert.equal(manifest.trim().split("\n").length, 4);
  assert.doesNotMatch(
    await readFile(fixture.calls, "utf8"),
    /checksums\.txt.*--clobber|release.*create|--latest/,
  );
});

test("native publisher preserves complete stable assets and refuses partial assets", async (t) => {
  const fixture = await publishFixture(t);
  const assets = [
    "native-checksums.txt",
    ...targets.map(
      (target) => `diffx-native_1.2.3_${target.replace("/", "_")}.tar.gz`,
    ),
  ].join("\n");
  const { stdout } = await publish(fixture, { STUB_ASSETS: assets });
  assert.match(stdout, /preserving stable assets/);
  assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
  await assert.rejects(
    publish(fixture, { STUB_ASSETS: "native-checksums.txt" }),
    /partial stable native publication/,
  );
  assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
});

test("native publisher refuses corrupted archives before any upload", async (t) => {
  const fixture = await publishFixture(t);
  await writeFile(
    join(fixture.out, "diffx-native_1.2.3_linux_amd64.tar.gz"),
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
  test(`native edge publisher refuses stale ${variable}`, async (t) => {
    const fixture = await publishFixture(t, "edge");
    await assert.rejects(
      publish(fixture, { RELEASE_TAG: "edge", [variable]: "b".repeat(40) }),
    );
    assert.doesNotMatch(await readFile(fixture.calls, "utf8"), /upload/);
  });
}

test("native edge publisher clobbers only native assets and publishes manifest last", async (t) => {
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
    "apps/desktop/build/release/native-checksums.txt",
    "--clobber",
  ]);
  assert.equal(calls.length, 3);
});

async function publishFixture(
  t: TestContext,
  version = "1.2.3",
): Promise<Fixture> {
  const fixture = await createFixture(t);
  await mkdir(fixture.out, { recursive: true });
  for (const target of targets) {
    const name = `diffx-native_${version}_${target.replace("/", "_")}.tar.gz`;
    await writeFile(join(fixture.out, name), "archive payload");
    const digest = createHash("sha256").update("archive payload").digest("hex");
    await writeFile(
      join(fixture.out, `${name}.sha256`),
      `${digest}  ${name}\n`,
    );
  }
  return fixture;
}

function packageNative(fixture: Fixture, env: NodeJS.ProcessEnv = {}) {
  return execute("/bin/sh", [join(fixture.root, "scripts/native-package.sh")], {
    cwd: fixture.root,
    env: { ...fixture.env, ...env },
  });
}

function publish(fixture: Fixture, env: NodeJS.ProcessEnv = {}) {
  return execute(
    "/bin/sh",
    [join(repository, ".mise/tasks/release/native-publish")],
    {
      cwd: fixture.root,
      env: { ...fixture.env, RELEASE_TAG: "v1.2.3", ...env },
    },
  );
}

async function createFixture(t: TestContext): Promise<Fixture> {
  const root = await mkdtemp(join(tmpdir(), "diffx-native-package-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const directory of [
    "scripts",
    "tools/native-tokens/themes",
    "docs",
    "tools",
    "module/internal/shader",
    "goroot",
  ])
    await mkdir(join(root, directory), { recursive: true });
  await cp(
    join(repository, "scripts/native-package.sh"),
    join(root, "scripts/native-package.sh"),
  );
  await cp(
    join(repository, "apps/desktop/resources"),
    join(root, "apps/desktop/resources"),
    { recursive: true },
  );
  await writeFile(join(root, "LICENSE"), "fixture MIT\n");
  await writeFile(join(root, "docs/release.md"), "fixture web licensing\n");
  await writeFile(
    join(root, "tools/native-tokens/themes/LICENSE-theme"),
    "fixture theme license\n",
  );
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
    DIFFX_NATIVE_TARGET: "linux/amd64",
    GOFLAGS: "-p=2",
  };
  for (const [name, script] of [
    ["go", goStub],
    ["git", gitStub],
    ["gh", ghStub],
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
const { appendFileSync, writeFileSync, chmodSync } = require("node:fs");
const env = process.env;
const all = process.argv.slice(2);
const native = all[0] === "-C";
const args = native ? all.slice(2) : all;
if (args[0] === "env") { console.log(env.STUB_ROOT + "/goroot"); process.exit(0); }
appendFileSync(env.STUB_CALLS, JSON.stringify(all) + " CGO_ENABLED=" + env.CGO_ENABLED + " GOFLAGS=" + env.GOFLAGS + "\\n");
if (args[0] === "list") {
  if (args.includes("{{.ImportPath}}")) console.log(env.STUB_FAILURE === "automation" ? "github.com/egoist/mygo/plugins/automation" : "github.com/egoist/mygo/ui");
  else console.log("github.com/egoist/mygo|" + env.STUB_ROOT + "/module");
} else if (args[0] === "version") console.log("build tags mygo_noinspector");
else if (args[0] === "build") {
  if (env.STUB_FAILURE === (native ? "native-build" : "sidecar-build")) { console.error("build failed"); process.exit(1); }
  const flags = args[args.indexOf("-ldflags") + 1];
  const version = flags.match(/Version=([^ ]+)/)?.[1];
  if (!version) process.exit(2);
  const output = args[args.indexOf("-o") + 1];
  writeFileSync(output, "#!/bin/sh\\nprintf '%s\\\\n' '" + (native ? "diffx-native" : "diffx") + " " + version + "'\\n");
  chmodSync(output, 0o755);
} else process.exit(2);
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
