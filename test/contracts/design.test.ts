import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
  checkDesign,
  readDesignSources,
  readFrontendSources,
} from "../../tools/check-design.ts";

const ui = "packages/ui/src/";
const web = "apps/web/src/";

function check(text: string, path = "new.css") {
  return checkDesign([{ path, text }]);
}

test("semantic tokens compose colors, type, geometry and spacing", () => {
  assert.deepEqual(
    check(`.control {
    color: var(--fg); background: color-mix(in srgb, var(--accent) 10%, transparent);
    font: var(--weight-medium) var(--text-sm) var(--font-sans);
    border: 1px solid currentColor; border-radius: var(--radius-control) 0;
    box-shadow: var(--shadow-overlay); padding: 0 var(--space-3); margin-left: auto;
  }`),
    [],
  );
});

test("literal palette cannot escape through custom properties, fallbacks or shorthand", () => {
  for (const declaration of [
    "color: red",
    "background: #abc",
    "border: 1px solid rebeccapurple",
    "--new-color: hsl(1 2% 3%)",
    "color: var(--unknown, red)",
    "--alias: blue",
    "background: color-mix(in srgb, var(--bg), white)",
  ]) {
    assert.equal(
      check(`.new { ${declaration} !important }`).length,
      1,
      declaration,
    );
  }
});

test("canonical source ownership does not exempt unrelated files", () => {
  assert.deepEqual(check(":root { --fg: #fff }", `${ui}tokens.css`), []);
  assert.equal(check(".new { color: #fff }", `${ui}tokens.css`).length, 1);
  assert.equal(
    check(":root { --fg: #fff }", `${ui}nested/tokens.css`).length,
    1,
  );
  // Apps compose the shared tokens; an app-local copy is not a second owner.
  assert.equal(check(":root { --fg: #fff }", `${web}tokens.css`).length, 1);
  assert.deepEqual(
    check(
      ":root { --text-xs: .75rem; font-size: 100% }",
      `${ui}typography.css`,
    ),
    [],
  );
  assert.equal(
    check(":root { --text-xs: .75rem }", `${web}typography.css`).length,
    1,
  );
  assert.equal(
    check(":root { --text-xs: .75rem }", `${ui}tokens.css`).length,
    1,
  );
});

test("CSS parser handles nesting, comments, strings and multiline values", () => {
  const result = check(`/* color: red; */
@media (width > 100px) {
  .new { content: "color: red; }";
    background-image: url("data:image/svg+xml;unused");
    color: rgb(
      1 2 3
    );
  }
}`);
  assert.equal(result.length, 1);
  assert.equal(result[0]?.line, 5);
  assert.equal(result[0]?.property, "color");
  assert.deepEqual(
    check(`/* @apply text-xl; */ .new { content: "/* @apply text-xl; */" }`),
    [],
  );
});

test("type, radius, shadows and layout lengths require tokens", () => {
  for (const declaration of [
    "font-size: 15px",
    "font-family: Arial",
    "font-family: var(--font-sans), Arial",
    "font-weight: 550",
    "line-height: 1.7",
    "letter-spacing: .2em",
    "border-radius: 50%",
    "border-radius: calc(var(--radius-sm) + .5rem)",
    "--local-radius: .5rem",
    "--space-custom: 13px",
    "--shadow-custom: 0 2px var(--border)",
    "box-shadow: 0 2px 8px var(--border)",
    "padding: 10px",
    "gap: calc(var(--space-2) + 2px)",
  ]) {
    assert.equal(check(`.new { ${declaration} }`).length, 1, declaration);
  }
});

test("optical exceptions remain exact in selector, value and source", () => {
  assert.deepEqual(
    check(".file-reviewed-check { border-radius: 50% }", `${web}review.css`),
    [],
  );
  assert.equal(
    check(".new { border-radius: 50% }", `${web}review.css`).length,
    1,
  );
  assert.equal(
    check(".file-reviewed-check { border-radius: 25% }", `${web}review.css`)
      .length,
    1,
  );
  assert.equal(
    check(".file-reviewed-check { border-radius: 50% }", "new.css").length,
    1,
  );
  assert.deepEqual(check(".new { padding: 1px var(--space-2) }"), []);
});

