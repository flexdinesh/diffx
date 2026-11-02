import assert from "node:assert/strict";
import { test } from "node:test";
import {
  elapsedLabel,
  failureMessage,
  fetchedLabel,
  parsePullRequestInput,
  publicPullRequestLocation,
  pullRequestPath,
  retryAfterSeconds,
  retryLabel,
  retryMinutes,
  returnQuery,
  signInReturn,
} from "../src/public-pull-request.ts";

const diffx = { owner: "flexdinesh", repo: "diffx", number: 157 };

test("pasted pull request links in every GitHub form open the same preview", () => {
  for (const input of [
    "https://github.com/flexdinesh/diffx/pull/157",
    "http://github.com/flexdinesh/diffx/pull/157",
    "HTTPS://GitHub.com/flexdinesh/diffx/pull/157",
    "github.com/flexdinesh/diffx/pull/157",
    "www.github.com/flexdinesh/diffx/pull/157",
    "https://www.github.com/flexdinesh/diffx/pull/157",
    "  https://github.com/flexdinesh/diffx/pull/157  ",
    "https://github.com/flexdinesh/diffx/pull/157/",
    "https://github.com/flexdinesh/diffx/pull/157/files",
    "https://github.com/flexdinesh/diffx/pull/157/files/",
    "https://github.com/flexdinesh/diffx/pull/157/commits",
    "https://github.com/flexdinesh/diffx/pull/157/commits/0a1b2c3d4e",
    "https://github.com/flexdinesh/diffx/pull/157/checks",
    "https://github.com/flexdinesh/diffx/pull/157#issuecomment-1",
    "https://github.com/flexdinesh/diffx/pull/157/files?w=1#diff-abc",
    "github.com/flexdinesh/diffx/pull/157?diff=split",
    "flexdinesh/diffx#157",
  ])
    assert.deepEqual(parsePullRequestInput(input), diffx, input);
  assert.deepEqual(parsePullRequestInput("my-org/repo.name_x-1#2"), {
    owner: "my-org",
    repo: "repo.name_x-1",
    number: 2,
  });
});

test("anything but a public GitHub pull request link is rejected", () => {
  for (const input of [
    "",
    "   ",
    "flexdinesh/diffx",
    "flexdinesh/diffx#",
    "flexdinesh/diffx#0",
    "flexdinesh/diffx #157",
    "https://gitlab.com/flexdinesh/diffx/pull/157",
    "https://github.com.evil.example/flexdinesh/diffx/pull/157",
    "https://gist.github.com/flexdinesh/diffx/pull/157",
    "https://github.com:8443/flexdinesh/diffx/pull/157",
    "https://user@github.com/flexdinesh/diffx/pull/157",
    "ftp://github.com/flexdinesh/diffx/pull/157",
    "https://github.com/flexdinesh/diffx/issues/157",
    "https://github.com/flexdinesh/diffx/pulls",
    "https://github.com/flexdinesh/diffx/pull/",
    "https://github.com/flexdinesh/diffx/pull/abc",
    "https://github.com/flexdinesh/diffx/pull/0",
    "https://github.com/flexdinesh/diffx/pull/0157",
    "https://github.com/flexdinesh/diffx/pull/99999999999999999999",
    "https://github.com/extra/flexdinesh/diffx/pull/157",
    "https://github.com/flexdinesh/pull/157",
    "https://github.com/flexdinesh/diffx/pull/157/merge",
    "https://github.com/flexdinesh/diffx/pull/157/files/a/b",
    "https://github.com/-flexdinesh/diffx/pull/157",
    "https://github.com/flexdinesh-/diffx/pull/157",
    "https://github.com/flex_dinesh/diffx/pull/157",
    `https://github.com/${"a".repeat(40)}/diffx/pull/157`,
    `https://github.com/flexdinesh/${"a".repeat(101)}/pull/157`,
    "https://github.com/flexdinesh/diff%20x/pull/157",
    "https://github.com/flexdinesh/../pull/157",
    "https://github.com/flexdinesh/diffx/pull/157 extra",
    "javascript:alert(1)//github.com/a/b/pull/1",
  ])
    assert.equal(parsePullRequestInput(input), null, input);
});

