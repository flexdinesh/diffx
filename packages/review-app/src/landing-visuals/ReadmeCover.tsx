import {
  ArrowDown,
  ArrowUp,
  Bot,
  ChevronDown,
  ChevronLeft,
  ChevronUp,
  ChevronsDownUp,
  Circle,
  CircleCheck,
  GitPullRequest,
  History,
  Inbox,
  RefreshCw,
  Search,
} from "lucide-react";
import {
  Collapse,
  Count,
  Draft,
  Roll,
  Stage,
  type Act,
} from "@diffx/stageshow";
import { after, before, PROMPT, REPLY, Step } from "./AgentsDemo.tsx";
import { BenchRow, type Review } from "./BenchDemo.tsx";
import { COMMENT } from "./CommentsDemo.tsx";
import { retryLines } from "./HeroDiff.tsx";
import {
  BrandMark,
  DiffRow,
  FileHeader,
  GitHubMark,
  TreeNode,
  type DiffLine,
  type TreeEntry,
} from "./parts.tsx";
import "./landing-visuals.css";

// The README cover: the whole loop on a diffx server in one window. A GitHub
// pull request arrives on the bench and opens; a comment lands on line 11;
// a coding agent reads it over MCP, fixes the line, replies and resolves it.
// Filmed to animated WebP by apps/web/scripts/record-readme-cover.ts.

type S = {
  scene: "bench" | "review";
  arrived: boolean;
  selected: boolean;
  hover: boolean;
  open: boolean;
  draft: string;
  editing: boolean;
  saved: boolean;
  agent: boolean;
  typed: string;
  step: number;
};

const initial: S = {
  scene: "bench",
  arrived: false,
  selected: false,
  hover: false,
  open: false,
  draft: "",
  editing: true,
  saved: false,
  agent: false,
  typed: "",
  step: 0,
};
const done: S = {
  scene: "review",
  arrived: true,
  selected: true,
  hover: false,
  open: true,
  draft: COMMENT,
  editing: false,
  saved: true,
  agent: true,
  typed: PROMPT,
  step: 4,
};

async function script(a: Act<S>) {
  // Bench: the pull request arrives and opens
  await a.wait(900);
  await a.set({ arrived: true });
  await a.wait(1100);
  await a.move({ at: "row-pr", x: 0.35, y: 0.5 });
  await a.wait(150);
  await a.click();
  await a.set({ selected: true });
  await a.wait(450);
  await a.key("enter");
  await a.set({ scene: "review" });
  await a.wait(900);
  // Review: comment on line 11
  await a.move({ at: "line", x: 0.035, y: 0.5 });
  await a.set({ hover: true });
  await a.wait(300);
  await a.click();
  await a.set({ open: true });
  await a.wait(450);
  await a.type("draft", COMMENT);
  await a.wait(300);
  await a.move({ at: "submit", x: 0.6, y: 0.55 });
  await a.wait(150);
  await a.click();
  await a.set({ editing: false, saved: true, hover: false });
  await a.wait(400);
  await a.move("rest");
  // Agent over MCP
  await a.set({ agent: true });
  await a.wait(600);
  await a.type("typed", PROMPT);
  await a.wait(300);
  await a.key("enter");
  for (let step = 1; step <= 4; step++) {
    await a.wait(step === 1 ? 500 : 1000);
    await a.set({ step });
  }
  await a.wait(2400);
  // Back to the bench before it starts over: the review fades out whole,
  // then resets out of sight
  await a.set({
    scene: "bench",
    agent: false,
    arrived: false,
    selected: false,
  });
  await a.wait(600);
  await a.set(initial);
  await a.wait(200);
  a.mark("end");
}

const W = 1000;
const H = 620;

const pr: Review = {
  id: "pr",
  title: "Retry webhooks with backoff",
  repo: "acme/api",
  source: (
    <>
      <GitHubMark size={14} />
      @maya
    </>
  ),
  age: "now",
};

const unread: Review = {
  id: "agent",
  branch: true,
  title: "docs-refresh",
  repo: "acme/docs",
  source: (
    <>
      <Bot />
      Codex
    </>
  ),
  age: "8m",
};

