import { saved } from "@diffx/ui/lib/storage";

export function removeSaved(key: string) {
  try {
    localStorage.removeItem(key);
  } catch {
    // Storage is optional.
  }
}

export function savedReviews(key: string): Map<string, string> {
  try {
    const data: unknown = JSON.parse(saved(key) ?? "[]");
    const entries = new Map<string, string>();
    if (Array.isArray(data))
      for (const entry of data) {
        if (
          Array.isArray(entry) &&
          typeof entry[0] === "string" &&
          typeof entry[1] === "string"
        )
          entries.set(entry[0], entry[1]);
      }
    return entries;
  } catch {
    return new Map();
  }
}

// Per-tab view state survives reloads but not closing the tab.
export function sessionPaths(key: string): Set<string> {
  try {
    const data: unknown = JSON.parse(sessionStorage.getItem(key) ?? "[]");
    return new Set(
      Array.isArray(data)
        ? data.filter((value) => typeof value === "string")
        : [],
    );
  } catch {
    return new Set();
  }
}

export function sessionFlag(key: string): boolean {
  try {
    return sessionStorage.getItem(key) === "true";
  } catch {
    return false;
  }
}

export function saveSessionFlag(key: string, value: boolean) {
  try {
    if (value) sessionStorage.setItem(key, "true");
    else sessionStorage.removeItem(key);
  } catch {
    // Storage is optional.
  }
}

export function saveSessionPaths(key: string, paths: ReadonlySet<string>) {
  try {
    if (paths.size) sessionStorage.setItem(key, JSON.stringify([...paths]));
    else sessionStorage.removeItem(key);
  } catch {
    // Storage is optional.
  }
}

export function sessionValue(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

export function saveSessionValue(key: string, value: string) {
  try {
    sessionStorage.setItem(key, value);
  } catch {
    // Storage is optional.
  }
}

export function removeSessionValue(key: string) {
  try {
    sessionStorage.removeItem(key);
  } catch {
    // Storage is optional.
  }
}
