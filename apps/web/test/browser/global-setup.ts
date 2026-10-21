import { execFile } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { build } from "vite";
import { repositoryRoot } from "../fixture-server.ts";

// Build the web app and CLI once; each worker then starts its own fixture
// server from them, so tests never share review state across workers.
export default async function globalSetup() {
  const directory = await mkdtemp(join(tmpdir(), "diffx-browser-"));
  const webDir = join(directory, "web");
  const binary = join(directory, "diffx");
  await Promise.all([
    build({
      root: fileURLToPath(new URL("../..", import.meta.url)),
      logLevel: "warn",
      build: { outDir: webDir, emptyOutDir: true },
    }),
    promisify(execFile)("go", ["build", "-o", binary, "./cmd/diffx"], {
      cwd: repositoryRoot,
    }),
  ]);
  process.env.DIFFX_FIXTURE_BINARY = binary;
  process.env.DIFFX_FIXTURE_WEB_DIR = webDir;
  return () => rm(directory, { recursive: true, force: true });
}