const rest: readonly Review[] = [
  {
    id: "eslint",
    title: "Bump eslint to 10",
    repo: "acme/web",
    source: "@ana",
    age: "2h",
  },
  {
    id: "login",
    title: "Fix login redirect",
    repo: "acme/web",
    source: "@sam",
    age: "3d",
  },
  {
    id: "node",
    title: "Pin Node to 26",
    repo: "acme/api",
    source: "@lee",
    age: "4d",
  },
  {
    id: "upload",
    title: "Cap upload size",
    repo: "acme/api",
    source: "@lee",
    age: "5d",
  },
  {
    id: "headers",
    title: "Rate limit headers",
    repo: "acme/api",
    source: "@maya",
    age: "6d",
  },
  {
    id: "release",
    branch: true,
    title: "release/1.4",
    repo: "acme/web",
    source: "ci-runner",
    age: "1w",
  },
  {
    id: "cache",
    title: "Cache tenant lookups",
    repo: "acme/api",
    source: "@ana",
    age: "1w",
  },
  {
    id: "flags",
    title: "Remove old feature flags",
    repo: "acme/web",
    source: "@sam",
    age: "2w",
  },
  {
    id: "deps",
    title: "Update dependencies",
    repo: "acme/docs",
    source: "@lee",
    age: "2w",
  },
  {
    id: "tz",
    title: "Store times in UTC",
    repo: "acme/api",
    source: "@ana",
    age: "3w",
  },
  {
    id: "a11y",
    title: "Label icon buttons",
    repo: "acme/web",
    source: "@sam",
    age: "3w",
  },
  {
    id: "logs",
    title: "Structured request logs",
    repo: "acme/api",
    source: "@maya",
    age: "1mo",
  },
];

const tree: readonly (TreeEntry & { selected?: boolean })[] = [
  { name: "src", depth: 0, folder: "open" },
  { name: "http.ts", depth: 1, status: "M" },
  { name: "retry.ts", depth: 1, status: "M", selected: true },
  { name: "sleep.ts", depth: 1, status: "A" },
  { name: "webhooks.ts", depth: 1, status: "M" },
  { name: "test", depth: 0, folder: "open" },
  { name: "http.test.ts", depth: 1, status: "M" },
  { name: "retry.test.ts", depth: 1, status: "A" },
  { name: "CHANGELOG.md", depth: 0, status: "M" },
];

/** retry.ts from just above line 11, the thread, then the rest of the file. */
const head: readonly DiffLine[] = [
  { kind: "hunk", text: "@@ -5,7 +5,14 @@ const baseDelay = 250;" },
  ...retryLines.slice(5, 8),
];
const more: readonly DiffLine[] = [
  { kind: "ctx", n: 19, text: "" },
  { kind: "ctx", n: 20, text: "export function backoff(attempt: number) {" },
  { kind: "ctx", n: 21, text: "  return baseDelay * 2 ** attempt;" },
  { kind: "ctx", n: 22, text: "}" },
  { kind: "ctx", n: 23, text: "" },
  { kind: "ctx", n: 24, text: "export const maxAttempts = 6;" },
  { kind: "ctx", n: 25, text: "" },
  { kind: "ctx", n: 26, text: "export function retryAfter(header: string) {" },
  { kind: "ctx", n: 27, text: "  const seconds = Number(header);" },
  { kind: "ctx", n: 28, text: "  return seconds * 1000;" },
  { kind: "ctx", n: 29, text: "}" },
  { kind: "ctx", n: 30, text: "" },
  { kind: "ctx", n: 31, text: "export function isTimeout(error: unknown) {" },
  { kind: "ctx", n: 32, text: "  return error instanceof TimeoutError;" },
  { kind: "ctx", n: 33, text: "}" },
];
const tail = [...retryLines.slice(9), ...more];
/** The same lines once the fix adds one: numbers below it move down. */
const shifted = (line: DiffLine): DiffLine =>
  line.kind === "hunk" ? line : { ...line, n: line.n + 1 };