test("inline styles and named style objects cannot bypass rules", () => {
  for (const text of [
    `const View = () => <div style={{ color: "red" }} />`,
    `const rowStyle = { fontSize: 15 };`,
    `const View = () => <div style={{ fontWeight: customWeight }} />`,
    `const rowStyle = { "border-radius": "12px" };`,
    `const s = {color: "red"}; const View = () => <div style={s} />;`,
    `const base = {color: "red"}; const s = {...base}; const View = () => <div style={s} />;`,
    `const s: React.CSSProperties = {color: "red"};`,
    `const View = () => <svg fill="#abc" />;`,
    'const View = () => <div style={{borderColor: "red"}} />;',
    'const View = () => <div style={{outline: "1px solid red"}} />;',
    "const View = () => <div style={{color: `${palette}`}} />;",
    "const View = () => <div style={{fontSize: `var(--${role})`}} />;",
    "const View = () => <div style={{padding: `${spacing}`}} />;",
    "const View = () => <svg fill={`${palette}`} />;",
  ])
    assert.equal(check(text, "new.tsx").length, 1, text);
  assert.deepEqual(
    check(
      `const rowStyle = { color: "var(--fg)", paddingLeft: \`calc(var(--space-1) + \${depth} * var(--space-3))\` };`,
      "new.tsx",
    ),
    [],
  );
  assert.deepEqual(
    check(`const itemMetrics = { lineHeight: measuredHeight };`, "new.tsx"),
    [],
  );
  // Component props named like SVG attributes are variants, not paint.
  assert.deepEqual(
    check("const View = () => <StatusDot fill={state} />;", "new.tsx"),
    [],
  );
  assert.deepEqual(check(".control { border-radius: 0px }"), []);
});

test("unsafeCSS is checked as CSS and dynamic injection is rejected", () => {
  assert.equal(
    check("const options = { unsafeCSS: `.new { color: #fff }` };", "new.ts")
      .length,
    1,
  );
  assert.equal(
    check("const options = { unsafeCSS: injectedRules };", "new.ts").length,
    1,
  );
  assert.equal(
    check(
      "const options = { unsafeCSS: `.new { ${injectedRules} }` };",
      "new.ts",
    ).length,
    1,
  );
  assert.deepEqual(
    check(
      "const options = { unsafeCSS: `.new { color: var(--fg) }` };",
      "new.ts",
    ),
    [],
  );
});

test("utility styling covers variants, arbitrary properties and escaped JS strings", () => {
  for (const token of [
    "hover:bg-red-500",
    "text-white",
    "text-sm",
    "font-bold",
    "rounded-md",
    "shadow-lg",
    "text-[15px]",
    "bg-[#fff]",
    "[color:red]",
    "focus:[font-size:15px]",
    "[&_svg]:text-[length:12px]",
    "gap-[15px]",
  ]) {
    assert.equal(
      check(`const classes = ${JSON.stringify(token)};`, "new.tsx").length,
      1,
      token,
    );
  }
  assert.equal(
    check(`const classes = "bg-\\u0072ed-500";`, "new.tsx").length,
    1,
  );
  assert.equal(check(`.new { @apply hover:bg-red-500; }`).length, 1);
  assert.deepEqual(
    check(
      `const classes = "hover:bg-[var(--hover)] text-[length:var(--text-xs)] rounded-[var(--radius-sm)] gap-[var(--space-2)]";`,
      "new.tsx",
    ),
    [],
  );
});

test("utility guards distinguish styling from displayed data and follow aliases", () => {
  assert.deepEqual(
    check(
      'const example = "text-sm font-bold"; const View = () => <code>{example}</code>;',
      "new.tsx",
    ),
    [],
  );
  for (const text of [
    'const s = "rounded-md"; const View = () => <div className={s} />;',
    'const base = "text-sm"; const s = base; const View = () => <div className={s} />;',
    'const base = "shadow-lg"; const View = () => <div className={cn(base)} />;',
    'const variants = cva("text-sm", {variants: {size: {large: "text-xl"}}});',
  ])
    assert.ok(check(text, "new.tsx").length > 0, text);
});

