import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "@babel/parser";

export interface DesignSource {
  path: string;
  text: string;
}

export interface DesignViolation {
  path: string;
  line: number;
  property: string;
  value: string;
}

const colors = new Set(
  "aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen".split(
    " ",
  ),
);
const typeProperty =
  /^(?:(?:font(?:-family|-size|-weight)?|line-height|letter-spacing)$|--(?:font|text|weight|leading|tracking|icon)-)/;
const spacingProperty =
  /^(?:(?:padding|margin)(?:-(?:top|right|bottom|left|inline|block)(?:-start|-end)?)?|(?:row-|column-)?gap)$/;
const radiusProperty = /^border(?:-[\w-]+)?-radius$/;
const colorProperty =
  /(?:color|background|border|outline|shadow|fill|stroke)|^--/;

// Exact optical/renderer allowances, not file-wide exclusions. DESIGN.md owns
// the reasons; token rules still apply to every other declaration in these files.
const exceptions = new Set([
  "review.css|.file-reviewed-check|border-radius|50%",
  "review.css|.file-icon|line-height|1",
  "review.css|.monogram-icon text|font|700 9px ui-monospace, monospace",
  "style.css|#viewer|--diff-gutter-divider-width|2px",
  // Tailwind resolves breakpoints at build time, so they cannot reference
  // tokens; they mirror the 768px and 1012px queries.
  "style.css|@theme|--breakpoint-md|48rem",
  "style.css|@theme|--breakpoint-lg|63.25rem",
]);

function normalize(value: string): string {
  return value
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\s*!important$/i, "")
    .replace(/\(\s+/g, "(")
    .replace(/\s+\)/g, ")");
}

function withoutVariables(value: string): string {
  // Keep fallbacks visible: var(--role, red) must still fail the palette rule.
  return value.replace(/var\(\s*--[\w-]+\s*(?:,\s*([^()]*))?\)/gi, "$1");
}

function invalidValue(
  path: string,
  selector: string,
  property: string,
  raw: string,
): boolean {
  const value = normalize(raw);
  if (exceptions.has(`${path}|${selector}|${property}|${value}`)) return false;
  const paletteOwner = path === "tokens.css" && property.startsWith("--");
  const typeOwner = path === "typography.css";
  const remainder = withoutVariables(value).replace(
    /url\([^)]*\)|"[^"\\]*(?:\\.[^"\\]*)*"|'[^'\\]*(?:\\.[^'\\]*)*'/gi,
    "",
  );
  const literalColor =
    /#[\da-f]{3,8}\b|\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/i.test(
      remainder,
    );
  const namedColor =
    colorProperty.test(property) &&
    (remainder.toLowerCase().match(/[a-z]+/g) ?? []).some((word) =>
      colors.has(word),
    );
  // Pierre mixes black into its own addition/deletion backgrounds for comments.
  const pierreMix =
    path === "DiffWorkspace.tsx" &&
    property === "--diffs-line-bg" &&
    /^color-mix\(in srgb, var\(--diffs-computed-diff-line-bg\) 76%, color-mix\(in srgb, var\(--diffs-(?:addition|deletion)-base\) 72%, black\)\)$/.test(
      value,
    );
  if (!paletteOwner && (literalColor || (namedColor && !pierreMix)))
    return true;
  if (
    property.startsWith("--") &&
    !paletteOwner &&
    !typeOwner &&
    /(?:\d*\.)?\d+(?:px|rem|em|%|vh|vw|dvh|dvw)/.test(remainder)
  ) {
    const dimensions =
      remainder.match(/(?:\d*\.)?\d+(?:px|rem|em|%|vh|vw|dvh|dvw)/g) ?? [];
    if (
      dimensions.some(
        (dimension) => !/^0(?:px|rem|em|%|vh|vw|dvh|dvw)$/.test(dimension),
      ) &&
      !property.startsWith("--diffs-line-bg")
    )
      return true;
  }
  if (
    typeProperty.test(property) &&
    !typeOwner &&
    !(paletteOwner && literalColor)
  ) {
    if (/^(?:inherit|initial|unset|revert|revert-layer)$/.test(value))
      return false;
    if (!value.includes("var(")) return true;
    if (/\b\d/.test(remainder)) return true;
    if (/[a-z]/i.test(remainder.replace(/\b(?:calc|min|max|clamp)\b/g, "")))
      return true;
  }
  if (
    radiusProperty.test(property) &&
    !/^(?:0(?:px|rem|em|%)?|none|inherit|initial|unset)$/.test(value)
  ) {
    if (
      !value.includes("var(") ||
      /(?:\d*\.)?\d+(?:px|rem|em|%)/.test(remainder)
    )
      return true;
  }
  if (
    /^(?:box|text)-shadow$/.test(property) &&
    !/^(?:none|inherit|initial|unset|var\(--[\w-]+\))$/.test(value)
  ) {
    // These inset strokes carry header separators/navigation feedback, no elevation.
    if (
      !(
        path === "DiffWorkspace.tsx" &&
        property === "box-shadow" &&
        /^(?:inset 0 -1px var\(--border\)|inset 3px 0 var\(--accent\)(?:, inset 0 -1px var\(--border\))?|inset 0 0 transparent(?:, inset 0 -1px var\(--border\))?)$/.test(
          value,
        )
      )
    )
      return true;
  }
  if (spacingProperty.test(property) && !paletteOwner) {
    // Zero, auto, calc multipliers and 1px optical insets are allowed; layout
    // lengths use tokens. The overlapping resizer has an exact allowance above.
    const lengths =
      remainder.match(/-?(?:\d*\.)?\d+(?:px|rem|em|%|vh|vw|dvh|dvw)/g) ?? [];
    if (
      lengths.some(
        (length) => !/^-?(?:0(?:px|rem|em|%|vh|vw|dvh|dvw)|1px)$/.test(length),
      )
    )
      return true;
    if (/^[-+]?\d+(?:\.\d+)?$/.test(value) && Number(value) !== 0) return true;
  }
  return false;
}

