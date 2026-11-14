import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  ChevronsDownUp,
  EyeOff,
  GitBranch,
  ListTree,
  RefreshCw,
  Search,
  Settings2,
} from "lucide-react";
import { Collapse, Roll, Stage, type Act } from "@diffx/stageshow";
import {
  BrandMark,
  DiffRow,
  FileHeader,
  TreeNode,
  type DiffLine,
  type TreeEntry,
} from "./parts.tsx";
import "./landing-visuals.css";

// The review window itself, whole in the showcase band: Alt+M marks the open
// file reviewed, it collapses to its header, the next file rises and the
// sidebar's progress counts it; the header's Reviewed toggle does the same.

type FileId = "http" | "retry" | "sleep";
type S = {
  reviewed: FileId[];
  selected: FileId | "test";
  pressed: boolean;
};

const initial: S = { reviewed: ["http"], selected: "retry", pressed: false };
const done: S = {
  reviewed: ["http", "retry", "sleep"],
  selected: "test",
  pressed: false,
};
const TOTAL = 7;

const tree: readonly (TreeEntry & { id?: FileId | "test" })[] = [
  { name: "docs", depth: 0, folder: "closed" },
  { name: "src", depth: 0, folder: "open" },
  { name: "http.ts", depth: 1, status: "M", id: "http" },
  { name: "retry.ts", depth: 1, status: "M", id: "retry" },
  { name: "sleep.ts", depth: 1, status: "A", id: "sleep" },
  { name: "test", depth: 0, folder: "open" },
  { name: "http.test.ts", depth: 1, status: "M", id: "test" },
  { name: "retry.test.ts", depth: 1, status: "A" },
  { name: "CHANGELOG.md", depth: 0, status: "M" },
  { name: "package.json", depth: 0, status: "M" },
];

export const retryLines: readonly DiffLine[] = [
  { kind: "hunk", text: "@@ -5,7 +5,14 @@ const baseDelay = 250;" },
  {
    kind: "ctx",
    n: 5,
    text: "export async function retry<T>(task: () => Promise<T>, attempts = 6) {",
  },
  { kind: "del", n: 6, text: "  return task();" },
  { kind: "add", n: 6, text: "  for (let attempt = 0; ; attempt++) {" },
  { kind: "add", n: 7, text: "    try {" },
  { kind: "add", n: 8, text: "      return await task();" },
  { kind: "add", n: 9, text: "    } catch (error) {" },
  {
    kind: "add",
    n: 10,
    text: "      if (attempt + 1 >= attempts) throw error;",
  },
  { kind: "add", n: 11, text: "      await sleep(baseDelay * 2 ** attempt);" },
  { kind: "add", n: 12, text: "    }" },
  { kind: "add", n: 13, text: "  }" },
  { kind: "ctx", n: 14, text: "}" },
  { kind: "ctx", n: 15, text: "" },
  { kind: "ctx", n: 16, text: "export function isRetryable(status: number) {" },
  { kind: "ctx", n: 17, text: "  return status === 429 || status >= 500;" },
  { kind: "ctx", n: 18, text: "}" },
];

const sleepLines: readonly DiffLine[] = [
  { kind: "add", n: 1, text: "export function sleep(ms: number) {" },
  {
    kind: "add",
    n: 2,
    text: "  return new Promise<void>((resolve) => setTimeout(resolve, ms));",
  },
  { kind: "add", n: 3, text: "}" },
  { kind: "add", n: 4, text: "" },
  { kind: "add", n: 5, text: "export function jitter(ms: number) {" },
  {
    kind: "add",
    n: 6,
    text: "  return Math.round(ms * (0.5 + Math.random() / 2));",
  },
  { kind: "add", n: 7, text: "}" },
];

const testLines: readonly DiffLine[] = [
  { kind: "hunk", text: '@@ -18,6 +18,14 @@ describe("request", () => {' },
  { kind: "ctx", n: 18, text: '  it("returns parsed JSON", async () => {' },
  { kind: "ctx", n: 19, text: "    mockFetch(200, { ok: true });" },
  {
    kind: "ctx",
    n: 20,
    text: '    expect(await request("/health")).toEqual({ ok: true });',
  },
  { kind: "ctx", n: 21, text: "  });" },
  { kind: "add", n: 22, text: "" },
  {
    kind: "add",
    n: 23,
    text: '  it("retries a 503 before it succeeds", async () => {',
  },
  { kind: "add", n: 24, text: "    mockFetch(503).then(200, { ok: true });" },
  {
    kind: "add",
    n: 25,
    text: '    expect(await request("/health")).toEqual({ ok: true });',
  },
  { kind: "add", n: 26, text: "    expect(fetch).toHaveBeenCalledTimes(2);" },
  { kind: "add", n: 27, text: "  });" },
  { kind: "ctx", n: 28, text: "});" },
];

