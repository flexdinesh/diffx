import { execFile } from "node:child_process";
import { access } from "node:fs/promises";
import { dirname, join } from "node:path";
import { promisify } from "node:util";
import { repositoryRoot } from "../fixture-server.ts";

// Full checks supply their builds. Standalone runs use this checkout's build
// task, which also stages assets required by Go's embed directive.
export default async function globalSetup() {
  const existingBinary = process.env.DIFFX_FIXTURE_BINARY;
  const existingWebDir = process.env.DIFFX_FIXTURE_WEB_DIR;
  if (existingBinary && existingWebDir) {
    // The build task writes diffx-server beside the CLI.
    process.env.DIFFX_FIXTURE_SERVER_BINARY ??= join(
      dirname(existingBinary),
      "diffx-server",
    );
    await access(existingBinary);
    await access(process.env.DIFFX_FIXTURE_SERVER_BINARY);
    await access(join(existingWebDir, "index.html"));
    return;
  }
  await promisify(execFile)("mise", ["run", "build"], {
    cwd: repositoryRoot,
    env: {
      ...process.env,
      GOMAXPROCS: "2",
      GOFLAGS: `${process.env.GOFLAGS ?? ""} -p=2`,
    },
  });
  process.env.DIFFX_FIXTURE_BINARY = join(repositoryRoot, "dist", "diffx");
  process.env.DIFFX_FIXTURE_SERVER_BINARY = join(
    repositoryRoot,
    "dist",
    "diffx-server",
  );
  process.env.DIFFX_FIXTURE_WEB_DIR = join(
    repositoryRoot,
    "apps",
    "web",
    "dist",
  );
}
