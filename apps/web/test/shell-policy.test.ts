import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiBootstrap } from "@diffx/api";
import { shellPolicy, showsReviewColumn } from "../src/shell-policy.ts";

function local(input: "checkout" | "checkouts" | "capture"): ApiBootstrap {
  return {
    mode: "local",
    googleEnabled: false,
    user: null,
    csrfToken: null,
    collect: input !== "capture",
    input,
  };
}

const inputs: ("checkout" | "checkouts")[] = ["checkout", "checkouts"];

const remote: ApiBootstrap = {
  mode: "remote",
  googleEnabled: false,
  signupEnabled: false,
  passwordSignupEnabled: false,
  user: null,
  csrfToken: null,
};

test("shell policy follows what the server serves", () => {
  assert.deepEqual(shellPolicy(local("checkout")), {
    reviewNavigation: true,
    reviewColumn: "multiple",
    collection: "checkout",
  });
  assert.deepEqual(shellPolicy(local("checkouts")), {
    reviewNavigation: true,
    reviewColumn: "multiple",
    collection: "checkouts",
  });
  assert.deepEqual(shellPolicy(local("capture")), {
    reviewNavigation: false,
    reviewColumn: "multiple",
    collection: null,
  });
  assert.deepEqual(shellPolicy(remote), {
    reviewNavigation: true,
    reviewColumn: "always",
    collection: null,
  });
});

test("a local review column waits for a second review; remote always shows", () => {
  for (const input of inputs) {
    const policy = shellPolicy(local(input));
    assert.equal(showsReviewColumn(policy, 0), false);
    assert.equal(showsReviewColumn(policy, 1), false);
    assert.equal(showsReviewColumn(policy, 2), true);
  }
  assert.equal(showsReviewColumn(shellPolicy(remote), 0), true);
  assert.equal(showsReviewColumn(shellPolicy(remote), 1), true);
  assert.equal(showsReviewColumn(shellPolicy(local("capture")), 3), false);
});
