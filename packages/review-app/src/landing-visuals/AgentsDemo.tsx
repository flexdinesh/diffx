import { Bot, ChevronDown, Circle, CircleCheck } from "lucide-react";
import type { ReactNode } from "react";
import { Collapse, Roll, Stage, type Act } from "@diffx/stageshow";
import { DiffRow, FileHeader, type DiffLine } from "./parts.tsx";
import { COMMENT } from "./CommentsDemo.tsx";
import "./landing-visuals.css";

// Agents over MCP: a coding agent finds the open thread, fixes the line,
// replies and resolves it, and the review shows each step land: the new
// lines, the reply, the resolved thread.

const PROMPT = "Address the open diffx comments";
const REPLY = "Capped the delay at 5 seconds and added jitter.";

type S = { typed: string; step: number };

const initial: S = { typed: "", step: 0 };
const done: S = { typed: PROMPT, step: 4 };

const before: DiffLine = {
  kind: "add",
  n: 11,
  text: "      await sleep(baseDelay * 2 ** attempt);",
};
const after: readonly DiffLine[] = [
  {
    kind: "add",
    n: 11,
    text: "      const ms = Math.min(2 ** attempt * 250, 5000);",
  },
  { kind: "add", n: 12, text: "      await sleep(jitter(ms));" },
];

async function script(a: Act<S>) {
  await a.wait(700);
  await a.type("typed", PROMPT);
  await a.wait(350);
  await a.key("enter");
  for (let step = 1; step <= 4; step++) {
    await a.wait(step === 1 ? 500 : 1100);
    await a.set({ step });
  }
}

const WIDE = { width: 670, height: 460 };
const COMPACT = { width: 358, height: 470 };

export function AgentsDemo() {
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
      className="dxl dx-agents"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        return (
          <>
            <div
              className="dx-app"
              style={
                compact
                  ? { left: 16, top: 190, width: 326, height: 400 }
                  : { left: 32, top: 218, width: 606, height: 300 }
              }
            >
              <div className="dx-canvas">
                <section className="dx-file">
                  <FileHeader
                    path={compact ? "retry.ts" : "src/retry.ts"}
                    status="M"
                    plus={s.step >= 2 ? 9 : 8}
                    minus={1}
                    reviewed={false}
                  />
                  <div className="dx-rows">
                    <Collapse open={s.step < 2}>
                      <DiffRow line={before} anchored />
                    </Collapse>
                    <Collapse open={s.step >= 2}>
                      {after.map((line, index) => (
                        <DiffRow key={index} line={line} anchored />
                      ))}
                    </Collapse>
                    <div className="dx-annotation">
                      <Thread step={s.step} />
                    </div>
                  </div>
                </section>
              </div>
            </div>
            <div
              className="dx-app dx-terminal"
              style={
                compact
                  ? { left: 16, top: 16, width: 326 }
                  : { left: 32, top: 32, width: 606 }
              }
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
          </>
        );
      }}
    </Stage>
  );
}

/** The thread as the review shows it, reply and resolution arriving. */
function Thread({ step }: { step: number }) {
  const resolved = step >= 4;
  return (
    <div className="dx-comment dx-thread">
      <div className="dx-comment-head">
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
      <div className="dx-comment-body">{COMMENT}</div>
      <Collapse open={step >= 3} reveal="drop">
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

function Step({
  show,
  tool,
  mcp,
  children,
}: {
  show: boolean;
  tool: string;
  mcp?: boolean;
  children?: ReactNode;
}) {
  return (
    <Collapse open={show}>
      <div className="dx-step">
        <span className="dx-dot">⏺</span>
        <span>
          <span className="dx-tool">
            {mcp && <span className="dx-mcp">diffx · </span>}
            {tool}
          </span>
          {children !== undefined && (
            <span className="dx-result">{children}</span>
          )}
        </span>
      </div>
    </Collapse>
  );
}
