import type {
  LineDiffTypes,
  SupportedLanguages,
  ThemesType,
} from "@pierre/diffs";
import { themeCode, type ThemePreference } from "./theme.ts";

export type DiffTheme =
  | "match"
  | "pierre"
  | "github"
  | "vitesse"
  | "catppuccin";

export const CODE_THEME_STORAGE_KEY = "code-theme";
const LEGACY_CODE_THEME_STORAGE_KEY = "diff-theme";

export const DIFF_THEMES: readonly {
  value: DiffTheme;
  label: string;
  /** Omitted when the code theme follows the app theme. */
  themes?: ThemesType;
}[] = [
  { value: "match", label: "Match app theme" },
  {
    value: "pierre",
    label: "Pierre",
    themes: { light: "pierre-light", dark: "pierre-dark" },
  },
  {
    value: "github",
    label: "GitHub",
    themes: { light: "github-light", dark: "github-dark" },
  },
  {
    value: "vitesse",
    label: "Vitesse",
    themes: { light: "vitesse-light", dark: "vitesse-dark" },
  },
  {
    value: "catppuccin",
    label: "Catppuccin",
    themes: { light: "catppuccin-latte", dark: "catppuccin-mocha" },
  },
];

export const LINE_DIFF_TYPES: readonly {
  value: LineDiffTypes;
  label: string;
}[] = [
  { value: "word-alt", label: "Words" },
  { value: "word", label: "Strict words" },
  { value: "char", label: "Characters" },
  { value: "none", label: "Off" },
];

export function readDiffTheme(value: string | null): DiffTheme {
  switch (value) {
    case "pierre":
    case "github":
    case "vitesse":
    case "catppuccin":
      return value;
    default:
      return "match";
  }
}

/**
 * Reads the saved code theme. The legacy key always saved its Pierre default,
 * which Diffx themes match, so only its other choices carry over as overrides.
 */
export function savedDiffTheme(
  read: (key: string) => string | null,
): DiffTheme {
  const current = read(CODE_THEME_STORAGE_KEY);
  if (current !== null) return readDiffTheme(current);
  const legacy = read(LEGACY_CODE_THEME_STORAGE_KEY);
  return legacy === "pierre" ? "match" : readDiffTheme(legacy);
}

export function readLineDiffType(value: string | null): LineDiffTypes {
  switch (value) {
    case "word":
    case "char":
    case "none":
      return value;
    default:
      return "word-alt";
  }
}

export function themesFor(
  value: DiffTheme,
  appTheme: ThemePreference,
): ThemesType {
  return (
    DIFF_THEMES.find((option) => option.value === value)?.themes ??
    themeCode(appTheme)
  );
}

export function languageOverride(
  path: string,
  firstLine?: string,
): SupportedLanguages | undefined {
  const name = path.split("/").at(-1) ?? path;
  if (/^Dockerfile(?:[.-].+)?$/i.test(name)) return "dockerfile";
  if (/^\.env(?:[.-].+)?$/i.test(name)) return "dotenv";
  if (/^Justfile(?:[.-].+)?$/i.test(name)) return "just";
  if (/^Procfile(?:[.-].+)?$/i.test(name)) return "shellscript";
  if (!firstLine?.startsWith("#!")) return undefined;
  if (/\b(?:python|python3)\b/.test(firstLine)) return "python";
  if (/\bruby\b/.test(firstLine)) return "ruby";
  if (/\bnode\b/.test(firstLine)) return "javascript";
  if (/\bfish\b/.test(firstLine)) return "fish";
  if (/\b(?:sh|bash|dash|zsh)\b/.test(firstLine)) return "shellscript";
  return undefined;
}
