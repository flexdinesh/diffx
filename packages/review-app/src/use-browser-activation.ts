import { useEffect } from "react";
import { startPwa } from "./pwa.ts";
import { syncPushDevice } from "./push.ts";

export type ApplicationPlatform = "browser" | "desktop";

/** Deployment policy belongs to the application entry point. */
export function useBrowserActivation(
  platform: ApplicationPlatform,
  remote: boolean,
  userId: string | null,
) {
  useEffect(() => {
    if (platform === "browser" && remote) startPwa();
  }, [platform, remote]);
  useEffect(() => {
    if (platform === "browser" && remote && userId)
      void syncPushDevice().catch(() => {});
  }, [platform, remote, userId]);
}