export function ReadmeCover() {
  return (
    <Stage
      width={W}
      height={H}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      keys={{ wide: "bottom-left", compact: "bottom" }}
      cursor={{ at: { x: 700, y: 330 } }}
      className="dxl dx-cover"
    >
      {(s) => (
        <>
          <div
            className="dx-app"
            style={{ left: 0, top: 0, width: W, height: H }}
          >
            <section
              className="dx-cover-scene"
              data-show={s.scene === "bench" || undefined}
            >
              <Bench s={s} />
            </section>
            <section
              className="dx-cover-scene"
              data-show={s.scene === "review" || undefined}
            >
              <ReviewScene s={s} />
            </section>
          </div>
          <div
            className="dx-app dx-terminal dx-cover-terminal"
            data-show={s.agent || undefined}
            style={{ left: 520, top: 404, width: 456 }}
          >
            <div className="dx-terminal-bar">claude · ~/acme/api</div>
            <div className="dx-terminal-body">
              <div className="dx-prompt">
                <span className="dx-sigil">&gt;</span>
                <span>
                  {s.typed}
                  <span
                    className="dx-caret"
                    data-hidden={s.step > 0 || undefined}
                  />
                </span>
              </div>
              <Step show={s.step >= 1} tool="find_feedback" mcp>
                1 open thread
              </Step>
              <Step show={s.step >= 2} tool="Update(src/retry.ts)">
                <span className="dx-term-add">+2</span>{" "}
                <span className="dx-term-del">-1</span>
              </Step>
              <Step show={s.step >= 3} tool="reply_to_review_comment" mcp />
              <Step show={s.step >= 4} tool="resolve_review_comment" mcp />
            </div>
          </div>
          <span
            data-target="rest"
            style={{ position: "absolute", left: 700, top: 330 }}
          />
        </>
      )}
    </Stage>
  );
}

function Bench({ s }: { s: S }) {
  return (
    <>
      <div className="dx-header">
        <BrandMark />
        <span className="dx-divider" />
        <span className="dx-crumb dx-crumb--title">
          <ChevronLeft />
          Reviews
        </span>
        <span className="dx-header-actions">
          <span className="dx-button" data-variant="ghost">
            <History />
            History
          </span>
          <span className="dx-icon-button">
            <Search />
          </span>
        </span>
      </div>
      <div className="dx-body dx-bench-body">
        <main className="dx-main dx-table">
          <div className="dx-toolbar dx-bench-toolbar">
            <span className="dx-bench-heading">
              <Inbox />
              Needs attention
              <span className="dx-count">
                <Count value={s.arrived ? 2 : 1} />
              </span>
            </span>
          </div>
          <div className="dx-bench-row dx-bench-columns">
            <span />
            <span />
            <span>Review</span>
            <span className="dx-bench-source">Source</span>
            <span className="dx-bench-age">Activity</span>
          </div>
          <div className="dx-section">
            New and updated
            <span className="dx-count">
              <Count value={s.arrived ? 2 : 1} />
            </span>
          </div>
          <Collapse open={s.arrived} reveal="drop">
            <BenchRow
              review={pr}
              unread
              selected={s.selected}
              compact={false}
            />
          </Collapse>
          <BenchRow review={unread} unread compact={false} />
          <div className="dx-section">
            Everything else
            <span className="dx-count">{rest.length}</span>
          </div>
          {rest.map((review) => (
            <BenchRow key={review.id} review={review} compact={false} />
          ))}
        </main>
      </div>
    </>
  );
}

