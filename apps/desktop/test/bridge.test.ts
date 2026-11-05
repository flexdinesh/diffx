import assert from "node:assert/strict";
import { test } from "node:test";
import { launchStatus } from "../src/bridge.ts";

test("bridge refuses malformed status before mounting a review workspace", () => {
  const ready = {
    phase: "ready",
    generation: 7,
    error: "",
    owned: false,
    canReplace: false,
  };
  assert.deepEqual(launchStatus(ready), ready);
  for (const response of [
    null,
    [],
    {},
    { ...ready, phase: "attached" },
    { ...ready, generation: NaN },
    { ...ready, generation: -1 },
    { ...ready, generation: 1.5 },
    { ...ready, owned: "false" },
    { ...ready, error: null },
  ]) {
    assert.throws(() => launchStatus(response), /Incompatible desktop bridge/);
  }
});