test("new nested frontend files enter the check automatically", () => {
  const directory = mkdtempSync(join(tmpdir(), "diffx-design-"));
  try {
    mkdirSync(join(directory, "new"));
    writeFileSync(join(directory, "new", "control.css"), ".new { color: red }");
    writeFileSync(
      join(directory, "new", "Control.tsx"),
      'const classes = "text-xl";',
    );
    writeFileSync(join(directory, "README.md"), "color: red");
    const sources = readDesignSources(directory);
    assert.equal(sources.length, 2);
    assert.equal(checkDesign(sources).length, 2);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("the shared design system and every app enter the check", () => {
  const directory = mkdtempSync(join(tmpdir(), "diffx-design-"));
  try {
    for (const root of [`${ui}components`, "apps/console/src/new"])
      mkdirSync(join(directory, root), { recursive: true });
    mkdirSync(join(directory, "apps/console/public"));
    writeFileSync(join(directory, ui, "components/x.css"), ".x { color: red }");
    writeFileSync(
      join(directory, "apps/console/src/new/control.css"),
      ".new { color: red }",
    );
    writeFileSync(
      join(directory, "apps/console/public/x.css"),
      ".x { color: red }",
    );
    assert.deepEqual(
      readFrontendSources(directory).map((source) => source.path),
      [`${ui}components/x.css`, "apps/console/src/new/control.css"],
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

// A minimal theme bridge and spacing scale, as styles.css and tokens.css declare them.
const theme = [
  {
    path: `${ui}styles.css`,
    text: `@theme inline { --color-hover: var(--hover); --spacing-icon-sm: var(--icon-sm); }
@theme inline reference { --radius-control: var(--radius-control); --text-xs: var(--text-xs); }`,
  },
  {
    path: `${ui}tokens.css`,
    text: ":root { --space-0-5: .125rem; --space-1: .25rem; --space-1-5: .375rem; --space-2: .5rem; --space-3: .75rem }",
  },
];

function checkThemed(text: string, path = "new.tsx") {
  return checkDesign([...theme, { path, text }]).filter(
    (violation) => violation.path === path,
  );
}

test("arbitrary token variables use the utility that already renders them", () => {
  // Audited: ProjectsNav icons and the shadcn tabs trigger before migration.
  for (const token of [
    "size-(--icon-sm)",
    "rounded-[var(--radius-control)]",
    "hover:bg-[var(--hover)]",
    "text-[length:var(--text-xs)]",
    "gap-[var(--space-1)]",
    "-mr-(--space-1-5)",
  ])
    assert.equal(
      checkThemed(`const classes = ${JSON.stringify(token)};`)[0]?.property,
      "token-utility",
      token,
    );
  assert.match(
    checkThemed('const classes = "size-(--icon-sm)";')[0]?.value ?? "",
    /→ size-icon-sm$/,
  );
  assert.deepEqual(
    checkThemed('const classes = "w-(--drawer-width) max-h-[var(--x)]";'),
    [],
  );
});

test("spacing utilities stay on the token scale; 2px steps stay in components", () => {
  // Audited: px-2.5 in the shadcn toggle, py-0.5 in the shadcn badge.
  assert.equal(
    checkThemed('const classes = "px-2.5";')[0]?.property,
    "spacing-scale",
  );
  assert.equal(
    checkThemed('const classes = "px-2.5";', `${ui}components/ui/toggle.tsx`)[0]
      ?.property,
    "spacing-scale",
  );
  assert.equal(
    checkThemed('const classes = "py-0.5";')[0]?.property,
    "spacing-scale",
  );
  assert.deepEqual(
    checkThemed('const classes = "py-0.5";', `${ui}components/ui/badge.tsx`),
    [],
  );
  // Only the shared package owns components; an app folder of that name does not.
  assert.equal(
    checkThemed('const classes = "py-0.5";', `${web}components/badge.tsx`)[0]
      ?.property,
    "spacing-scale",
  );
  assert.deepEqual(
    checkThemed('const classes = "gap-1.5 p-0 mt-3 -ml-1 h-24 size-4";'),
    [],
  );
});

test("feature CSS cannot reach into primitive slots or style by ID", () => {
  // Audited: sidebar tabs and the file-name ID before migration.
  assert.equal(
    check(
      '.sidebar-tabs [data-slot="tabs-trigger"] { min-width: 0 }',
      `${web}review.css`,
    )[0]?.property,
    "slot-selector",
  );
  assert.equal(
    check("#repo-path { overflow: hidden }", `${web}style.css`)[0]?.property,
    "id-selector",
  );
  assert.equal(
    check(":is(#server-status, .popup) svg { flex-shrink: 0 }")[0]?.property,
    "id-selector",
  );
  assert.deepEqual(check("#viewer { min-height: 0 }", `${web}style.css`), []);
  assert.deepEqual(check('a[href="#top"] { overflow: hidden }'), []);
  // A rule reports once, not per declaration.
  assert.equal(
    check("#repo-path { overflow: hidden; min-width: 0 }").length,
    1,
  );
});

const button = 'import { Button } from "@diffx/ui/components/ui/button";';

test("feature classNames on components arrange them, never restyle them", () => {
  for (const className of [
    "h-9",
    "text-error",
    "rounded-control",
    "px-3",
    "hover:bg-hover",
    "border-separator",
  ])
    assert.equal(
      check(
        `${button} const View = () => <Button className={cn("ml-auto", ${JSON.stringify(className)})} />;`,
        "View.tsx",
      )[0]?.property,
      "component-class",
      className,
    );
  assert.deepEqual(
    check(
      `${button} const View = () => <Button className="ml-auto min-w-0 flex-1 max-md:hidden w-full text-left" />;`,
      "View.tsx",
    ),
    [],
  );
  // Plain elements and the design-system layer itself are out of scope.
  assert.deepEqual(
    check(`const View = () => <p className="text-error" />;`, "View.tsx"),
    [],
  );
});

test("feature CSS cannot restyle a component through a hook class", () => {
  // Audited: DiffWorkspace's Reviewed toggle, colored by .review-button.
  const usage = {
    path: `${web}DiffWorkspace.tsx`,
    text: `${button} const View = () => <Button className="review-button" />;`,
  };
  const styled = checkDesign([
    usage,
    { path: `${web}style.css`, text: ".review-button { color: var(--muted) }" },
  ]);
  assert.equal(styled[0]?.property, "component-style");
  assert.deepEqual(
    checkDesign([
      usage,
      {
        path: `${web}style.css`,
        text: ".review-button { margin-left: auto } .review-button svg { flex-shrink: 0 }",
      },
    ]),
    [],
  );
  assert.deepEqual(
    checkDesign([
      {
        path: "View.tsx",
        text: 'const View = () => <div className="review-button" />;',
      },
      {
        path: `${web}style.css`,
        text: ".review-button { color: var(--muted) }",
      },
    ]),
    [],
  );
});

test("feature files render primitives instead of raw form controls", () => {
  // Audited: ProjectsNav's raw repo-row button before migration.
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const config: unknown = JSON.parse(
    readFileSync(join(root, ".oxlintrc.json"), "utf8"),
  );
  const directory = mkdtempSync(join(tmpdir(), "diffx-elements-"));
  try {
    writeFileSync(join(directory, ".oxlintrc.json"), JSON.stringify(config));
    const control =
      'export const Row = () => <button type="button" className="project-row" />;\n';
    for (const file of [
      "apps/web/src/ProjectsNav.tsx",
      "apps/web/src/components/row.tsx",
      "apps/console/src/Members.tsx",
      "packages/ui/src/gallery/Gallery.tsx",
      "packages/ui/src/components/ui/button.tsx",
    ]) {
      mkdirSync(join(directory, file, ".."), { recursive: true });
      writeFileSync(join(directory, file), control);
    }
    const result = spawnSync(
      join(root, "node_modules/.bin/oxlint"),
      ["-c", ".oxlintrc.json", "--format", "unix", "apps", "packages"],
      { cwd: directory, encoding: "utf8" },
    );
    const output = result.stdout + result.stderr;
    assert.equal(result.status, 1, output);
    for (const name of ["ProjectsNav", "row", "Members", "Gallery"])
      assert.match(output, new RegExp(`${name}\\.tsx.*forbid-elements`));
    assert.doesNotMatch(output, /button\.tsx.*forbid-elements/);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("current frontend satisfies the guard", () => {
  const root = fileURLToPath(new URL("../../", import.meta.url));
  assert.deepEqual(checkDesign(readFrontendSources(root)), []);
});
