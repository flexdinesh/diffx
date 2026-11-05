import { api, errorDetail } from "@diffx/api";
import {
  existingServiceWorker,
  serviceWorkerReady,
  standalone,
} from "./pwa.ts";

/** What this browser offers for Web Push. */
export interface PushEnvironment {
  serviceWorker: boolean;
  pushManager: boolean;
  notification: boolean;
  /** iPhone or iPad, including iPadOS reporting a desktop Mac. */
  appleMobile: boolean;
  standalone: boolean;
}

/**
 * `ios-install`: iOS and iPadOS expose push only to Home Screen apps, so a
 * Safari tab without PushManager has to be installed first.
 */
export type PushSupport = "supported" | "ios-install" | "unsupported";

export function pushSupport(environment: PushEnvironment): PushSupport {
  if (
    environment.serviceWorker &&
    environment.pushManager &&
    environment.notification
  )
    return "supported";
  return environment.appleMobile && !environment.standalone
    ? "ios-install"
    : "unsupported";
}

export function appleMobile(userAgent: string, maxTouchPoints: number) {
  return (
    /iPhone|iPad|iPod/.test(userAgent) ||
    (/Macintosh/.test(userAgent) && maxTouchPoints > 1)
  );
}

export function pushEnvironment(): PushEnvironment {
  return {
    serviceWorker: "serviceWorker" in navigator,
    pushManager: "PushManager" in window,
    notification: "Notification" in window,
    appleMobile: appleMobile(navigator.userAgent, navigator.maxTouchPoints),
    standalone: standalone(),
  };
}

