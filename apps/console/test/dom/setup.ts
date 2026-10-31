import { JSDOM, VirtualConsole } from "jsdom";

let navigations = 0;
const virtualConsole = new VirtualConsole().forwardTo(console, {
  jsdomErrors: "none",
});
// jsdom cannot load another document; record the attempt so tests can assert
// that the page left for it.
virtualConsole.on("jsdomError", (error) => {
  if (error.message.includes("navigation to another Document"))
    navigations += 1;
  else console.error(error);
});
const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  url: "http://localhost/",
  pretendToBeVisual: true,
  virtualConsole,
});
for (const name of [
  "window",
  "document",
  "navigator",
  "HTMLElement",
  "HTMLInputElement",
  "Element",
  "Node",
  "MutationObserver",
  "Event",
  "MouseEvent",
  "KeyboardEvent",
  "PopStateEvent",
  "getComputedStyle",
]) {
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: Reflect.get(dom.window, name),
  });
}
Object.defineProperty(globalThis, "IS_REACT_ACT_ENVIRONMENT", {
  value: true,
  writable: true,
});
Object.defineProperty(globalThis, "requestAnimationFrame", {
  value: dom.window.requestAnimationFrame.bind(dom.window),
});
Object.defineProperty(globalThis, "cancelAnimationFrame", {
  value: dom.window.cancelAnimationFrame.bind(dom.window),
});

// Browsers resolve API-relative URLs before fetch. Keep Node's real Request,
// Response and abort signals, adding only that URL resolution behavior.
const NativeRequest = globalThis.Request;
globalThis.Request = class extends NativeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(
      typeof input === "string" ? new URL(input, window.location.href) : input,
      init,
    );
  }
};

let respond: (request: Request) => Promise<Response> = async (request) => {
  throw new Error(`Unexpected request: ${request.method} ${request.url}`);
};
// openapi-fetch captures fetch at import time. This stable boundary delegates
// to each test's handler without replacing the production client or hooks.
globalThis.fetch = async (input, init) => respond(new Request(input, init));

export function requests(
  handler: (request: Request) => Response | Promise<Response>,
) {
  respond = async (request) => handler(request);
}

export function json(value: unknown, status = 200) {
  return Response.json(value, { status });
}

/** How often the document tried to leave for another one since the last call. */
export function takeNavigations() {
  const taken = navigations;
  navigations = 0;
  return taken;
}