// Lex declarations rather than grep lines: comments, strings, nested rules,
// multiline functions and semicolons inside URLs do not split declarations.
function withoutComments(text: string): string {
  let output = "";
  let quote = "";
  for (let index = 0; index < text.length; index++) {
    const character = text[index] ?? "";
    if (quote) {
      output += character;
      if (character === "\\") output += text[++index] ?? "";
      else if (character === quote) quote = "";
    } else if (character === '"' || character === "'") {
      quote = character;
      output += character;
    } else if (character === "/" && text[index + 1] === "*") {
      const end = text.indexOf("*/", index + 2);
      const finish = end < 0 ? text.length : end + 2;
      output += text.slice(index, finish).replace(/[^\n]/g, " ");
      index = finish - 1;
    } else output += character;
  }
  return output;
}

function cssDeclarations(
  text: string,
  visit: (
    selector: string,
    property: string,
    value: string,
    offset: number,
  ) => void,
): void {
  const clean = withoutComments(text);
  const selectors: string[] = [];
  let start = 0;
  let parentheses = 0;
  let quote = "";
  function declaration(end: number) {
    const segment = clean.slice(start, end);
    const match = /^\s*([\w-]+)\s*:\s*([\s\S]*)$/.exec(segment);
    if (match?.[1] && match[2] !== undefined) {
      visit(
        selectors.at(-1) ?? "",
        match[1].toLowerCase(),
        match[2],
        start + segment.search(/\S/),
      );
    }
    const apply = /^\s*@apply\s+([\s\S]+)$/.exec(segment);
    if (apply?.[1])
      visit(
        selectors.at(-1) ?? "",
        "@apply",
        apply[1],
        start + segment.search(/\S/),
      );
  }
  for (let index = 0; index < clean.length; index++) {
    const character = clean[index];
    if (quote) {
      if (character === "\\") index++;
      else if (character === quote) quote = "";
      continue;
    }
    if (character === '"' || character === "'") {
      quote = character;
      continue;
    }
    if (character === "(") parentheses++;
    if (character === ")") parentheses--;
    if (parentheses !== 0) continue;
    if (character === "{") {
      selectors.push(normalize(clean.slice(start, index)));
      start = index + 1;
    } else if (character === ";") {
      declaration(index);
      start = index + 1;
    } else if (character === "}") {
      declaration(index);
      selectors.pop();
      start = index + 1;
    }
  }
}

function utilityTokens(text: string): string[] {
  return text.split(/\s+/);
}

