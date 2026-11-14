import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  Circle,
  CircleCheck,
  Copy,
  Link2,
  Pencil,
  Trash2,
} from "lucide-react";
import { Collapse, Draft, Stage, type Act } from "@diffx/stageshow";
import { DiffRow, FileHeader, type DiffLine } from "./parts.tsx";
import { retryLines } from "./HeroDiff.tsx";
import "./landing-visuals.css";

// Comments: hover a changed line, open the composer from the gutter, type,
// and the draft becomes the saved comment in place, darkening its line.

export const COMMENT = "Cap this delay at 5 seconds and add jitter.";
const LINE = 11;

type S = {
  hover: boolean;
  open: boolean;
  draft: string;
  editing: boolean;
  saved: boolean;
};

const initial: S = {
  hover: false,
  open: false,
  draft: "",
  editing: true,
  saved: false,
};
const done: S = {
  hover: false,
  open: true,
  draft: COMMENT,
  editing: false,
  saved: true,
};

async function script(a: Act<S>) {
  await a.wait(900);
  await a.move({ at: "line", x: 0.035, y: 0.5 });
  await a.set({ hover: true });
  await a.wait(350);
  await a.click();
  await a.set({ open: true });
  await a.wait(500);
  await a.type("draft", COMMENT);
  await a.wait(400);
  await a.move({ at: "submit", x: 0.6, y: 0.55 });
  await a.wait(200);
  await a.click();
  await a.set({ editing: false, saved: true, hover: false });
  await a.wait(500);
  await a.move("rest");
}

/** retry.ts from the loop to line 11, the thread, then what follows; the
 * file runs on past the window's foot in every state. */
const head = retryLines.slice(3, 8);
const anchor = retryLines[8];
const more: readonly DiffLine[] = [
  { kind: "ctx", n: 19, text: "" },
  { kind: "ctx", n: 20, text: "export function backoff(attempt: number) {" },
  { kind: "ctx", n: 21, text: "  return baseDelay * 2 ** attempt;" },
  { kind: "ctx", n: 22, text: "}" },
];
const tail = [...retryLines.slice(9), ...more];

const WIDE = { width: 670, height: 460 };
const COMPACT = { width: 358, height: 460 };

export function CommentsDemo() {
  return (
    <Stage
      width={WIDE.width}
      height={WIDE.height}
      compact={{ below: 520, ...COMPACT }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      cursor={{
        at: { wide: { x: 520, y: 150 }, compact: { x: 300, y: 150 } },
      }}
      className="dxl dx-comments"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        return (
          <>
            <div
              className="dx-app"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 480 }
                  : { left: 32, top: 32, width: 606, height: 500 }
              }
            >
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
                  </span>
                </div>
              )}
              <div className="dx-canvas">
                <section className="dx-file">
                  <FileHeader
                    path={compact ? "retry.ts" : "src/retry.ts"}
                    status="M"
                    plus={8}
                    minus={1}
                    reviewed={false}
                  />
                  <div className="dx-rows">
                    {head.map((line, index) => (
                      <DiffRow key={index} line={line} />
                    ))}
                    {anchor && (
                      <DiffRow
                        line={anchor}
                        target="line"
                        hovered={s.hover}
                        anchored={s.saved}
                        gutterAdd={s.hover}
                      />
                    )}
                    <Collapse open={s.open}>
                      <div className="dx-annotation">
                        <Thread s={s} compact={compact} />
                      </div>
                    </Collapse>
                    {tail.map((line, index) => (
                      <DiffRow key={index} line={line} />
                    ))}
                  </div>
                </section>
              </div>
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 300 : 520,
                top: 150,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}

/** The composer, then the saved thread in its place: the review's card. */
function Thread({ s, compact }: { s: S; compact: boolean }) {
  return (
    <div className="dx-comment">
      <div className="dx-swap">
        <div className="dx-comment-head" data-show={!s.saved || undefined}>
          <span className="dx-comment-label">New comment</span>
          <span className="dx-comment-loc">
            {compact ? "retry.ts" : "src/retry.ts"}:{LINE}
          </span>
          <span className="dx-comment-head-end">
            <Copy />
          </span>
        </div>
        <div className="dx-comment-head" data-show={s.saved || undefined}>
          <ChevronDown />
          <span className="dx-comment-meta">Line {LINE} · You</span>
          <span className="dx-comment-head-end">
            <span className="dx-badge">
              <Circle />
              Open
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
      <div className="dx-comment-actions dx-swap">
        <div data-show={!s.saved || undefined}>
          {!compact && <span className="dx-hint">Ctrl+Enter to comment</span>}
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
        <div data-show={s.saved || undefined}>
          <span className="dx-icon-button">
            <Copy />
          </span>
          <span className="dx-icon-button">
            <Link2 />
          </span>
          <span className="dx-icon-button">
            <Pencil />
          </span>
          <span className="dx-button" data-variant="ghost">
            Reply
          </span>
          <span className="dx-button dx-end" data-variant="outline">
            <CircleCheck />
            Resolve
          </span>
          <span className="dx-icon-button dx-danger">
            <Trash2 />
          </span>
        </div>
      </div>
    </div>
  );
}
