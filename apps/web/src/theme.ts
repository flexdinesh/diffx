import type { ThemesType } from "@pierre/diffs";

export type ResolvedTheme = "light" | "dark";

/**
 * Pierre code theme registered from CSS variables; palettes without a bundled
 * Shiki equivalent define its colors in tokens.css.
 */
export const PALETTE_CODE_THEME = "diffx-palette";

type ThemeDefinition = {
  id: string;
  label: string;
  kind: "base" | "palette";
  mode: ResolvedTheme | "system";
  /** Code theme used when the code theme matches the app theme. */
  code: ThemesType;
};

const pierre: ThemesType = { light: "pierre-light", dark: "pierre-dark" };
const palette: ThemesType = {
  light: PALETTE_CODE_THEME,
  dark: PALETTE_CODE_THEME,
};
function bundled(name: string): ThemesType {
  return { light: name, dark: name };
}

export const THEMES = [
  {
    id: "system",
    label: "System",
    kind: "base",
    mode: "system",
    code: pierre,
  },
  { id: "light", label: "Light", kind: "base", mode: "light", code: pierre },
  { id: "dark", label: "Dark", kind: "base", mode: "dark", code: pierre },
  {
    id: "catppuccin",
    label: "Catppuccin",
    kind: "palette",
    mode: "dark",
    code: bundled("catppuccin-mocha"),
  },
  {
    id: "catppuccin-latte",
    label: "Catppuccin Latte",
    kind: "palette",
    mode: "light",
    code: bundled("catppuccin-latte"),
  },
  {
    id: "ethereal",
    label: "Ethereal",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "everforest",
    label: "Everforest",
    kind: "palette",
    mode: "dark",
    code: bundled("everforest-dark"),
  },
  {
    id: "flexoki-light",
    label: "Flexoki Light",
    kind: "palette",
    mode: "light",
    code: palette,
  },
  {
    id: "gruvbox",
    label: "Gruvbox",
    kind: "palette",
    mode: "dark",
    code: bundled("gruvbox-dark-medium"),
  },
  {
    id: "hackerman",
    label: "Hackerman",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "kanagawa",
    label: "Kanagawa",
    kind: "palette",
    mode: "dark",
    code: bundled("kanagawa-wave"),
  },
  {
    id: "last-horizon",
    label: "Last Horizon",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "lumon",
    label: "Lumon",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "lupine",
    label: "Lupine",
    kind: "palette",
    mode: "light",
    code: palette,
  },
  {
    id: "matte-black",
    label: "Matte Black",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "miasma",
    label: "Miasma",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "nord",
    label: "Nord",
    kind: "palette",
    mode: "dark",
    code: bundled("nord"),
  },
  {
    id: "osaka-jade",
    label: "Osaka Jade",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "retro-82",
    label: "Retro 82",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "ristretto",
    label: "Ristretto",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "rose-pine",
    label: "Rosé Pine",
    kind: "palette",
    mode: "light",
    code: bundled("rose-pine-dawn"),
  },
  {
    id: "solitude",
    label: "Solitude",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "tokyo-night",
    label: "Tokyo Night",
    kind: "palette",
    mode: "dark",
    code: bundled("tokyo-night"),
  },
  {
    id: "vantablack",
    label: "Vantablack",
    kind: "palette",
    mode: "dark",
    code: palette,
  },
  {
    id: "white",
    label: "White",
    kind: "palette",
    mode: "light",
    code: palette,
  },
] as const satisfies readonly ThemeDefinition[];

export type ThemePreference = (typeof THEMES)[number]["id"];

export const THEME_STORAGE_KEY = "theme-v2";

export function readThemePreference(value: unknown): ThemePreference {
  return THEMES.find((theme) => theme.id === value)?.id ?? "system";
}

function themeFor(preference: ThemePreference) {
  return THEMES.find((theme) => theme.id === preference) ?? THEMES[0];
}

export function themeLabel(preference: ThemePreference): string {
  return themeFor(preference).label;
}

export function resolveTheme(
  preference: ThemePreference,
  systemDark: boolean,
): ResolvedTheme {
  const { mode } = themeFor(preference);
  if (mode !== "system") return mode;
  return systemDark ? "dark" : "light";
}

/** The `data-palette` id for palette themes; base themes use base tokens. */
export function themePalette(preference: ThemePreference): string | undefined {
  const theme = themeFor(preference);
  return theme.kind === "palette" ? theme.id : undefined;
}

/** The swatch palette: base themes preview their light/dark tokens. */
export function swatchPalette(
  preference: Exclude<ThemePreference, "system">,
): string {
  const theme = themeFor(preference);
  return theme.kind === "palette" ? theme.id : `diffx-${theme.mode}`;
}

export function themeCode(preference: ThemePreference): ThemesType {
  return themeFor(preference).code;
}
