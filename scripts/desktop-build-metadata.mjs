import { readFileSync } from "node:fs";

const [desktop, cli, version, source] = process.argv.slice(2);
if (!desktop || !cli || !version || !source)
  throw new Error("usage: desktop-build-metadata.mjs APP CLI VERSION SOURCE");

for (const [role, binary] of [
  ["desktop", desktop],
  ["cli", cli],
]) {
  const record = Buffer.from(`diffx-build-v1|${role}|${version}|${source}|end`);
  if (!readFileSync(binary).includes(record))
    throw new Error(`binary metadata mismatch: ${role} version/source`);
}
