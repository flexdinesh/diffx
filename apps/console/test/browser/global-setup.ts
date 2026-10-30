import { execFile } from "node:child_process";
import { access } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const repositoryRoot = fileURLToPath(new URL("../../../../", import.meta.url));

// Full checks supply their builds. Standalone runs use this checkout's build
// task, which also stages the console assets diffx-console embeds.
export default async function globalSetup() {
  const server = process.env.DIFFX_FIXTURE_SERVER_BINARY;
  if (server) {
    // The build task writes diffx-console beside diffx-server.
    process.env.DIFFX_FIXTURE_CONSOLE_BINARY ??= join(
      dirname(server),
      "diffx-console",
    );
    await access(server);
    await access(process.env.DIFFX_FIXTURE_CONSOLE_BINARY);
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
  process.env.DIFFX_FIXTURE_SERVER_BINARY = join(
    repositoryRoot,
    "dist",
    "diffx-server",
  );
  process.env.DIFFX_FIXTURE_CONSOLE_BINARY = join(
    repositoryRoot,
    "dist",
    "diffx-console",
  );
}