function browserName(userAgent: string) {
  if (/Edg(?:e|A|iOS)?\//.test(userAgent)) return "Edge";
  if (/OPR\/|Opera/.test(userAgent)) return "Opera";
  if (/SamsungBrowser\//.test(userAgent)) return "Samsung Internet";
  if (/Firefox\/|FxiOS\//.test(userAgent)) return "Firefox";
  if (/Chrome\/|CriOS\/|Chromium\//.test(userAgent)) return "Chrome";
  if (/Safari\//.test(userAgent)) return "Safari";
  // Home Screen apps on iOS omit Safari's own token.
  if (/AppleWebKit\//.test(userAgent) && /iPhone|iPad|iPod/.test(userAgent))
    return "Safari";
  return null;
}

function systemName(userAgent: string, maxTouchPoints: number) {
  if (/iPhone|iPod/.test(userAgent)) return "iPhone";
  if (/iPad/.test(userAgent) || appleMobile(userAgent, maxTouchPoints))
    return "iPad";
  if (/Android/.test(userAgent)) return "Android";
  if (/CrOS/.test(userAgent)) return "ChromeOS";
  if (/Windows/.test(userAgent)) return "Windows";
  if (/Macintosh|Mac OS X/.test(userAgent)) return "macOS";
  if (/Linux/.test(userAgent)) return "Linux";
  return null;
}

/** A short device description such as "Chrome on Android". */
export function deviceLabel(userAgent: string, maxTouchPoints = 0) {
  const browser = browserName(userAgent);
  const system = systemName(userAgent, maxTouchPoints);
  if (browser && system) return `${browser} on ${system}`;
  if (system) return `Browser on ${system}`;
  return browser ?? "Web browser";
}

/** Decodes the server's base64url VAPID key for `pushManager.subscribe`. */
export function applicationServerKey(publicKey: string) {
  const base64 = publicKey.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, "="));
  const key = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++)
    key[index] = binary.charCodeAt(index);
  return key;
}

function sameKey(subscription: PushSubscription, key: Uint8Array) {
  const current = subscription.options.applicationServerKey;
  if (!current) return false;
  const bytes = new Uint8Array(current);
  return (
    bytes.length === key.length &&
    bytes.every((value, index) => value === key[index])
  );
}

/** Reuses a subscription made with the server's key, replacing any other. */
async function ensureSubscription(
  registration: ServiceWorkerRegistration,
  publicKey: string,
) {
  const key = applicationServerKey(publicKey);
  const existing = await registration.pushManager.getSubscription();
  if (existing && sameKey(existing, key)) return existing;
  await existing?.unsubscribe();
  return registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: key,
  });
}

/** This browser's device, remembered so sign-out can remove it. */
let remembered: { endpoint: string; id: string } | null = null;

/** Registers or refreshes the subscription on the server (idempotent). */
async function register(subscription: PushSubscription) {
  const { endpoint, keys } = subscription.toJSON();
  const p256dh = keys?.p256dh;
  const auth = keys?.auth;
  if (!endpoint || !p256dh || !auth)
    throw new Error("This browser returned an incomplete push subscription.");
  const { data, error } = await api.PUT("/api/v2/push/subscriptions", {
    body: {
      endpoint,
      keys: { p256dh, auth },
      label: deviceLabel(navigator.userAgent, navigator.maxTouchPoints),
    },
  });
  if (!data)
    throw new Error(
      errorDetail(error, "Unable to turn on notifications. Try again."),
    );
  remembered = { endpoint, id: data.id };
  return data;
}

async function currentSubscription() {
  const registration = await existingServiceWorker();
  return (await registration?.pushManager.getSubscription()) ?? null;
}

/**
 * This browser's registered device when notifications are on: permission
 * granted and a subscription exists. Re-registers it, covering endpoint and
 * server key rotation.
 */
export async function thisDevice(publicKey: string) {
  if (Notification.permission !== "granted") return null;
  if (!(await currentSubscription())) return null;
  const subscription = await ensureSubscription(
    await serviceWorkerReady(),
    publicKey,
  );
  return register(subscription);
}

/**
 * Asks for permission (call directly from a click), subscribes and registers
 * this device. Null when permission is not granted.
 */
export async function enablePush(publicKey: string) {
  const permission = await Notification.requestPermission();
  if (permission !== "granted") return null;
  const subscription = await ensureSubscription(
    await serviceWorkerReady(),
    publicKey,
  );
  return register(subscription);
}

/** Drops this browser's subscription after the server forgot the device. */
export async function discardSubscription() {
  remembered = null;
  await (await currentSubscription())?.unsubscribe();
}

export async function removeDevice(id: string) {
  const { response, error } = await api.DELETE(
    "/api/v2/push/subscriptions/{subscriptionId}",
    { params: { path: { subscriptionId: id } } },
  );
  if (!response.ok && response.status !== 404)
    throw new Error(errorDetail(error, "Unable to remove device. Try again."));
  if (remembered?.id === id) remembered = null;
}

/** Turns notifications off for this device: server first, then browser. */
export async function disablePush(id: string) {
  await removeDevice(id);
  await discardSubscription();
}

/**
 * On boot of a signed-in session, refreshes a subscription the user turned
 * on earlier. Best-effort: failures leave Settings to show the state.
 */
export async function syncPushDevice() {
  if (!("Notification" in window) || Notification.permission !== "granted")
    return;
  if (!(await currentSubscription())) return;
  const { data } = await api.GET("/api/v2/push");
  if (!data?.enabled || !data.publicKey) return;
  const subscription = await ensureSubscription(
    await serviceWorkerReady(),
    data.publicKey,
  );
  await register(subscription);
}

async function forget() {
  const subscription = await currentSubscription();
  if (!subscription) return;
  try {
    const id =
      remembered?.endpoint === subscription.endpoint
        ? remembered.id
        : (await register(subscription)).id;
    await api.DELETE("/api/v2/push/subscriptions/{subscriptionId}", {
      params: { path: { subscriptionId: id } },
    });
  } finally {
    remembered = null;
    await subscription.unsubscribe();
  }
}

/**
 * Before sign-out, stops this browser receiving the account's notifications.
 * Best-effort and bounded to a few seconds: it never throws or holds sign-out.
 */
export async function forgetPushDevice() {
  let timer: ReturnType<typeof setTimeout> | undefined;
  await Promise.race([
    forget().catch(() => {}),
    new Promise((resolve) => {
      timer = setTimeout(resolve, 3000);
    }),
  ]);
  clearTimeout(timer);
}
