import { spawn, type ChildProcess } from "node:child_process";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import type { Plugin } from "vite";

function stopProcess(child: ChildProcess, detached: boolean) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const processId = child.pid;
  if (!detached || processId === undefined) {
    child.kill("SIGTERM");
    return;
  }
  try {
    process.kill(-processId, "SIGTERM");
  } catch (error) {
    if (!child.killed) child.kill("SIGTERM");
    if (child.exitCode === null && child.signalCode === null)
      console.warn("Unable to stop Go fixture process", error);
  }
}

export const repositoryRoot = fileURLToPath(
  new URL("../../..", import.meta.url),
);

/**
 * Starts an isolated in-memory fixture server. Browser tests pass a prebuilt
 * binary and built web assets; Vite development compiles the CLI instead.
 */
export async function startFixtureProcess(
  options: { binary?: string; webDir?: string } = {},
): Promise<{ target: string; stop: () => void }> {
  // Full checks own the process group through cancellation and queue release.
  const detached =
    process.platform !== "win32" && process.env.DIFFX_CHECK_RUNNER !== "1";
  const child = spawn(
    options.binary ?? "go",
    [
      ...(options.binary ? [] : ["run", "./cmd/diffx"]),
      "dev",
      "--fixture",
      "test/fixtures/sample.diff",
      "--state",
      "memory",
      "--no-browser",
      "--port",
      "0",
      ...(options.webDir ? ["--web-dir", options.webDir] : []),
    ],
    {
      cwd: repositoryRoot,
      detached,
      env: { ...process.env, DIFFX_EXIT_ON_STDIN_CLOSE: "1" },
      stdio: ["pipe", "pipe", "inherit"],
    },
  );
  const output = child.stdout;
  if (!output) {
    stopProcess(child, detached);
    throw new Error("Unable to read Go fixture server address");
  }
  const target = await new Promise<string>((resolve, reject) => {
    let settled = false;
    const lines = createInterface({ input: output });
    const timer = setTimeout(() => {
      fail(new Error("Timed out starting Go fixture server"));
    }, 30_000);
    const fail = (error: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      lines.close();
      stopProcess(child, detached);
      reject(error);
    };
    child.once("error", fail);
    child.once("exit", (code, signal) => {
      fail(
        new Error(
          `Go fixture server exited before startup (${signal ?? code ?? "unknown"})`,
        ),
      );
    });
    lines.on("line", (line) => {
      const port = /url:\s+http:\/\/127\.0\.0\.1:(\d+)/.exec(line)?.[1];
      if (!port || settled) return;
      settled = true;
      clearTimeout(timer);
      lines.close();
      resolve(`http://127.0.0.1:${port}`);
    });
  });
  return { target, stop: () => stopProcess(child, detached) };
}

export async function startFixtureServer(): Promise<{
  target: string;
  plugin: Plugin;
}> {
  const { target, stop } = await startFixtureProcess();
  const interrupt = () => {
    stop();
    process.exit(130);
  };
  const terminate = () => {
    stop();
    process.exit(143);
  };
  process.once("exit", stop);
  process.once("SIGINT", interrupt);
  process.once("SIGTERM", terminate);
  return {
    target,
    plugin: {
      name: "diffx-fixture",
      configureServer(server) {
        server.httpServer?.once("close", () => {
          process.off("exit", stop);
          process.off("SIGINT", interrupt);
          process.off("SIGTERM", terminate);
          stop();
        });
      },
    },
  };
}
