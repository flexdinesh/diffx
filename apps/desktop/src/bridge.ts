export interface LaunchStatus {
  phase: "starting" | "ready" | "failed" | "folder";
  generation: number;
  error: string;
  owned: boolean;
  canReplace: boolean;
}

type Method =
  | "Desktop.Status"
  | "Desktop.Retry"
  | "Desktop.Folder"
  | "Desktop.Replace";

declare global {
  interface Window {
    mygo?: { call(method: Method): Promise<unknown> };
  }
}

/** Matches the narrow Go Desktop service. No arbitrary method or argument API. */
export function desktopCall(method: Method): Promise<LaunchStatus> {
  if (!window.mygo)
    return Promise.reject(
      new Error("Open this application with diffx-desktop."),
    );
  return window.mygo.call(method).then(launchStatus);
}

export function launchStatus(value: unknown): LaunchStatus {
  if (
    typeof value !== "object" ||
    value === null ||
    !("phase" in value) ||
    !("generation" in value) ||
    !("error" in value) ||
    !("owned" in value) ||
    !("canReplace" in value) ||
    (value.phase !== "starting" &&
      value.phase !== "ready" &&
      value.phase !== "failed" &&
      value.phase !== "folder") ||
    typeof value.generation !== "number" ||
    !Number.isSafeInteger(value.generation) ||
    value.generation < 0 ||
    typeof value.error !== "string" ||
    typeof value.owned !== "boolean" ||
    typeof value.canReplace !== "boolean"
  )
    throw new Error(
      "Incompatible desktop bridge response. Restart the matching desktop build.",
    );
  return {
    phase: value.phase,
    generation: value.generation,
    error: value.error,
    owned: value.owned,
    canReplace: value.canReplace,
  };
}