function baseUtility(token: string): string {
  let depth = 0;
  let lastVariant = -1;
  for (let index = 0; index < token.length; index++) {
    if (token[index] === "[" || token[index] === "(") depth++;
    if (token[index] === "]" || token[index] === ")") depth--;
    if (token[index] === ":" && depth === 0) lastVariant = index;
  }
  return token.slice(lastVariant + 1).replace(/^!|!$/g, "");
}

type Namespace =
  | "color"
  | "spacing"
  | "text"
  | "font"
  | "leading"
  | "tracking"
  | "rounded"
  | "shadow";

interface Theme {
  /** Registered type, radius and shadow utilities, e.g. `text-sm`. */
  utilities: Set<string>;
  /** Token variable to the utility name that already renders it, per namespace. */
  aliases: Map<string, Map<Namespace, string>>;
  /** Spacing multipliers backed by `--space-*` tokens, e.g. `1.5`. */
  spacing: Set<string>;
}

// Utilities registered by style.css's theme bridge resolve to canonical tokens.
function readTheme(sources: readonly DesignSource[]): Theme {
  const theme: Theme = {
    utilities: new Set(),
    aliases: new Map(),
    spacing: new Set(),
  };
  const alias = (variable: string, namespace: Namespace, name: string) => {
    const names = theme.aliases.get(variable) ?? new Map<Namespace, string>();
    names.set(namespace, name);
    theme.aliases.set(variable, names);
  };
  const prefixes: ReadonlyArray<readonly [RegExp, Namespace, string]> = [
    [/^--text-([\w-]+)$/, "text", "text-"],
    [/^--font-weight-([\w-]+)$/, "font", "font-"],
    [/^--font-([\w-]+)$/, "font", "font-"],
    [/^--leading-([\w-]+)$/, "leading", "leading-"],
    [/^--tracking-([\w-]+)$/, "tracking", "tracking-"],
    [/^--radius-([\w-]+)$/, "rounded", "rounded-"],
    [/^--shadow-([\w-]+)$/, "shadow", "shadow-"],
    [/^--color-([\w-]+)$/, "color", ""],
    [/^--spacing-([\w-]+)$/, "spacing", ""],
  ];
  for (const source of sources) {
    if (source.path === "tokens.css")
      cssDeclarations(source.text, (_selector, property) => {
        const step = /^--space-(\d+)(-5)?$/.exec(property);
        if (!step?.[1]) return;
        const name = step[1] + (step[2] ? ".5" : "");
        theme.spacing.add(name);
        alias(property, "spacing", name);
      });
    if (source.path !== "style.css") continue;
    cssDeclarations(source.text, (selector, property, value) => {
      if (!selector.startsWith("@theme") || value.trim() === "initial") return;
      const variable = /^var\((--[\w-]+)\)$/.exec(normalize(value))?.[1];
      for (const [pattern, namespace, prefix] of prefixes) {
        const name = pattern.exec(property)?.[1];
        if (name && !name.includes("--")) {
          if (prefix) theme.utilities.add(prefix + name);
          if (variable) alias(variable, namespace, name);
          break;
        }
      }
    });
  }
  return theme;
}

// Primitives and composites own visuals; feature files compose them.
function designSystemFile(path: string): boolean {
  return path.startsWith("components/");
}

const colorUtility =
  /^(?:bg|text|border(?:-[trblxyse])?|outline|ring|fill|stroke|divide|accent|caret|decoration|placeholder)$/;
const spacingUtility =
  /^(?:[pm][xytrblse]?|gap(?:-[xy])?|space-[xy]|inset(?:-[xy])?|top|right|bottom|left|start|end|w|h|size|min-[wh]|max-[wh]|basis|translate-[xy]|scroll-[mp][xytrblse]?|indent)$/;
const rhythmUtility =
  /^-?([pm][xytrblse]?|gap(?:-[xy])?|space-[xy]|inset(?:-[xy])?|top|right|bottom|left|start|end|translate-[xy]|scroll-[mp][xytrblse]?)-(\d+(?:\.\d+)?)$/;

