import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>", {
  url: "http://localhost/",
  pretendToBeVisual: true,
});
for (const name of [
  "window",
  "document",
  "navigator",
  "HTMLElement",
  "Element",
  "Node",
  "MutationObserver",
  "Event",
  "MouseEvent",
  "KeyboardEvent",
  "PopStateEvent",
  "localStorage",
  "sessionStorage",
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

// DOM tests use desktop policy only. Responsive geometry and modal focus stay
// in Chromium; no layout measurements are simulated here.
Object.defineProperty(globalThis, "matchMedia", {
  value: (media: string) => ({
    media,
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }),
});
Object.defineProperty(globalThis, "EventSource", {
  value: class extends dom.window.EventTarget {
    close() {}
  },
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

export function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error("Promise not initialized");
  };
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}
