"use client";
import { Copy, Link2, Pencil, Trash2 } from "lucide-react";
import { Collapse, Draft, Stage, type Act } from "../stageshow/index.ts";
import { DiffRow, FileHeader, type DiffLine } from "./parts.tsx";
import { retryLines } from "./HeroDiff.tsx";
import "./landing.css";

// Comments: hover a changed line, open the composer from the gutter, type,
// and the draft becomes the saved comment in place, tinting its line.

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
  await a.move({ at: "submit", x: 0.7, y: 0.6 });
  await a.wait(200);
  await a.click();
  await a.set({ editing: false, saved: true, hover: false });
  await a.wait(500);
  await a.move("rest");
}

/** Lines 5 to 11 of retry.ts, then the comment, then what follows. The
 * file runs on past the card's foot in every state. */
const head = retryLines.slice(0, 8);
const anchor = retryLines[8];
const more: readonly DiffLine[] = [
  { kind: "ctx", n: 19, text: "" },
  { kind: "ctx", n: 20, text: "export function backoff(attempt: number) {" },
  { kind: "ctx", n: 21, text: "  return baseDelay * 2 ** attempt;" },
  { kind: "ctx", n: 22, text: "}" },
];
const tail = [...retryLines.slice(9), ...more];

export function CommentsDemo() {
  return (
    <Stage
      width={620}
      height={448}
      compact={{ below: 520, width: 358, height: 448 }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      cursor={{
        at: { wide: { x: 480, y: 165 }, compact: { x: 300, y: 165 } },
      }}
      className="dxl dx-comments"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        return (
          <>
            <section
              className="dx-file dx-figure-file"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 416 }
                  : { left: 28, top: 28, width: 564, height: 392 }
              }
            >
              <FileHeader
                path={compact ? "retry.ts" : "src/retry.ts"}
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
                    <div className="dx-comment">
                      <div className="dx-swap">
                        <div
                          className="dx-comment-head"
                          data-show={!s.saved || undefined}
                        >
                          <span className="dx-comment-title">New comment</span>
                          <span className="dx-comment-loc">
                            src/retry.ts:{LINE}
                          </span>
                        </div>
                        <div
                          className="dx-comment-head"
                          data-show={s.saved || undefined}
                        >
                          <span className="dx-comment-title">Line {LINE}</span>
                          <span className="dx-comment-meta">· You</span>
                        </div>
                      </div>
                      <Draft
                        value={s.draft}
                        full={COMMENT}
                        editing={s.editing}
                        placeholder="Leave a comment"
                      />
                      <div className="dx-comment-actions dx-swap">
                        <div data-show={!s.saved || undefined}>
                          {!compact && (
                            <span className="dx-hint">⌘Enter to comment</span>
                          )}
                          <span className="dx-button dx-end">Cancel</span>
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
                          <span className="dx-icon-button">
                            <Trash2 />
                          </span>
                          <span className="dx-button">Reply</span>
                          <span
                            className="dx-button dx-end"
                            data-variant="outline"
                          >
                            Resolve
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>
                </Collapse>
                {tail.map((line, index) => (
                  <DiffRow key={index} line={line} />
                ))}
              </div>
            </section>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 300 : 480,
                top: 165,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}