/** The registered utility to use instead of an arbitrary token variable. */
function aliasFor(token: string, theme: Theme): string | undefined {
  const match =
    /^(-?)([a-z]+(?:-[a-z]+)*?)-(?:\[(?:(length|color|number|family-name):)?var\((--[\w-]+)\)\]|\((?:(length|color|number|family-name):)?(--[\w-]+)\))$/.exec(
      token,
    );
  const prefix = match?.[2];
  const variable = match?.[4] ?? match?.[6];
  if (!match || !prefix || !variable) return undefined;
  const hint = match[3] ?? match[5];
  const names = theme.aliases.get(variable);
  if (!names) return undefined;
  const namespaces: Namespace[] =
    prefix === "text"
      ? hint === "length"
        ? ["text"]
        : ["color", "text"]
      : colorUtility.test(prefix)
        ? ["color"]
        : spacingUtility.test(prefix)
          ? ["spacing"]
          : prefix === "font"
            ? ["font"]
            : prefix === "leading" ||
                prefix === "tracking" ||
                prefix === "shadow"
              ? [prefix]
              : /^rounded(?:-[trblse]{1,2})?$/.test(prefix)
                ? ["rounded"]
                : [];
  for (const namespace of namespaces) {
    const name = names.get(namespace);
    if (name) return `${match[1]}${prefix}-${name}`;
  }
  return undefined;
}

