import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, renderHook } from "@testing-library/react";
import {
  useBrowserActivation,
  type ApplicationPlatform,
} from "../../src/use-browser-activation.ts";

afterEach(cleanup);

test("only remote browser applications activate PWA and refresh push", () => {
  let permissionReads = 0;
  const notification = {
    get permission() {
      permissionReads++;
      return "default";
    },
  };
  Object.defineProperty(window, "Notification", {
    configurable: true,
    value: notification,
  });
  Object.defineProperty(globalThis, "Notification", {
    configurable: true,
    value: notification,
  });
  const manifest = () => document.querySelector('link[rel="manifest"]');
  const inactive: { platform: ApplicationPlatform; remote: boolean }[] = [
    { platform: "desktop", remote: false },
    { platform: "desktop", remote: true },
    { platform: "browser", remote: false },
  ];
  try {
    for (const { platform, remote } of inactive) {
      const { unmount } = renderHook(() =>
        useBrowserActivation(platform, remote, "owner"),
      );
      assert.equal(manifest(), null);
      assert.equal(permissionReads, 0);
      unmount();
    }
    const initialProps: { userId: string | null } = { userId: null };
    const { rerender } = renderHook(
      ({ userId }: { userId: string | null }) =>
        useBrowserActivation("browser", true, userId),
      { initialProps },
    );
    assert.equal(manifest()?.getAttribute("href"), "/manifest.webmanifest");
    assert.equal(permissionReads, 0);
    rerender({ userId: "owner" });
    assert.equal(permissionReads, 1);
    assert.equal(document.querySelectorAll('link[rel="manifest"]').length, 1);
  } finally {
    Reflect.deleteProperty(window, "Notification");
    Reflect.deleteProperty(globalThis, "Notification");
  }
});
