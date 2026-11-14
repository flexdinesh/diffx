import {
  Bot,
  ChevronLeft,
  GitBranch,
  GitPullRequest,
  History,
  Inbox,
  PanelRight,
  Search,
} from "lucide-react";
import type { ReactNode } from "react";
import { Collapse, Count, Stage, type Act } from "@diffx/stageshow";
import { BrandMark } from "./parts.tsx";
import "./landing-visuals.css";

// The Reviews bench, on its Needs attention view: an agent's branch arrives
// at the top, is selected, and Space peeks at it without opening it.

type S = { arrived: boolean; selected: boolean; peek: boolean };

const initial: S = { arrived: false, selected: false, peek: false };
const done: S = { arrived: true, selected: true, peek: true };

type Review = {
  id: string;
  branch?: boolean;
  title: string;
  repo: string;
  source: ReactNode;
  age: string;
};

const arrival: Review = {
  id: "retry",
  branch: true,
  title: "retry-backoff",
  repo: "acme/api",
  source: (
    <>
      <Bot />
      Claude Code
    </>
  ),
  age: "now",
};

const unread: Review = {
  id: "headers",
  title: "Rate limit headers",
  repo: "acme/api",
  source: "@maya",
  age: "12m",
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
    id: "docs",
    branch: true,
    title: "docs-refresh",
    repo: "acme/docs",
    source: (
      <>
        <Bot />
        Codex
      </>
    ),
    age: "1d",
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
    id: "release",
    branch: true,
    title: "release/1.4",
    repo: "acme/web",
    source: "ci-runner",
    age: "6d",
  },
];

async function script(a: Act<S>) {
  await a.wait(1000);
  await a.set({ arrived: true });
  await a.wait(1300);
  await a.move({ at: "row-retry", x: 0.4, y: 0.5 });
  await a.wait(200);
  await a.click();
  await a.set({ selected: true });
  if (a.layout === "compact") {
    await a.wait(400);
    await a.move("rest");
    return;
  }
  await a.wait(600);
  await a.key("space");
  await a.set({ peek: true });
  await a.wait(500);
  await a.move("rest");
}

const WIDE = { width: 670, height: 460 };
const COMPACT = { width: 358, height: 400 };

export function BenchDemo() {
  return (
    <Stage
      width={WIDE.width}
      height={WIDE.height}
      compact={{ below: 520, ...COMPACT }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      keys={{ wide: "bottom-left", compact: "bottom" }}
      cursor={{
        at: { wide: { x: 300, y: 380 }, compact: { x: 280, y: 330 } },
      }}
      className="dxl dx-bench"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        return (
          <>
            <div
              className="dx-app"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 420 }
                  : { left: 32, top: 32, width: 606, height: 500 }
              }
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                <span className="dx-crumb dx-crumb--title">
                  {!compact && <ChevronLeft />}
                  Reviews
                </span>
                <span className="dx-header-actions">
                  {!compact && (
                    <span className="dx-button" data-variant="ghost">
                      <History />
                      History
                    </span>
                  )}
                  <span className="dx-icon-button">
                    <Search />
                  </span>
                </span>
              </div>
              <div className="dx-body dx-bench-body">
                <main
                  className="dx-main dx-table"
                  data-peek={s.peek || undefined}
                >
                  <div className="dx-toolbar dx-bench-toolbar">
                    <span className="dx-bench-heading">
                      <Inbox />
                      Needs attention
                      <span className="dx-count">
                        <Count value={s.arrived ? 2 : 1} />
                      </span>
                    </span>
                    {!compact && (
                      <span
                        className="dx-button dx-end"
                        data-variant="outline"
                        data-on={s.peek || undefined}
                      >
                        <PanelRight />
                        Peek
                        <span className="dx-kbd">Space</span>
                      </span>
                    )}
                  </div>
                  {!compact && (
                    <div className="dx-bench-row dx-bench-columns">
                      <span />
                      <span />
                      <span>Review</span>
                      <span className="dx-bench-source">Source</span>
                      <span className="dx-bench-age">Activity</span>
                    </div>
                  )}
                  <div className="dx-section">
                    New and updated
                    <span className="dx-count">
                      <Count value={s.arrived ? 2 : 1} />
                    </span>
                  </div>
                  <Collapse open={s.arrived} reveal="drop">
                    <BenchRow
                      review={arrival}
                      unread
                      selected={s.selected}
                      compact={compact}
                    />
                  </Collapse>
                  <BenchRow review={unread} unread compact={compact} />
                  <div className="dx-section">
                    Everything else
                    <span className="dx-count">{rest.length}</span>
                  </div>
                  {rest.map((review) => (
                    <BenchRow
                      key={review.id}
                      review={review}
                      compact={compact}
                    />
                  ))}
                </main>
                {!compact && <Peek open={s.peek} />}
              </div>
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 280 : 300,
                top: compact ? 330 : 380,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}

function BenchRow({
  review,
  unread,
  selected,
  compact,
}: {
  review: Review;
  unread?: boolean;
  selected?: boolean;
  compact: boolean;
}) {
  return (
    <div
      className="dx-bench-row"
      data-target={`row-${review.id}`}
      data-selected={selected || undefined}
      data-unread={unread || undefined}
      data-compact={compact || undefined}
    >
      <span className="dx-unread" />
      {review.branch ? <GitBranch /> : <GitPullRequest />}
      <span className="dx-bench-name" data-mono={review.branch || undefined}>
        {review.title}
      </span>
      {compact ? (
        <span className="dx-bench-meta">{review.repo}</span>
      ) : (
        <span className="dx-bench-source">{review.source}</span>
      )}
      <span className="dx-bench-age">{review.age}</span>
    </div>
  );
}

function Peek({ open }: { open: boolean }) {
  return (
    <aside className="dx-peek" data-open={open || undefined}>
      <div className="dx-pane-title">Peek</div>
      <div className="dx-peek-body">
        <div className="dx-peek-origin">
          <GitBranch />
          acme/api · From your machines
        </div>
        <div className="dx-peek-title">retry-backoff</div>
        <div className="dx-peek-badges">
          <span className="dx-badge" data-tone="accent">
            New
          </span>
          <span className="dx-peek-source">
            <Bot />
            Claude Code · laptop
          </span>
        </div>
        <div className="dx-peek-actions">
          <span className="dx-button" data-variant="primary" data-size="md">
            Open review
            <span className="dx-kbd dx-kbd--on-accent">Enter</span>
          </span>
        </div>
        <div className="dx-peek-label">Changes</div>
        <div className="dx-peek-line">
          <span>Changed files</span>
          <span>
            7 files <span className="dx-minus">-6</span>{" "}
            <span className="dx-plus">+41</span>
          </span>
        </div>
        <div className="dx-peek-line">
          <span>Your progress</span>
          <span>0 of 7 reviewed</span>
        </div>
        <div className="dx-peek-label">Where it changes</div>
        <div className="dx-peek-line">
          <span>src</span>
          <span className="dx-peek-mono">3 files</span>
        </div>
        <div className="dx-peek-line">
          <span>test</span>
          <span className="dx-peek-mono">2 files</span>
        </div>
      </div>
    </aside>
  );
}
