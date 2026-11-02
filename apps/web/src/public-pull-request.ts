import { pluralize } from "@diffx/ui/lib/format";
import { relativeAge } from "./project-nav.ts";

/** A pull request in a GitHub repository, as its URL names it. */
export type PullRequestRef = { owner: string; repo: string; number: number };

// GitHub's owner and repository name rules, as the API contract states them.
const ownerPattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/;
const repoPattern = /^[A-Za-z0-9._-]+$/;
/**
 * `/{owner}/{repo}/pull/{number}` plus a GitHub tab suffix: `/files`,
 * `/commits`, `/checks`, one segment below them (`/commits/<sha>`) and a
 * trailing slash.
 */
const pullPath =
  /^\/([^/]+)\/([^/]+)\/pull\/(\d+)(?:\/(?:files|commits|checks)(?:\/[^/]+)?)?\/?$/;
const shorthand = /^([^/\s#]+)\/([^/\s#]+)#(\d+)$/;
const githubHosts = new Set(["github.com", "www.github.com"]);
/** Query parameters the review itself owns; others (GitHub's) are dropped. */
const reviewParameters = ["scope", "commit"];

function pullRequestRef(
  owner: string,
  repo: string,
  number: string,
): PullRequestRef | null {
  const value = Number(number);
  if (
    owner.length > 39 ||
    !ownerPattern.test(owner) ||
    repo.length > 100 ||
    !repoPattern.test(repo) ||
    repo === "." ||
    repo === ".." ||
    !/^[1-9]\d*$/.test(number) ||
    !Number.isSafeInteger(value)
  )
    return null;
  return { owner, repo, number: value };
}

function fromPath(pathname: string) {
  const match = pullPath.exec(pathname);
  return match?.[1] && match[2] && match[3]
    ? pullRequestRef(match[1], match[2], match[3])
    : null;
}

/**
 * Reads what someone pasted: a github.com pull request URL with or without
 * its scheme, `www.`, a tab suffix, query or fragment, or `owner/repo#123`.
 */
export function parsePullRequestInput(input: string): PullRequestRef | null {
  const text = input.trim();
  const short = shorthand.exec(text);
  if (short?.[1] && short[2] && short[3])
    return pullRequestRef(short[1], short[2], short[3]);
  if (/\s/.test(text)) return null;
  let url: URL;
  try {
    url = new URL(
      /^[a-z][a-z\d+.-]*:\/\//i.test(text) ? text : `https://${text}`,
    );
  } catch {
    return null;
  }
  if (
    (url.protocol !== "https:" && url.protocol !== "http:") ||
    !githubHosts.has(url.hostname) ||
    url.port !== "" ||
    url.username !== "" ||
    url.password !== ""
  )
    return null;
  return fromPath(url.pathname);
}

export function pullRequestPath({ owner, repo, number }: PullRequestRef) {
  return `/${owner}/${repo}/pull/${number}`;
}

export function pullRequestName({ owner, repo }: PullRequestRef) {
  return `${owner}/${repo}`;
}

export function gitHubUrl(pull: PullRequestRef) {
  return `https://github.com${pullRequestPath(pull)}`;
}

/**
 * The public preview a page URL names, with its canonical address: GitHub's
 * tab suffixes, trailing slash, fragment and query leave; the review's own
 * scope and commit parameters stay so reloads keep the view.
 */
export function publicPullRequestLocation(location: {
  pathname: string;
  search: string;
}): { pull: PullRequestRef; href: string } | null {
  const pull = fromPath(location.pathname);
  if (!pull) return null;
  const current = new URLSearchParams(location.search);
  const kept = new URLSearchParams();
  for (const name of reviewParameters) {
    const value = current.get(name);
    if (value !== null) kept.set(name, value);
  }
  const query = kept.toString();
  return { pull, href: `${pullRequestPath(pull)}${query ? `?${query}` : ""}` };
}

/** The only app pages sign-in may return to; anything else is ignored. */
const returnPages = ["/settings/github"] as const;
export type ReturnPage = (typeof returnPages)[number];

export function signInReturn(search: string): ReturnPage | null {
  const next = new URLSearchParams(search).get("next");
  return returnPages.find((page) => page === next) ?? null;
}

/** `?next=` for links between sign-in pages that keep the return page. */
export function returnQuery(next: ReturnPage | null) {
  return next ? `?${new URLSearchParams({ next }).toString()}` : "";
}

export const notPublic = "This pull request isn't public or doesn't exist.";

/** Why a preview has no snapshot, as the reader sees it. */
export function failureMessage(
  code: "not_found" | "too_large" | "fetch_failed",
  name: string,
) {
  if (code === "not_found") return notPublic;
  if (code === "too_large")
    return `${name} is over 500 MB, too large to preview.`;
  return "GitHub fetch failed.";
}

/** Retry-After seconds as whole minutes, rounding up, at least one. */
export function retryMinutes(seconds: number) {
  return Math.max(1, Math.ceil(seconds / 60));
}

export function retryLabel(seconds: number) {
  return `Try again in ${pluralize(retryMinutes(seconds), "minute")}.`;
}

/** Retry-After as seconds; a missing or malformed header waits a minute. */
export function retryAfterSeconds(header: string | null) {
  const seconds = header === null ? NaN : Number(header);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : 60;
}

export function fetchedLabel(fetchedAt: number, now: number) {
  const age = relativeAge(fetchedAt, now);
  return age === "now" ? "Fetched just now" : `Fetched ${age} ago`;
}

/** Elapsed waiting time: `12s`, then `1m 05s`. */
export function elapsedLabel(milliseconds: number) {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000));
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

/** Demo pull requests the landing page offers to try. */
export const demoPullRequests: readonly (PullRequestRef & { title: string })[] =
  [
    {
      owner: "flexdinesh",
      repo: "diffx",
      number: 157,
      title: "fix: allow cross-site page navigations on server and console",
    },
    {
      owner: "flexdinesh",
      repo: "diffx",
      number: 158,
      title: "feat: redesign account settings layout",
    },
    {
      owner: "flexdinesh",
      repo: "diffx",
      number: 144,
      title: "perf: batch collection reads and export diagnostics",
    },
  ];