const retryTestLines: readonly DiffLine[] = [
  { kind: "add", n: 1, text: 'import { retry } from "../src/retry";' },
  { kind: "add", n: 2, text: "" },
  { kind: "add", n: 3, text: 'describe("retry", () => {' },
  {
    kind: "add",
    n: 4,
    text: '  it("gives up after six attempts", async () => {',
  },
  {
    kind: "add",
    n: 5,
    text: '    const task = vi.fn().mockRejectedValue(new Error("503"));',
  },
  {
    kind: "add",
    n: 6,
    text: "    await expect(retry(task)).rejects.toThrow();",
  },
  { kind: "add", n: 7, text: "    expect(task).toHaveBeenCalledTimes(6);" },
  { kind: "add", n: 8, text: "  });" },
  { kind: "add", n: 9, text: "});" },
];

async function script(a: Act<S>) {
  if (a.layout === "compact") {
    await a.wait(1200);
    await a.move({ at: "reviewed-retry", x: 0.6, y: 0.55 });
    await a.wait(200);
    await a.click();
    await a.set({ reviewed: ["http", "retry"], selected: "sleep" });
    await a.wait(1500);
    await a.move({ at: "reviewed-sleep", x: 0.6, y: 0.55 });
    await a.wait(200);
    await a.click();
    await a.set({ reviewed: done.reviewed, selected: "test" });
    await a.wait(400);
    await a.move("rest");
    return;
  }
  await a.wait(1400);
  await a.set({ pressed: true });
  await a.key("alt+m");
  await a.set({
    reviewed: ["http", "retry"],
    selected: "sleep",
    pressed: false,
  });
  await a.wait(1800);
  await a.move({ at: "reviewed-sleep", x: 0.6, y: 0.55 });
  await a.wait(250);
  await a.click();
  await a.set({ reviewed: done.reviewed, selected: "test" });
  await a.wait(500);
  await a.move("rest");
}

const WIDE = { width: 1150, height: 560 };
const COMPACT = { width: 358, height: 500 };