function utilityInvalid(
  path: string,
  original: string,
  context = "",
  registered: ReadonlySet<string> = new Set(),
): boolean {
  const token = baseUtility(original);
  const property = /^\[([\w-]+):(.+)\]$/.exec(token);
  if (property?.[1] && property[2])
    return invalidValue(
      path,
      "",
      property[1],
      property[2].replaceAll("_", " "),
    );
  const arbitrary =
    /^(rounded(?:-[trblse]{1,2})?|font|text|leading|tracking|shadow|bg|border|outline|ring|fill|stroke|p[xytrblse]?|m[xytrblse]?|gap(?:-[xy])?)-\[(.+)\]$/.exec(
      token,
    );
  if (arbitrary?.[1] && arbitrary[2]) {
    const prefix = arbitrary[1];
    const value = arbitrary[2]
      .replace(/^(?:length|color):/, "")
      .replaceAll("_", " ");
    const cssProperty = prefix.startsWith("rounded")
      ? "border-radius"
      : prefix === "font"
        ? "font"
        : prefix === "leading"
          ? "line-height"
          : prefix === "tracking"
            ? "letter-spacing"
            : prefix === "shadow"
              ? "box-shadow"
              : /^(?:p|m|gap)/.test(prefix)
                ? "padding"
                : prefix === "text" &&
                    (arbitrary[2].startsWith("length:") ||
                      /\d(?:px|rem|em)/.test(value))
                  ? "font-size"
                  : "color";
    return invalidValue(path, "", cssProperty, value);
  }
  if (
    /^(?:bg|text|border|outline|ring|fill|stroke|shadow)-(?:black|white|(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d+)(?:\/.*)?$/.test(
      token,
    )
  )
    return true;
  if (
    /^text-(?:xs|sm|base|lg|xl|\d+xl)$|^font-(?:thin|extralight|light|normal|medium|semibold|bold|extrabold|black|sans|serif|mono)$|^leading-(?:none|tight|snug|normal|relaxed|loose|\d+)$|^tracking-(?:tighter|tight|normal|wide|wider|widest)$|^rounded(?:-[trblse]{1,2})?(?:-(?:xs|sm|md|lg|xl|\d+xl|full))?$|^(?:text-)?shadow(?:-(?:xs|sm|md|lg|xl|\d+xl))?$/.test(
      token,
    )
  ) {
    const rounded = /^rounded(?:-[trblse]{1,2})?-(.+)$/.exec(token);
    return !registered.has(rounded?.[1] ? `rounded-${rounded[1]}` : token);
  }
  return false;
}

/** The rule a utility breaks, as a violation property, if any. */
function utilityRule(
  path: string,
  original: string,
  context: string,
  theme: Theme,
): { property: string; value: string } | undefined {
  if (utilityInvalid(path, original, context, theme.utilities))
    return { property: "utility", value: original };
  const token = baseUtility(original);
  const alias = aliasFor(token, theme);
  if (alias)
    return { property: "token-utility", value: `${original} → ${alias}` };
  const step = rhythmUtility.exec(token)?.[2];
  // 2px steps are an optical detail of primitives and composites.
  if (
    step &&
    theme.spacing.size &&
    step !== "0" &&
    (!theme.spacing.has(step) || (step === "0.5" && !designSystemFile(path)))
  )
    return { property: "spacing-scale", value: original };
  return undefined;
}

function isRecord(node: unknown): node is Record<string, unknown> {
  return typeof node === "object" && node !== null;
}

/** Literal class fragments a className expression can produce. */
function classFragments(node: unknown): string[] {
  if (Array.isArray(node)) return node.flatMap(classFragments);
  if (!isRecord(node)) return [];
  if (node.type === "StringLiteral" && typeof node.value === "string")
    return [node.value];
  if (
    node.type === "TemplateElement" &&
    isRecord(node.value) &&
    typeof node.value.cooked === "string"
  )
    return [node.value.cooked];
  return Object.entries(node).flatMap(([key, value]) =>
    key === "loc" || key.endsWith("Comments") ? [] : classFragments(value),
  );
}

interface ComponentClass {
  path: string;
  offset: number;
  token: string;
}

// Classes feature files pass to primitives and composites. Utilities there may
// only arrange the component; any other class is a hook feature CSS could
// restyle it through.
function componentClasses(sources: readonly DesignSource[]): ComponentClass[] {
  const classes: ComponentClass[] = [];
  for (const source of sources) {
    if (!source.path.endsWith(".tsx") || designSystemFile(source.path))
      continue;
    const ast = parse(source.text, {
      sourceType: "module",
      plugins: ["typescript", "jsx"],
    });
    const components = new Set<string>();
    const body = isRecord(ast.program) ? ast.program.body : [];
    for (const statement of Array.isArray(body) ? body : []) {
      if (
        !isRecord(statement) ||
        statement.type !== "ImportDeclaration" ||
        !isRecord(statement.source) ||
        typeof statement.source.value !== "string" ||
        !/(?:^@\/|^\.{1,2}\/|\/)components\//.test(statement.source.value) ||
        !Array.isArray(statement.specifiers)
      )
        continue;
      for (const specifier of statement.specifiers)
        if (
          isRecord(specifier) &&
          isRecord(specifier.local) &&
          typeof specifier.local.name === "string"
        )
          components.add(specifier.local.name);
    }
    function elementName(node: unknown): string | undefined {
      if (!isRecord(node)) return undefined;
      if (node.type === "JSXIdentifier" && typeof node.name === "string")
        return node.name;
      if (node.type === "JSXMemberExpression") return elementName(node.object);
      return undefined;
    }
    function visit(node: unknown) {
      if (Array.isArray(node)) {
        for (const item of node) visit(item);
        return;
      }
      if (!isRecord(node)) return;
      const name =
        node.type === "JSXOpeningElement" ? elementName(node.name) : undefined;
      if (name && components.has(name) && Array.isArray(node.attributes))
        for (const attribute of node.attributes) {
          if (
            !isRecord(attribute) ||
            attribute.type !== "JSXAttribute" ||
            !isRecord(attribute.name) ||
            attribute.name.name !== "className"
          )
            continue;
          const offset =
            typeof attribute.start === "number" ? attribute.start : 0;
          for (const fragment of classFragments(attribute.value))
            for (const token of utilityTokens(fragment))
              if (token) classes.push({ path: source.path, offset, token });
        }
      for (const [key, value] of Object.entries(node))
        if (key !== "loc" && !key.endsWith("Comments")) visit(value);
    }
    visit(ast);
  }
  return classes;
}

// Color, border, radius, height, padding, type and state belong to variants.
const visualUtility =
  /^-?(?:bg|text|font|leading|tracking|rounded|border|divide|shadow|ring|outline|fill|stroke|opacity|[p][xytrblse]?|h|min-h|max-h|size|underline|decoration|placeholder|caret|accent|animate|transition|duration|ease)(?:-|$)/;
const layoutUtility =
  /^(?:text-(?:left|center|right|start|end|wrap|nowrap|balance|pretty|ellipsis|clip)|h-(?:full|auto|fit)|min-h-0|max-h-(?:full|none)|size-full)$/;
const visualProperty =
  /^(?:color|background(?:-[\w-]+)?|border(?:-[\w-]+)?|padding(?:-[\w-]+)?|height|min-height|max-height|font(?:-[\w-]+)?|line-height|letter-spacing|box-shadow|outline(?:-[\w-]+)?|text-decoration(?:-[\w-]+)?|text-transform|fill|stroke(?:-[\w-]+)?|opacity)$/;

// IDs that are real layout anchors rather than component styling hooks.
const idAnchors = new Set(["viewer"]);

function withoutStrings(selector: string): string {
  return selector.replace(/"[^"]*"|'[^']*'/g, '""');
}

/** The compound selector an element must match, after the last combinator. */
function subjectCompound(selector: string): string {
  let depth = 0;
  let start = 0;
  for (let index = 0; index < selector.length; index++) {
    const character = selector[index];
    if (character === "(" || character === "[") depth++;
    if (character === ")" || character === "]") depth--;
    if (depth === 0 && /[\s>+~]/.test(character ?? "")) start = index + 1;
  }
  return selector.slice(start);
}

function complexSelectors(selector: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < selector.length; index++) {
    const character = selector[index];
    if (character === "(" || character === "[") depth++;
    if (character === ")" || character === "]") depth--;
    if (depth === 0 && character === ",") {
      parts.push(selector.slice(start, index).trim());
      start = index + 1;
    }
  }
  parts.push(selector.slice(start).trim());
  return parts;
}

