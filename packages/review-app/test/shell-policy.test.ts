import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiBootstrap } from "@diffx/api";
import {
  publicPreviewPolicy,
  shellPolicy,
  showsReviewColumn,
} from "../src/shell-policy.ts";

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
  publicPullRequests: false,
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
    reviewMarks: true,
    serverStatus: true,
  });
  assert.deepEqual(shellPolicy(local("checkouts")), {
    reviewNavigation: true,
    reviewColumn: "multiple",
    collection: "checkouts",
    reviewMarks: true,
    serverStatus: true,
  });
  assert.deepEqual(shellPolicy(local("capture")), {
    reviewNavigation: false,
    reviewColumn: "multiple",
    collection: null,
    reviewMarks: true,
    serverStatus: true,
  });
  assert.deepEqual(shellPolicy(remote), {
    reviewNavigation: true,
    reviewColumn: "always",
    collection: null,
    reviewMarks: true,
    serverStatus: true,
  });
});

test("a public preview is one read-only snapshot without review navigation", () => {
  const visitor = publicPreviewPolicy(remote);
  assert.deepEqual(visitor, {
    reviewNavigation: false,
    reviewColumn: "multiple",
    collection: null,
    reviewMarks: false,
    serverStatus: false,
  });
  assert.equal(showsReviewColumn(visitor, 3), false);
  const member = publicPreviewPolicy({
    ...remote,
    user: { id: "account-a", email: "alice@example.com", method: "google" },
    csrfToken: "csrf",
  });
  assert.equal(member.serverStatus, true);
  assert.equal(member.reviewMarks, false);
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
