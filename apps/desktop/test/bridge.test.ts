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
    confirmSwitch: false,
    destination: "",
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
    { ...ready, confirmSwitch: "false" },
    { ...ready, destination: null },
    { ...ready, error: null },
  ]) {
    assert.throws(() => launchStatus(response), /Incompatible desktop bridge/);
  }
});
