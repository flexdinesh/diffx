import type { ApiRepositoryDiff } from "@diffx/api";
export type DiffMode = ApiRepositoryDiff["mode"];

export function isDiffMode(value: unknown): value is DiffMode {
  return value === "all" || value === "staged" || value === "unstaged";
}
