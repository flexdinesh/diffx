import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiBootstrap } from "@diffx/api";
import { publicPreviewPolicy, shellPolicy } from "../src/shell-policy.ts";

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
  githubEnabled: false,
  googleEnabled: false,
  signupEnabled: false,
  passwordSignupEnabled: false,
  user: null,
  csrfToken: null,
};

test("shell policy follows what the server serves", () => {
  assert.deepEqual(shellPolicy(local("checkout")), {
    reviewNavigation: true,
    collection: "checkout",
    reviewMarks: true,
    serverStatus: true,
    pullRequests: false,
    pullRequestLinks: false,
  });
  assert.deepEqual(shellPolicy(local("checkouts")), {
    reviewNavigation: true,
    collection: "checkouts",
    reviewMarks: true,
    serverStatus: true,
    pullRequests: false,
    pullRequestLinks: false,
  });
  assert.deepEqual(shellPolicy(local("capture")), {
    reviewNavigation: false,
    collection: null,
    reviewMarks: true,
    serverStatus: true,
    pullRequests: false,
    pullRequestLinks: false,
  });
  assert.deepEqual(shellPolicy(remote), {
    reviewNavigation: true,
    collection: null,
    reviewMarks: true,
    serverStatus: true,
    pullRequests: false,
    pullRequestLinks: false,
  });
});

test("remote readers open pull request links when the server lets them preview", () => {
  assert.equal(shellPolicy(remote).pullRequestLinks, false);
  assert.equal(
    shellPolicy({ ...remote, publicPullRequests: true }).pullRequestLinks,
    true,
  );
});

test("a public preview is one read-only snapshot without review navigation", () => {
  const visitor = publicPreviewPolicy(remote);
  assert.deepEqual(visitor, {
    reviewNavigation: false,
    collection: null,
    reviewMarks: false,
    serverStatus: false,
    pullRequests: false,
    pullRequestLinks: false,
  });
  const member = publicPreviewPolicy({
    ...remote,
    user: { id: "account-a", email: "alice@example.com", method: "google" },
    csrfToken: "csrf",
  });
  assert.equal(member.serverStatus, true);
  assert.equal(member.reviewMarks, false);
  assert.equal(
    publicPreviewPolicy({ ...remote, githubEnabled: true }).pullRequests,
    false,
  );
  assert.equal(
    publicPreviewPolicy({ ...remote, publicPullRequests: true })
      .pullRequestLinks,
    false,
  );
});

test("the pull request section follows the server's GitHub integration", () => {
  assert.equal(shellPolicy(remote).pullRequests, false);
  assert.equal(
    shellPolicy({ ...remote, githubEnabled: true }).pullRequests,
    true,
  );
  for (const input of inputs)
    assert.equal(shellPolicy(local(input)).pullRequests, false);
});
