"use client";
import {
  Bot,
  Check,
  CheckCheck,
  FolderGit2,
  GitBranch,
  GitPullRequest,
  GitPullRequestArrow,
  History,
  Inbox,
  PanelRight,
  Search,
} from "lucide-react";
import type { ReactNode } from "react";
import { Collapse, Count, Stage, type Act } from "../stageshow/index.ts";
import { BrandMark } from "./parts.tsx";
import "./landing.css";

// The Reviews bench: an agent's branch arrives in Needs attention beside a
// pull request, is selected, and Space peeks at it without opening it.

type S = { arrived: boolean; selected: boolean; peek: boolean };

const initial: S = { arrived: false, selected: false, peek: false };
const done: S = { arrived: true, selected: true, peek: true };

type Review = {
  id: string;
  branch?: boolean;
  title: string;
  source: ReactNode;
  age: string;
};

const arrival: Review = {
  id: "retry",
  branch: true,
  title: "retry-backoff",
  source: (
    <>
      <Bot />
      Claude Code · laptop
    </>
  ),
  age: "now",
};

const unread: Review = {
  id: "headers",
  title: "Add rate limit headers",
  source: "GitHub · @maya",
  age: "12m",
};

const rest: readonly Review[] = [
  {
    id: "eslint",
    title: "Bump eslint to 10",
    source: "GitHub · @ana",
    age: "2h",
  },
  {
    id: "node",
    title: "Pin Node to 26",
    source: "GitHub · @lee",
    age: "1d",
  },
  {
    id: "docs",
    branch: true,
    title: "docs-refresh",
    source: (
      <>
        <Bot />
        Codex · devbox
      </>
    ),
    age: "2d",
  },
  {
    id: "login",
    title: "Fix login redirect loop",
    source: "GitHub · @sam",
    age: "3d",
  },
  {
    id: "release",
    branch: true,
    title: "release/1.4",
    source: "ci-runner",
    age: "3d",
  },
];

async function script(a: Act<S>) {
  await a.wait(1000);
  await a.set({ arrived: true });
  await a.wait(1300);
  await a.move({ at: "row-retry", x: 0.45, y: 0.55 });
  await a.wait(200);
  await a.click();
  await a.set({ selected: true });
  if (a.layout === "compact") {
    await a.wait(400);
    await a.move("rest");
    return;
  }
  await a.wait(500);
  await a.key("space");
  await a.set({ peek: true });
  await a.wait(400);
  await a.move("rest");
}

export function BenchDemo() {
  return (
    <Stage
      width={620}
      height={420}
      compact={{ below: 520, width: 358, height: 340 }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      cursor={{
        at: { wide: { x: 420, y: 85 }, compact: { x: 300, y: 64 } },
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
                  ? { left: 16, top: 16, width: 326, height: 360 }
                  : { left: 28, top: 28, width: 564, height: 430 }
              }
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                <span className="dx-crumb dx-crumb--title">
                  <Inbox />
                  Reviews
                </span>
                <span className="dx-header-actions">
                  {!compact && (
                    <span className="dx-crumb">
                      <History />
                      History
                    </span>
                  )}
                  <span className="dx-icon-button">
                    <Search />
                  </span>
                </span>
              </div>
              <div className="dx-body" style={{ position: "relative" }}>
                {!compact && (
                  <aside
                    className="dx-sidebar dx-facets"
                    style={{ width: 172 }}
                  >
                    <div className="dx-pane-title">Views</div>
                    <Facet icon={<Inbox />} label="Needs attention" selected>
                      <Count value={s.arrived ? 2 : 1} />
                    </Facet>
                    <Facet icon={<GitPullRequestArrow />} label="All open">
                      <Count value={s.arrived ? 6 : 5} />
                    </Facet>
                    <Facet icon={<Check />} label="Closed">
                      12
                    </Facet>
                    <div className="dx-facet-label">Repositories</div>
                    <Facet icon={<FolderGit2 />} label="acme/api">
                      <Count value={s.arrived ? 3 : 2} />
                    </Facet>
                    <Facet icon={<FolderGit2 />} label="acme/web">
                      2
                    </Facet>
                    <Facet icon={<FolderGit2 />} label="acme/docs">
                      1
                    </Facet>
                    <div className="dx-facet-label">Agents</div>
                    <Facet icon={<Bot />} label="Claude Code">
                      <Count value={s.arrived ? 1 : 0} />
                    </Facet>
                    <Facet icon={<Bot />} label="Codex">
                      1
                    </Facet>
                  </aside>
                )}
                <main
                  className="dx-main dx-table"
                  data-peek={s.peek || undefined}
                >
                  <div className="dx-toolbar" style={{ padding: "0 8px" }}>
                    <span
                      className="dx-field"
                      style={{ margin: 0, height: 26, flex: 1, maxWidth: 220 }}
                    >
                      <Search />
                      Filter reviews…
                    </span>
                    {!compact && (
                      <span
                        className="dx-button dx-end"
                        data-variant={s.peek ? undefined : "ghost"}
                        data-on={s.peek || undefined}
                      >
                        <PanelRight />
                        Peek
                        <span className="dx-kbd" style={{ marginLeft: 2 }}>
                          Space
                        </span>
                      </span>
                    )}
                  </div>
                  {!compact && (
                    <div className="dx-bench-row dx-bench-columns">
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
                    <span className="dx-section-end">
                      <CheckCheck />
                      Mark all viewed
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
                left: compact ? 300 : 420,
                top: compact ? 64 : 85,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}

function Facet({
  icon,
  label,
  selected,
  children,
}: {
  icon: ReactNode;
  label: string;
  selected?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="dx-facet" data-selected={selected || undefined}>
      {icon}
      <span className="dx-facet-name">{label}</span>
      <span className="dx-count">{children}</span>
    </div>
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
      <span className="dx-bench-title">
        {review.branch ? <GitBranch /> : <GitPullRequest />}
        <span className="dx-bench-name" data-mono={review.branch || undefined}>
          {review.title}
        </span>
      </span>
      {!compact && <span className="dx-bench-source">{review.source}</span>}
      <span className="dx-bench-age">{review.age}</span>
    </div>
  );
}

function Peek({ open }: { open: boolean }) {
  return (
    <aside
      className="dx-peek"
      data-open={open || undefined}
      aria-hidden={!open}
    >
      <div className="dx-pane-title">Peek</div>
      <div className="dx-peek-body">
        <div className="dx-peek-origin">
          <GitBranch />
          retry-backoff · From your machines
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
            7 files <span className="dx-plus">+41</span>{" "}
            <span className="dx-minus">−6</span>
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