function ReviewScene({ s }: { s: S }) {
  return (
    <>
      <div className="dx-header">
        <BrandMark />
        <span className="dx-divider" />
        <span className="dx-identity">
          <span className="dx-identity-icon">
            <GitPullRequest />
          </span>
          <span className="dx-identity-title">Retry webhooks with backoff</span>
          <span className="dx-identity-meta">· acme/api #482 · @maya</span>
        </span>
        <span className="dx-header-actions">
          <span className="dx-button" data-variant="ghost">
            <RefreshCw />
            Refresh
          </span>
        </span>
      </div>
      <div className="dx-body">
        <aside className="dx-sidebar" style={{ width: 250 }}>
          <div className="dx-tabs">
            <span className="dx-tab" data-selected>
              Changes <span className="dx-count">8</span>
            </span>
            <span className="dx-tab">
              Review{" "}
              <span className="dx-count">
                <Count value={s.saved && s.step < 4 ? 1 : 0} />
              </span>
            </span>
            <span className="dx-icon-button dx-tabs-end">
              <ChevronsDownUp />
            </span>
          </div>
          <div className="dx-tree">
            {tree.map((entry) => (
              <TreeNode
                key={entry.name}
                entry={entry}
                selected={entry.selected === true}
              />
            ))}
          </div>
          <div className="dx-summary">
            <div className="dx-summary-title">
              Summary
              <ChevronDown />
            </div>
            <div className="dx-summary-line">
              <span>Files changed</span>
              <span>8</span>
            </div>
            <div className="dx-summary-line">
              <span>Additions</span>
              <span className="dx-plus">+{s.step >= 2 ? 49 : 48}</span>
            </div>
            <div className="dx-summary-line">
              <span>Deletions</span>
              <span className="dx-minus">-7</span>
            </div>
            <div className="dx-summary-line">
              <span>Open threads</span>
              <span>
                <Count value={s.saved && s.step < 4 ? 1 : 0} />
              </span>
            </div>
          </div>
          <div className="dx-statusbar">
            <GitHubMark size={14} />
            Following acme/api
          </div>
        </aside>
        <main className="dx-main">
          <div className="dx-toolbar">
            <span className="dx-picker">
              Pull request
              <ChevronDown />
            </span>
            <span className="dx-toolbar-end">
              <span className="dx-icon-button">
                <ChevronUp />
              </span>
              <span className="dx-icon-button">
                <ChevronDown />
              </span>
              <span className="dx-icon-button">
                <ArrowUp />
              </span>
              <span className="dx-icon-button">
                <ArrowDown />
              </span>
              <span className="dx-segmented">
                <span>Split</span>
                <span data-selected>Unified</span>
              </span>
            </span>
          </div>
          <div className="dx-canvas">
            <section className="dx-file">
              <FileHeader
                path="src/retry.ts"
                status="M"
                plus={s.step >= 2 ? 9 : 8}
                minus={1}
                reviewed={false}
              />
              <div className="dx-rows">
                {head.map((line, index) => (
                  <DiffRow key={index} line={line} />
                ))}
                <Collapse open={s.step < 2}>
                  <DiffRow
                    line={before}
                    target="line"
                    hovered={s.hover}
                    anchored={s.saved}
                    gutterAdd={s.hover}
                  />
                </Collapse>
                <Collapse open={s.step >= 2}>
                  {after.map((line, index) => (
                    <DiffRow key={index} line={line} anchored />
                  ))}
                </Collapse>
                <Collapse open={s.open}>
                  <div className="dx-annotation">
                    <Thread s={s} />
                  </div>
                </Collapse>
                {tail.map((line, index) => (
                  <DiffRow
                    key={index}
                    line={s.step >= 2 ? shifted(line) : line}
                  />
                ))}
              </div>
            </section>
          </div>
        </main>
      </div>
    </>
  );
}

/** The composer, saved as an open thread, then the agent's reply and the
 * resolution arriving in the same card. */
function Thread({ s }: { s: S }) {
  const resolved = s.step >= 4;
  return (
    <div className="dx-comment dx-thread">
      <div className="dx-swap">
        <div className="dx-comment-head" data-show={!s.saved || undefined}>
          <span className="dx-comment-label">New comment</span>
          <span className="dx-comment-loc">src/retry.ts:11</span>
        </div>
        <div className="dx-comment-head" data-show={s.saved || undefined}>
          <ChevronDown />
          <span className="dx-comment-meta">Line 11 · You</span>
          <span className="dx-comment-head-end">
            <span
              className="dx-badge"
              data-tone={resolved ? "success" : undefined}
            >
              {resolved ? <CircleCheck /> : <Circle />}
              <Roll>{resolved ? "Resolved" : "Open"}</Roll>
            </span>
          </span>
        </div>
      </div>
      <Draft
        value={s.draft}
        full={COMMENT}
        editing={s.editing}
        placeholder="Leave a review comment…"
      />
      <Collapse open={!s.saved}>
        <div className="dx-comment-actions">
          <span className="dx-hint">Ctrl+Enter to comment</span>
          <span className="dx-button dx-end" data-variant="ghost">
            Cancel
          </span>
          <span
            className="dx-button"
            data-variant="primary"
            data-target="submit"
          >
            Comment
          </span>
        </div>
      </Collapse>
      <Collapse open={s.step >= 3} reveal="drop">
        <div className="dx-reply">
          <div className="dx-reply-meta">
            <Bot />
            <span className="dx-reply-author">Claude Code</span>· now
          </div>
          <div className="dx-comment-body">{REPLY}</div>
        </div>
      </Collapse>
    </div>
  );
}
