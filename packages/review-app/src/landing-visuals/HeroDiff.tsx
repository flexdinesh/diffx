import {
  CheckCheck,
  ChevronDown,
  ChevronLeft,
  ChevronUp,
  FolderGit2,
  GitBranch,
  ListFilter,
  PanelLeft,
  Search,
} from "lucide-react";
import { Collapse, Stage, type Act } from "@diffx/stageshow";
import {
  BrandMark,
  DiffRow,
  FileHeader,
  TreeNode,
  type DiffLine,
  type TreeEntry,
} from "./parts.tsx";
import "./landing-visuals.css";

// The diff view: a file marked reviewed collapses to its header and the next
// file rises, from the keyboard (Alt+M) and from the header's Reviewed toggle.

type FileId = "http" | "retry" | "sleep";
type S = { reviewed: FileId[]; selected: FileId };

const initial: S = { reviewed: ["http"], selected: "retry" };
const done: S = { reviewed: ["http", "retry", "sleep"], selected: "sleep" };

const tree: readonly (TreeEntry & { id?: FileId })[] = [
  { name: "docs", depth: 0, folder: "closed" },
  { name: "src", depth: 0, folder: "open" },
  { name: "http.ts", depth: 1, status: "M", id: "http" },
  { name: "retry.ts", depth: 1, status: "M", id: "retry" },
  { name: "sleep.ts", depth: 1, status: "A", id: "sleep" },
  { name: "test", depth: 0, folder: "open" },
  { name: "http.test.ts", depth: 1, status: "M" },
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

async function script(a: Act<S>) {
  await a.wait(1400);
  await a.key("alt+m");
  await a.set({ reviewed: ["http", "retry"], selected: "sleep" });
  await a.wait(1800);
  await a.move({ at: "reviewed-sleep", x: 0.7, y: 0.6 });
  await a.wait(250);
  await a.click();
  await a.set({ reviewed: done.reviewed });
  await a.wait(500);
  await a.move("rest");
}

export function HeroDiff() {
  return (
    <Stage
      width={1104}
      height={460}
      compact={{ below: 640, width: 358, height: 420 }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      cursor={{
        at: { wide: { x: 860, y: 300 }, compact: { x: 300, y: 330 } },
      }}
      className="dxl dx-hero"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        const reviewed = (id: FileId) => s.reviewed.includes(id);
        return (
          <>
            <div
              className="dx-app"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 440 }
                  : { left: 56, top: 44, width: 992, height: 470 }
              }
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                {!compact && (
                  <>
                    <span className="dx-crumb">
                      <ChevronLeft />
                      Reviews
                    </span>
                    <span className="dx-sep">/</span>
                    <span className="dx-crumb dx-crumb--title">
                      <FolderGit2 />
                      acme/api
                      <ChevronDown />
                    </span>
                  </>
                )}
                <span className="dx-crumb dx-crumb--mono">
                  <GitBranch />
                  retry-backoff
                  {!compact && <ChevronDown />}
                </span>
                <span className="dx-header-actions">
                  <span className="dx-icon-button">
                    <Search />
                  </span>
                  {!compact && (
                    <>
                      <span className="dx-icon-button">
                        <CheckCheck />
                      </span>
                      <span className="dx-icon-button">
                        <ChevronUp />
                      </span>
                      <span className="dx-stepper">1 of 3</span>
                      <span className="dx-icon-button">
                        <ChevronDown />
                      </span>
                    </>
                  )}
                </span>
              </div>
              <div className="dx-body">
                {!compact && (
                  <aside className="dx-sidebar" style={{ width: 248 }}>
                    <div className="dx-tabs">
                      <span className="dx-tab" data-selected>
                        Changes <span className="dx-count">7</span>
                      </span>
                      <span className="dx-tab">
                        Review <span className="dx-count">0</span>
                      </span>
                    </div>
                    <div className="dx-field">
                      <ListFilter />
                      Filter files…
                      <span className="dx-kbd">Alt+/</span>
                    </div>
                    <div className="dx-tree">
                      {tree.map((entry) => (
                        <TreeNode
                          key={entry.name}
                          entry={entry}
                          selected={entry.id === s.selected}
                          reviewed={entry.id ? reviewed(entry.id) : false}
                        />
                      ))}
                    </div>
                  </aside>
                )}
                <main className="dx-main">
                  {!compact && (
                    <div className="dx-toolbar">
                      <span className="dx-icon-button" style={{ width: 26 }}>
                        <PanelLeft />
                      </span>
                      <span className="dx-picker">
                        Uncommitted
                        <ChevronDown />
                      </span>
                      <span className="dx-segmented">
                        <span data-selected>All</span>
                        <span>Staged</span>
                        <span>Unstaged</span>
                      </span>
                      <span className="dx-toolbar-end">
                        <span className="dx-segmented">
                          <span>Split</span>
                          <span data-selected>Unified</span>
                        </span>
                      </span>
                    </div>
                  )}
                  <div className="dx-canvas">
                    <DiffFile
                      path="src/retry.ts"
                      plus={8}
                      minus={1}
                      lines={retryLines}
                      reviewed={reviewed("retry")}
                      compact={compact}
                    />
                    <DiffFile
                      path="src/sleep.ts"
                      plus={7}
                      minus={0}
                      lines={sleepLines}
                      reviewed={reviewed("sleep")}
                      target="reviewed-sleep"
                      compact={compact}
                    />
                    <DiffFile
                      path="test/http.test.ts"
                      plus={9}
                      minus={0}
                      lines={testLines}
                      reviewed={false}
                      compact={compact}
                    />
                  </div>
                </main>
              </div>
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 300 : 860,
                top: compact ? 330 : 300,
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
  plus,
  minus,
  lines,
  reviewed,
  target,
  compact,
}: {
  path: string;
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
