import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { createElement } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { ServerStatus } from "../../src/ServerStatus.tsx";
import { deferred, json, requests } from "./setup.ts";

afterEach(cleanup);

test("a metrics response slower than the poll interval still reports ready", async () => {
  const pending = deferred<Response>();
  let reads = 0;
  let signal: AbortSignal | undefined;
  requests((request) => {
    reads++;
    signal = request.signal;
    return pending.promise;
  });
  const view = render(createElement(ServerStatus));
  assert.ok(screen.getByRole("button", { name: "Server status: Connecting" }));
  // Cross the production polling interval while the HTTP response is held.
  await act(async () => setTimeout(2_600));
  assert.equal(reads, 1);
  assert.equal(signal?.aborted, false);
  await act(async () =>
    pending.resolve(json({ cpuUsage: 1, rssBytes: 1_048_576 })),
  );
  assert.ok(screen.getByRole("button", { name: "Server status: Ready" }));
  view.unmount();
  assert.equal(signal?.aborted, true);
});
