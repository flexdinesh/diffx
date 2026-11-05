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
import test, { type TestContext } from "node:test";
import { promisify } from "node:util";

const execute = promisify(execFile);
const collector = join(process.cwd(), "scripts/desktop-web-licenses.mjs");

async function fixture(t: TestContext, missingRequired: boolean) {
  const root = await mkdtemp(join(tmpdir(), "diffx-web-licenses-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const bin = join(root, "bin");
  const alpha = join(root, "node_modules/alpha");
  const beta = join(root, "node_modules/beta");
  const peer = join(root, "node_modules/peer");
  for (const directory of [bin, alpha, beta, peer])
    await mkdir(directory, { recursive: true });
  await writeFile(
    join(alpha, "package.json"),
    JSON.stringify({
      name: "alpha",
      private: true,
      dependencies: {
        beta: "1.0.0",
        ...(missingRequired ? { missing: "1.0.0" } : {}),
      },
      optionalDependencies: { "aix-only": "1.0.0" },
      peerDependencies: { peer: "1.0.0", "absent-peer": "1.0.0" },
    }),
  );
  await writeFile(
    join(beta, "package.json"),
    JSON.stringify({
      name: "beta",
      version: "1.0.0",
      dependencies: { peer: "1.0.0" },
    }),
  );
  await writeFile(
    join(peer, "package.json"),
    JSON.stringify({ name: "peer", version: "1.0.0" }),
  );
  for (const directory of [alpha, beta, peer])
    await writeFile(
      join(directory, "LICENSE"),
      `${directory.split("/").at(-1)} license\n`,
    );
  const listing = join(root, "listing.json");
  await writeFile(
    listing,
    JSON.stringify([
      {
        dependencies: {
          alpha: {
            path: alpha,
            // Expanded pnpm listings can advertise absent platform packages.
            optionalDependencies: {
              "aix-only": { version: "1.0.0", path: join(root, "absent-aix") },
            },
          },
        },
      },
    ]),
  );
  await writeFile(
    join(bin, "pnpm"),
    '#!/usr/bin/env node\nprocess.stdout.write(require("node:fs").readFileSync(process.env.DIFFX_LICENSE_LIST));\n',
  );
  await chmod(join(bin, "pnpm"), 0o755);
  return {
    output: join(root, "licenses"),
    env: {
      ...process.env,
      PATH: `${bin}:${process.env.PATH ?? ""}`,
      DIFFX_LICENSE_LIST: listing,
    },
  };
}

test("web notices cover installed transitive and peer dependencies, excluding absent platform packages", async (t) => {
  const { output, env } = await fixture(t, false);
  await execute(process.execPath, [collector, output], { env });
  const manifest = await readFile(join(output, "dependencies.json"), "utf8");
  for (const name of ["alpha", "beta", "peer"]) {
    assert.equal(
      (manifest.match(new RegExp(`"name": "${name}"`, "g")) ?? []).length,
      1,
    );
    const version = name === "alpha" ? "workspace" : "1.0.0";
    assert.equal(
      await readFile(join(output, `${name}@${version}/LICENSE`), "utf8"),
      `${name} license\n`,
    );
  }
  assert.doesNotMatch(manifest, /aix-only|absent-peer/);
});

test("web notice collection fails when a required runtime dependency is missing", async (t) => {
  const { output, env } = await fixture(t, true);
  await assert.rejects(
    execute(process.execPath, [collector, output], { env }),
    /missing runtime dependency alpha: missing/,
  );
});
