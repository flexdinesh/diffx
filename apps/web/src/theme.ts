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
  group: "diffx" | "omarchy";
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

/** Omarchy palettes: https://github.com/basecamp/omarchy (MIT). */
export const THEMES = [
  {
    id: "system",
    label: "System",
    group: "diffx",
    mode: "system",
    code: pierre,
  },
  { id: "light", label: "Light", group: "diffx", mode: "light", code: pierre },
  { id: "dark", label: "Dark", group: "diffx", mode: "dark", code: pierre },
  {
    id: "catppuccin",
    label: "Catppuccin",
    group: "omarchy",
    mode: "dark",
    code: bundled("catppuccin-mocha"),
  },
  {
    id: "catppuccin-latte",
    label: "Catppuccin Latte",
    group: "omarchy",
    mode: "light",
    code: bundled("catppuccin-latte"),
  },
  {
    id: "ethereal",
    label: "Ethereal",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "everforest",
    label: "Everforest",
    group: "omarchy",
    mode: "dark",
    code: bundled("everforest-dark"),
  },
  {
    id: "flexoki-light",
    label: "Flexoki Light",
    group: "omarchy",
    mode: "light",
    code: palette,
  },
  {
    id: "gruvbox",
    label: "Gruvbox",
    group: "omarchy",
    mode: "dark",
    code: bundled("gruvbox-dark-medium"),
  },
  {
    id: "hackerman",
    label: "Hackerman",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "kanagawa",
    label: "Kanagawa",
    group: "omarchy",
    mode: "dark",
    code: bundled("kanagawa-wave"),
  },
  {
    id: "last-horizon",
    label: "Last Horizon",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "lumon",
    label: "Lumon",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "lupine",
    label: "Lupine",
    group: "omarchy",
    mode: "light",
    code: palette,
  },
  {
    id: "matte-black",
    label: "Matte Black",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "miasma",
    label: "Miasma",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "nord",
    label: "Nord",
    group: "omarchy",
    mode: "dark",
    code: bundled("nord"),
  },
  {
    id: "osaka-jade",
    label: "Osaka Jade",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "retro-82",
    label: "Retro 82",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "ristretto",
    label: "Ristretto",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "rose-pine",
    label: "Rosé Pine",
    group: "omarchy",
    mode: "light",
    code: bundled("rose-pine-dawn"),
  },
  {
    id: "solitude",
    label: "Solitude",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "tokyo-night",
    label: "Tokyo Night",
    group: "omarchy",
    mode: "dark",
    code: bundled("tokyo-night"),
  },
  {
    id: "vantablack",
    label: "Vantablack",
    group: "omarchy",
    mode: "dark",
    code: palette,
  },
  {
    id: "white",
    label: "White",
    group: "omarchy",
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

/** The `data-palette` id for Omarchy themes; Diffx themes use base tokens. */
export function themePalette(preference: ThemePreference): string | undefined {
  const theme = themeFor(preference);
  return theme.group === "omarchy" ? theme.id : undefined;
}

/** The swatch palette: Diffx themes preview their light/dark tokens. */
export function swatchPalette(
  preference: Exclude<ThemePreference, "system">,
): string {
  const theme = themeFor(preference);
  return theme.group === "omarchy" ? theme.id : `diffx-${theme.mode}`;
}

export function themeCode(preference: ThemePreference): ThemesType {
  return themeFor(preference).code;
}