test("a preview path keeps GitHub's shape and drops its tab suffixes", () => {
  assert.equal(pullRequestPath(diffx), "/flexdinesh/diffx/pull/157");
  for (const pathname of [
    "/flexdinesh/diffx/pull/157",
    "/flexdinesh/diffx/pull/157/",
    "/flexdinesh/diffx/pull/157/files",
    "/flexdinesh/diffx/pull/157/commits",
    "/flexdinesh/diffx/pull/157/commits/0a1b2c3",
    "/flexdinesh/diffx/pull/157/checks",
  ])
    assert.deepEqual(
      publicPullRequestLocation({ pathname, search: "?w=1" }),
      { pull: diffx, href: "/flexdinesh/diffx/pull/157" },
      pathname,
    );
  // The review's own view survives a reload; GitHub's parameters do not.
  assert.deepEqual(
    publicPullRequestLocation({
      pathname: "/flexdinesh/diffx/pull/157",
      search: "?diff=split&scope=branch&commit=abc",
    }),
    {
      pull: diffx,
      href: "/flexdinesh/diffx/pull/157?scope=branch&commit=abc",
    },
  );
  // App routes have other shapes.
  for (const pathname of [
    "/",
    "/signin",
    "/contexts/abc",
    "/streams/abc",
    "/settings/github",
    "/flexdinesh/diffx",
    "/flexdinesh/diffx/pull",
    "/flexdinesh/diffx/issues/157",
    "/a/flexdinesh/diffx/pull/157",
  ])
    assert.equal(
      publicPullRequestLocation({ pathname, search: "" }),
      null,
      pathname,
    );
});

test("sign-in returns only to allowed pages", () => {
  assert.equal(signInReturn("?next=/settings/github"), "/settings/github");
  assert.equal(signInReturn("?next=%2Fsettings%2Fgithub"), "/settings/github");
  for (const search of [
    "",
    "?next=",
    "?next=/settings",
    "?next=/settings/tokens",
    "?next=https://evil.example/settings/github",
    "?next=//evil.example",
    "?next=/settings/github/",
    "?other=/settings/github",
  ])
    assert.equal(signInReturn(search), null, search);
  assert.equal(returnQuery(null), "");
  assert.equal(returnQuery("/settings/github"), "?next=%2Fsettings%2Fgithub");
  assert.equal(
    signInReturn(returnQuery("/settings/github")),
    "/settings/github",
  );
});

test("rate limits read as whole minutes, rounding up", () => {
  assert.equal(retryMinutes(1), 1);
  assert.equal(retryMinutes(60), 1);
  assert.equal(retryMinutes(61), 2);
  assert.equal(retryMinutes(3599), 60);
  assert.equal(retryLabel(30), "Try again in 1 minute.");
  assert.equal(retryLabel(150), "Try again in 3 minutes.");
  assert.equal(retryAfterSeconds("120"), 120);
  assert.equal(retryAfterSeconds(null), 60);
  assert.equal(retryAfterSeconds("soon"), 60);
  assert.equal(retryAfterSeconds("0"), 60);
});

test("fetch times and waits read compactly", () => {
  const now = 10 * 60 * 60_000;
  assert.equal(fetchedLabel(now - 10_000, now), "Fetched just now");
  assert.equal(fetchedLabel(now - 3 * 60_000, now), "Fetched 3m ago");
  assert.equal(fetchedLabel(now - 2 * 60 * 60_000, now), "Fetched 2h ago");
  assert.equal(elapsedLabel(0), "0s");
  assert.equal(elapsedLabel(12_400), "12s");
  assert.equal(elapsedLabel(65_000), "1m 05s");
});

test("failures name the problem in the reader's terms", () => {
  assert.equal(
    failureMessage("not_found", "a/b"),
    "This pull request isn't public or doesn't exist.",
  );
  assert.equal(
    failureMessage("too_large", "a/b"),
    "a/b is over 500 MB, too large to preview.",
  );
  assert.equal(failureMessage("fetch_failed", "a/b"), "GitHub fetch failed.");
});
