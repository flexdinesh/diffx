import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// Go embed stores file bytes unchanged, including in cross-compiled executables.
// Inspect actual release executables rather than trusting a staging manifest.
function verify(binary, directory) {
  const executable = readFileSync(binary);
  readFileSync(join(directory, "index.html"));
  const entries = readdirSync(join(directory, "assets"));
  if (!entries.some((name) => name.endsWith(".js")))
    throw new Error(`missing frontend JavaScript: ${directory}`);
  function walk(root) {
    for (const entry of readdirSync(root, { withFileTypes: true })) {
      const path = join(root, entry.name);
      if (entry.isDirectory()) walk(path);
      else {
        const contents = readFileSync(path);
        if (contents.length && !executable.includes(contents))
          throw new Error(`${binary} does not embed ${path}`);
      }
    }
  }
  walk(directory);
}

const [desktop, server] = process.argv.slice(2);
if (!desktop || !server)
  throw new Error("usage: desktop-assets.mjs APP SERVER");
verify(desktop, "apps/desktop/internal/assets/dist");
verify(server, "internal/webui/dist");