/**
 * Classes the subject element must carry. :is() and :where() arguments are
 * alternative subjects; :not() and :has() arguments describe other elements.
 */
function subjectClasses(complex: string): string[] {
  const subject = subjectCompound(complex).replace(/\[[^\]]*\]/g, "");
  const classes: string[] = [];
  let own = "";
  for (let index = 0; index < subject.length; index++) {
    const pseudo = /^:(is|where|not|has)\(/.exec(subject.slice(index));
    if (!pseudo) {
      own += subject[index];
      continue;
    }
    const open = index + pseudo[0].length;
    let depth = 1;
    let end = open;
    for (; end < subject.length && depth > 0; end++) {
      if (subject[end] === "(") depth++;
      if (subject[end] === ")") depth--;
    }
    if (pseudo[1] === "is" || pseudo[1] === "where")
      for (const inner of complexSelectors(subject.slice(open, end - 1)))
        classes.push(...subjectClasses(inner));
    index = end - 1;
  }
  for (const name of own.match(/\.-?[_a-z][\w-]*/gi) ?? [])
    classes.push(name.slice(1));
  return classes;
}

export function checkDesign(
  sources: readonly DesignSource[],
): DesignViolation[] {
  const violations: DesignViolation[] = [];
  const theme = readTheme(sources);
  const passed = componentClasses(sources);
  const hooks = new Set<string>();
  for (const { path, offset, token } of passed) {
    const utility = baseUtility(token);
    if (visualUtility.test(utility) && !layoutUtility.test(utility)) {
      const source = sources.find((candidate) => candidate.path === path);
      violations.push({
        path,
        line: (source?.text ?? "").slice(0, offset).split("\n").length,
        property: "component-class",
        value: token,
      });
    } else hooks.add(token);
  }
  for (const source of sources) {
    const add = (property: string, value: string, offset: number) =>
      violations.push({
        path: source.path,
        line: source.text.slice(0, offset).split("\n").length,
        property,
        value: normalize(value),
      });
    const reported = new Set<string>();
    const css = (text: string, offset: number) =>
      cssDeclarations(text, (selector, property, value, at) => {
        if (property === "@apply") {
          for (const token of utilityTokens(value)) {
            const broken = utilityRule(source.path, token, value, theme);
            if (broken) add(broken.property, broken.value, offset + at);
          }
        } else if (invalidValue(source.path, selector, property, value))
          add(property, value, offset + at);
        if (!source.path.endsWith(".css") || selector.startsWith("@")) return;
        const plain = withoutStrings(selector);
        const once = (rule: string) => {
          if (reported.has(rule + selector)) return;
          reported.add(rule + selector);
          add(rule, selector, offset + at);
        };
        // Primitives own their slots; feature CSS cannot reach into them.
        if (plain.includes("[data-slot")) once("slot-selector");
        const ids = plain.replace(/\[[^\]]*\]/g, "").match(/#-?[_a-z][\w-]*/gi);
        if (ids?.some((id) => !idAnchors.has(id.slice(1)))) once("id-selector");
        if (
          visualProperty.test(property) &&
          complexSelectors(plain).some((complex) =>
            subjectClasses(complex).some((name) => hooks.has(name)),
          )
        )
          add("component-style", `${selector} { ${property} }`, offset + at);
      });
    if (source.path.endsWith(".css")) {
      css(source.text, 0);
      continue;
    }
    const ast = parse(source.text, {
      sourceType: "module",
      plugins: ["typescript", "jsx"],
    });
    function record(node: unknown): node is Record<string, unknown> {
      return typeof node === "object" && node !== null;
    }
    function literal(node: unknown): string | undefined {
      if (!record(node)) return undefined;
      if (node.type === "StringLiteral" && typeof node.value === "string")
        return node.value;
      if (node.type === "NumericLiteral" && typeof node.value === "number")
        return String(node.value);
      if (node.type === "TemplateLiteral" && Array.isArray(node.quasis)) {
        const parts: string[] = [];
        for (const quasi of node.quasis) {
          if (
            !record(quasi) ||
            !record(quasi.value) ||
            typeof quasi.value.cooked !== "string"
          )
            return undefined;
          parts.push(quasi.value.cooked);
        }
        return parts.join("0");
      }
      return undefined;
    }
    function interpolated(node: unknown): boolean {
      return (
        record(node) &&
        node.type === "TemplateLiteral" &&
        Array.isArray(node.expressions) &&
        node.expressions.length > 0
      );
    }
    const styleNames = new Set<string>();
    const classNames = new Set<string>();
    const declarations = new Map<string, unknown>();
    function collectIdentifiers(node: unknown, names: Set<string>) {
      if (Array.isArray(node)) {
        for (const item of node) collectIdentifiers(item, names);
        return;
      }
      if (!record(node)) return;
      if (node.type === "Identifier" && typeof node.name === "string")
        names.add(node.name);
      for (const [key, value] of Object.entries(node))
        if (key !== "loc" && key !== "comments")
          collectIdentifiers(value, names);
    }
    function utilityCall(node: Record<string, unknown>): boolean {
      return (
        node.type === "CallExpression" &&
        record(node.callee) &&
        typeof node.callee.name === "string" &&
        /^(?:cn|cva|clsx|classnames)$/.test(node.callee.name)
      );
    }
    function collect(node: unknown) {
      if (Array.isArray(node)) {
        for (const item of node) collect(item);
        return;
      }
      if (!record(node)) return;
      if (
        node.type === "JSXAttribute" &&
        record(node.name) &&
        node.name.name === "style"
      )
        collectIdentifiers(node.value, styleNames);
      if (
        node.type === "JSXAttribute" &&
        record(node.name) &&
        node.name.name === "className"
      )
        collectIdentifiers(node.value, classNames);
      if (utilityCall(node)) collectIdentifiers(node.arguments, classNames);
      if (
        node.type === "VariableDeclarator" &&
        record(node.id) &&
        typeof node.id.name === "string"
      ) {
        declarations.set(node.id.name, node.init);
        if (/style$/i.test(node.id.name)) styleNames.add(node.id.name);
        if (/class(?:es|names)?$/i.test(node.id.name))
          classNames.add(node.id.name);
        const annotation = record(node.id.typeAnnotation)
          ? node.id.typeAnnotation.typeAnnotation
          : undefined;
        if (
          record(annotation) &&
          record(annotation.typeName) &&
          (annotation.typeName.name === "CSSProperties" ||
            (record(annotation.typeName.right) &&
              annotation.typeName.right.name === "CSSProperties"))
        )
          styleNames.add(node.id.name);
      }
      for (const [key, value] of Object.entries(node))
        if (key !== "loc" && key !== "comments") collect(value);
    }
    collect(ast);
    // Resolve referenced object aliases/spreads, without evaluating JS behavior.
    for (const name of styleNames)
      collectIdentifiers(declarations.get(name), styleNames);
    for (const name of classNames)
      collectIdentifiers(declarations.get(name), classNames);
    // SVG color attributes apply to DOM elements; components own their props.
    function walk(
      node: unknown,
      styleContext = false,
      utilityContext = false,
      element = true,
    ) {
      if (Array.isArray(node)) {
        for (const item of node)
          walk(item, styleContext, utilityContext, element);
        return;
      }
      if (!record(node)) return;
      const offset = typeof node.start === "number" ? node.start : 0;
      if (node.type === "JSXOpeningElement")
        element =
          record(node.name) &&
          node.name.type === "JSXIdentifier" &&
          typeof node.name.name === "string" &&
          /^[a-z]/.test(node.name.name);
      if (
        (node.type === "JSXAttribute" &&
          record(node.name) &&
          node.name.name === "style") ||
        (node.type === "VariableDeclarator" &&
          record(node.id) &&
          typeof node.id.name === "string" &&
          styleNames.has(node.id.name))
      )
        styleContext = true;
      if (
        (node.type === "JSXAttribute" &&
          record(node.name) &&
          node.name.name === "className") ||
        utilityCall(node) ||
        (node.type === "VariableDeclarator" &&
          record(node.id) &&
          typeof node.id.name === "string" &&
          classNames.has(node.id.name))
      )
        utilityContext = true;
      if (
        element &&
        node.type === "JSXAttribute" &&
        record(node.name) &&
        typeof node.name.name === "string" &&
        /^(?:fill|stroke|color)$/.test(node.name.name)
      ) {
        const attribute =
          record(node.value) && node.value.type === "JSXExpressionContainer"
            ? node.value.expression
            : node.value;
        const value = literal(attribute);
        if (
          value === undefined ||
          interpolated(attribute) ||
          invalidValue(source.path, "", node.name.name, value)
        )
          add(node.name.name, value ?? "<dynamic>", offset);
      }
      if (node.type === "ObjectProperty") {
        const name =
          record(node.key) && typeof node.key.name === "string"
            ? node.key.name
            : literal(node.key);
        const value = literal(node.value);
        if (name === "unsafeCSS") {
          const expressions =
            record(node.value) && Array.isArray(node.value.expressions)
              ? node.value.expressions
              : [];
          if (
            value === undefined ||
            expressions.some(
              (expression: unknown) =>
                source.path !== "DiffWorkspace.tsx" ||
                !record(expression) ||
                expression.type !== "Identifier" ||
                expression.name !== "NAVIGATION_CUE_MS",
            )
          )
            add("unsafeCSS", "<dynamic>", offset);
          else
            css(
              value,
              record(node.value) && typeof node.value.start === "number"
                ? node.value.start
                : offset,
            );
        } else if (name && styleContext) {
          const property = name.replace(
            /[A-Z]/g,
            (letter) => `-${letter.toLowerCase()}`,
          );
          if (
            typeProperty.test(property) ||
            radiusProperty.test(property) ||
            spacingProperty.test(property) ||
            /(?:^|-)color$|^background(?:-image)?$|^(?:border|outline)(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?$|^(?:box|text)-shadow$|^(?:fill|stroke)$/.test(
              property,
            )
          ) {
            if (
              value === undefined ||
              (interpolated(node.value) &&
                !(
                  spacingProperty.test(property) &&
                  value.startsWith("calc(") &&
                  value.includes("var(--space-")
                )) ||
              invalidValue(source.path, "", property, value)
            )
              add(property, value ?? "<dynamic>", offset);
          }
        }
      }
      if (
        utilityContext &&
        (node.type === "StringLiteral" || node.type === "TemplateLiteral")
      ) {
        const text = literal(node) ?? "";
        for (const token of utilityTokens(text)) {
          const broken = utilityRule(source.path, token, text, theme);
          if (broken) add(broken.property, broken.value, offset);
        }
      }
      for (const [key, value] of Object.entries(node))
        if (key !== "loc" && key !== "comments")
          walk(value, styleContext, utilityContext, element);
    }
    walk(ast);
  }
  return violations;
}

export function readDesignSources(root: string): DesignSource[] {
  const sources: DesignSource[] = [];
  function walk(directory: string) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) walk(path);
      else if (/\.(css|tsx?)$/.test(path))
        sources.push({
          path: relative(root, path).split(sep).join("/"),
          text: readFileSync(path, "utf8"),
        });
    }
  }
  walk(root);
  return sources;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const root = fileURLToPath(new URL("../apps/web/src/", import.meta.url));
  const violations = checkDesign(readDesignSources(root));
  for (const violation of violations)
    console.error(
      `${violation.path}:${violation.line}: ${violation.property}: ${violation.value} — use DESIGN.md tokens`,
    );
  if (violations.length) process.exitCode = 1;
}