export function HeroDiff() {
  return (
    <Stage
      width={WIDE.width}
      height={WIDE.height}
      compact={{ below: 640, ...COMPACT }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      keys={{ wide: "top", compact: "bottom" }}
      cursor={{
        at: { wide: { x: 900, y: 330 }, compact: { x: 290, y: 380 } },
      }}
      className="dxl dx-hero"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        const reviewed = (id: FileId) => s.reviewed.includes(id);
        const count = s.reviewed.length;
        return (
          <>
            <div
              className="dx-app dx-app--flush"
              style={{ left: 0, top: 0, ...(compact ? COMPACT : WIDE) }}
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                {!compact && (
                  <span className="dx-icon-button">
                    <ListTree />
                  </span>
                )}
                <span className="dx-identity">
                  <span className="dx-identity-icon">
                    <GitBranch />
                  </span>
                  <span className="dx-identity-title">retry-backoff</span>
                  {!compact && (
                    <span className="dx-identity-meta">
                      · acme/api · Claude Code on laptop
                    </span>
                  )}
                </span>
                {!compact && (
                  <span className="dx-header-actions">
                    <span className="dx-button" data-variant="ghost">
                      <RefreshCw />
                      Refresh
                    </span>
                  </span>
                )}
              </div>
              <div className="dx-body">
                {!compact && (
                  <aside className="dx-sidebar" style={{ width: 290 }}>
                    <div className="dx-tabs">
                      <span className="dx-tab" data-selected>
                        Changes <span className="dx-count">{TOTAL}</span>
                      </span>
                      <span className="dx-tab">
                        Review <span className="dx-count">0</span>
                      </span>
                      <span className="dx-icon-button dx-tabs-end">
                        <ChevronsDownUp />
                      </span>
                    </div>
                    <div className="dx-filter-row">
                      <div className="dx-field">
                        <Search />
                        Filter files…
                        <span className="dx-kbd">Alt+/</span>
                      </div>
                      <span className="dx-square-button">
                        <EyeOff />
                      </span>
                    </div>
                    <div className="dx-tree">
                      {tree.map((entry) => (
                        <TreeNode
                          key={entry.name}
                          entry={entry}
                          selected={entry.id === s.selected}
                          reviewed={
                            entry.id !== undefined &&
                            entry.id !== "test" &&
                            reviewed(entry.id)
                          }
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
                        <span>{TOTAL}</span>
                      </div>
                      <div className="dx-summary-line">
                        <span>Additions</span>
                        <span className="dx-plus">+41</span>
                      </div>
                      <div className="dx-summary-line">
                        <span>Deletions</span>
                        <span className="dx-minus">-6</span>
                      </div>
                      <div className="dx-summary-bar">
                        <span style={{ width: "87%" }} />
                      </div>
                      <div className="dx-summary-line dx-summary-progress">
                        <span>
                          <Roll>{`${count} of ${TOTAL} reviewed`}</Roll>
                        </span>
                        <span className="dx-button" data-variant="outline">
                          Reset reviewed
                        </span>
                      </div>
                    </div>
                    <div className="dx-statusbar">
                      <span className="dx-live" />
                      Watching acme/api
                    </div>
                  </aside>
                )}
                <main className="dx-main">
                  {!compact && (
                    <div className="dx-toolbar">
                      <span className="dx-picker">
                        Branch changes
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
                        <span className="dx-icon-button">
                          <Settings2 />
                        </span>
                      </span>
                    </div>
                  )}
                  <div className="dx-canvas">
                    <DiffFile
                      path="src/retry.ts"
                      status="M"
                      plus={8}
                      minus={1}
                      lines={retryLines}
                      reviewed={reviewed("retry")}
                      target="reviewed-retry"
                      compact={compact}
                    />
                    <DiffFile
                      path="src/sleep.ts"
                      status="A"
                      plus={7}
                      minus={0}
                      lines={sleepLines}
                      reviewed={reviewed("sleep")}
                      target="reviewed-sleep"
                      compact={compact}
                    />
                    <DiffFile
                      path="test/http.test.ts"
                      status="M"
                      plus={9}
                      minus={0}
                      lines={testLines}
                      reviewed={false}
                      compact={compact}
                    />
                    <DiffFile
                      path="test/retry.test.ts"
                      status="A"
                      plus={9}
                      minus={0}
                      lines={retryTestLines}
                      reviewed={false}
                      compact={compact}
                    />
                  </div>
                  {compact ? (
                    <div className="dx-statusbar">
                      <Roll>{`${count} of ${TOTAL} reviewed`}</Roll>
                    </div>
                  ) : (
                    <div className="dx-statusbar dx-shortcuts">
                      <span className="dx-kbd">Alt+J</span>
                      <span className="dx-kbd">Alt+K</span>
                      files
                      <span className="dx-kbd">Alt+[</span>
                      <span className="dx-kbd">Alt+]</span>
                      changes
                      <span className="dx-kbd" data-on={s.pressed || undefined}>
                        Alt+M
                      </span>
                      reviewed
                      <span className="dx-kbd">Alt+/</span>
                      filter
                    </div>
                  )}
                </main>
              </div>
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 290 : 900,
                top: compact ? 380 : 330,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}

function DiffFile({
  path,
  status,
  plus,
  minus,
  lines,
  reviewed,
  target,
  compact,
}: {
  path: string;
  status: "M" | "A";
  plus: number;
  minus: number;
  lines: readonly DiffLine[];
  reviewed: boolean;
  target?: string;
  compact: boolean;
}) {
  return (
    <section className="dx-file" data-collapsed={reviewed || undefined}>
      <FileHeader
        path={compact ? (path.split("/").pop() ?? path) : path}
        status={status}
        plus={plus}
        minus={minus}
        reviewed={reviewed}
        {...(target !== undefined && { reviewedTarget: target })}
      />
      <Collapse open={!reviewed}>
        <div className="dx-rows">
          {lines.map((line, index) => (
            <DiffRow key={index} line={line} />
          ))}
        </div>
      </Collapse>
    </section>
  );
}
