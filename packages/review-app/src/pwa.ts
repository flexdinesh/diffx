import { useSyncExternalStore } from "react";

/**
 * The remote server's installable app shell: head tags, the notification
 * service worker and the captured install prompt. Local and capture modes
 * never call `startPwa`, so they get none of these.
 */

/** Chromium's install prompt event; not part of the DOM typings. */
interface InstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
}

function installPromptEvent(event: Event): event is InstallPromptEvent {
  return (
    "prompt" in event &&
    typeof event.prompt === "function" &&
    "userChoice" in event
  );
}

let started = false;
let registration: Promise<ServiceWorkerRegistration> | null = null;
let installPrompt: InstallPromptEvent | null = null;
const listeners = new Set<() => void>();

function changed() {
  for (const listener of listeners) listener();
}

function appendHead(tag: string, attributes: Record<string, string>) {
  const element = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes))
    element.setAttribute(name, value);
  document.head.append(element);
}

/** Installs the app shell once; later calls are no-ops. */
export function startPwa() {
  if (started) return;
  started = true;
  // Chromium fires this once the manifest below makes the app installable.
  window.addEventListener("beforeinstallprompt", (event) => {
    if (!installPromptEvent(event)) return;
    event.preventDefault();
    installPrompt = event;
    changed();
  });
  window.addEventListener("appinstalled", () => {
    installPrompt = null;
    changed();
  });
  appendHead("link", { rel: "manifest", href: "/manifest.webmanifest" });
  appendHead("link", {
    rel: "apple-touch-icon",
    href: "/icons/apple-touch-icon.png",
  });
  appendHead("meta", { name: "mobile-web-app-capable", content: "yes" });
  appendHead("meta", { name: "apple-mobile-web-app-capable", content: "yes" });
  appendHead("meta", { name: "apple-mobile-web-app-title", content: "diffx" });
  if ("serviceWorker" in navigator) {
    registration = navigator.serviceWorker.register("/sw.js", {
      scope: "/",
      // Vite serves the worker source as a module during development.
      type: import.meta.env.DEV ? "module" : "classic",
    });
    registration.catch(() => {});
  }
}

/** The worker registration, active and ready to subscribe to push. */
export async function serviceWorkerReady() {
  // Surfaces a failed registration instead of waiting for `ready` forever.
  if (registration) await registration;
  return navigator.serviceWorker.ready;
}

/** The registration if one exists, without waiting for activation. */
export async function existingServiceWorker() {
  if (!("serviceWorker" in navigator)) return undefined;
  return navigator.serviceWorker.getRegistration("/");
}

/** True when opened from the Home Screen or as an installed app window. */
export function standalone() {
  return (
    matchMedia("(display-mode: standalone)").matches ||
    ("standalone" in navigator && navigator.standalone === true)
  );
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Shows the browser's install dialog; resolves once the user answers. */
async function install() {
  const prompt = installPrompt;
  if (!prompt) return;
  installPrompt = null;
  changed();
  await prompt.prompt();
  await prompt.userChoice;
}

/** `install` while the browser offers installation, otherwise null. */
export function useInstallPrompt() {
  const available = useSyncExternalStore(
    subscribe,
    () => installPrompt !== null,
  );
  return available ? install : null;
}
