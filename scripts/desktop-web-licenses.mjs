import { execFileSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  readdirSync,
  writeFileSync,
} from "node:fs";
import { createRequire } from "node:module";
import { basename, join, relative } from "node:path";

const [destination] = process.argv.slice(2);
if (!destination)
  throw new Error("usage: desktop-web-licenses.mjs DESTINATION");
const projects = JSON.parse(
  execFileSync(
    "pnpm",
    [
      "--filter",
      "@diffx/desktop",
      "--filter",
      "@diffx/review-app",
      "list",
      "--prod",
      "--depth",
      "0",
      "--json",
    ],
    { encoding: "utf8" },
  ),
);
const packages = new Map();
function collect(dependencies = {}) {
  for (const [name, dependency] of Object.entries(dependencies)) {
    if (!dependency.path) continue;
    visit(dependency.path, name, dependency.version);
  }
}
// pnpm collapses repeated subtrees in list output. Read installed manifests to
// cover their runtime dependencies, including peers linked by pnpm.
function visit(directory, name, version) {
  const root = realpathSync(directory);
  if (packages.has(root)) return;
  const file = join(root, "package.json");
  const manifest = existsSync(file)
    ? JSON.parse(readFileSync(file, "utf8"))
    : {};
  packages.set(root, {
    name: manifest.name ?? name,
    version: manifest.version ?? version ?? "workspace",
  });
  const require = createRequire(file);
  for (const dependency of Object.keys({
    ...manifest.dependencies,
    ...manifest.optionalDependencies,
    ...manifest.peerDependencies,
  })) {
    const candidate = (require.resolve.paths(dependency) ?? [])
      .map((path) => join(path, dependency, "package.json"))
      .find((path) => existsSync(path));
    if (!candidate) {
      if (dependency in (manifest.dependencies ?? {}))
        throw new Error(`missing runtime dependency ${name}: ${dependency}`);
      continue;
    }
    const metadata = JSON.parse(readFileSync(candidate, "utf8"));
    visit(join(candidate, ".."), dependency, metadata.version);
  }
}
for (const project of projects) {
  collect(project.dependencies);
  collect(project.optionalDependencies);
}
if (!packages.size) throw new Error("missing frontend runtime dependencies");
mkdirSync(destination, { recursive: true });
const manifest = [];
for (const [root, dependency] of packages) {
  const output = join(
    destination,
    `${dependency.name.replaceAll("/", "_")}@${dependency.version.replaceAll("/", "_")}`,
  );
  const notices = [];
  function walk(directory) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.isSymbolicLink()) continue;
      const path = join(directory, entry.name);
      if (entry.isDirectory()) walk(path);
      else if (
        /^(?:licen[cs]e|notice|copying|ofl|copyright|patents)(?:[.-]|$)/i.test(
          basename(path),
        ) &&
        !/\.(?:[cm]?jsx?|tsx?|css|json|map)$/.test(path)
      ) {
        const name = relative(root, path);
        const target = join(output, name);
        mkdirSync(join(target, ".."), { recursive: true });
        cpSync(path, target);
        notices.push(name);
      }
    }
  }
  walk(root);
  manifest.push({ ...dependency, notices });
}
writeFileSync(
  join(destination, "dependencies.json"),
  `${JSON.stringify(manifest, null, 2)}\n`,
);
