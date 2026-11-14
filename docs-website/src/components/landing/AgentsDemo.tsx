"use client";
import { Bot, CircleCheck } from "lucide-react";
import type { ReactNode } from "react";
import { Collapse, Roll, Stage, type Act } from "../stageshow/index.ts";
import { DiffRow, type DiffLine } from "./parts.tsx";
import { COMMENT } from "./CommentsDemo.tsx";
import "./landing.css";

// Agents over MCP: a coding agent finds the open thread, fixes the code,
// replies and resolves it; the review shows each step as it commits.

const PROMPT = "Address the open diffx comments on this branch";
const REPLY = "Capped the delay at 5 seconds and added jitter.";

type S = { typed: string; step: number };

const initial: S = { typed: "", step: 0 };
const done: S = { typed: PROMPT, step: 6 };

const line: DiffLine = {
  kind: "add",
  n: 11,
  text: "      await sleep(baseDelay * 2 ** attempt);",
};

async function script(a: Act<S>) {
  await a.wait(700);
  await a.type("typed", PROMPT);
  await a.wait(350);
  await a.key("enter");
  for (let step = 1; step <= 6; step++) {
    await a.set({ step });
    await a.wait(step === 4 || step === 5 ? 1000 : 700);
  }
}

export function AgentsDemo() {
  return (
    <Stage
      width={620}
      height={420}
      compact={{ below: 520, width: 358, height: 610 }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      keys={{ wide: "bottom-left", compact: "bottom-left" }}
      className="dxl dx-agents"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        return (
          <>
            <div
              className="dx-terminal"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 620 }
                  : { left: 28, top: 28, width: 460, height: 420 }
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
                <Step show={s.step >= 1} tool="diffx · find_feedback">
                  1 open thread · src/retry.ts:11
                </Step>
                <Step show={s.step >= 2} tool="Update(src/retry.ts)">
                  <span className="dx-term-add">+2</span>{" "}
                  <span className="dx-term-del">−1</span>
                </Step>
                <Step
                  show={s.step >= 3}
                  tool="diffx · reply_to_review_comment"
                />
                <Step show={s.step >= 4} tool="diffx · resolve_review_comment">
                  Resolved
                </Step>
                <Step show={s.step >= 6 && !compact} plain>
                  {REPLY} The thread is resolved.
                </Step>
              </div>
            </div>
            <div
              className="dx-satellite dx-thread"
              style={
                compact
                  ? { left: 28, bottom: 28, width: 302 }
                  : { left: 330, top: 96, width: 262 }
              }
            >
              <div className="dx-thread-path">src/retry.ts</div>
              <div className="dx-rows">
                <DiffRow line={line} anchored />
              </div>
              <div className="dx-annotation" style={{ paddingLeft: 12 }}>
                <div className="dx-comment">
                  <div className="dx-comment-head">
                    <span className="dx-comment-title">Line 11</span>
                    <span className="dx-comment-meta">· You</span>
                    <span className="dx-comment-head-end">
                      <span
                        className="dx-badge"
                        data-tone={s.step >= 5 ? "success" : undefined}
                      >
                        <Roll>{s.step >= 5 ? "Resolved" : "Open"}</Roll>
                      </span>
                    </span>
                  </div>
                  <div className="dx-comment-body">{COMMENT}</div>
                  <Collapse open={s.step >= 3} reveal="drop">
                    <div className="dx-reply">
                      <div className="dx-reply-meta">
                        <Bot />
                        <span className="dx-reply-author">Claude Code</span>·
                        now
                      </div>
                      <div className="dx-comment-body">{REPLY}</div>
                    </div>
                  </Collapse>
                  <Collapse open={s.step >= 5}>
                    <div className="dx-resolved-line">
                      <CircleCheck />
                      Resolved by Claude Code · now
                    </div>
                  </Collapse>
                </div>
              </div>
            </div>
          </>
        );
      }}
    </Stage>
  );
}

function Step({
  show,
  tool,
  plain,
  children,
}: {
  show: boolean;
  tool?: string;
  plain?: boolean;
  children?: ReactNode;
}) {
  return (
    <Collapse open={show}>
      <div className="dx-step" data-plain={plain || undefined}>
        <span className="dx-dot">⏺</span>
        <span>
          {tool !== undefined && (
            <span className="dx-tool">
              {tool}
              {tool.startsWith("diffx") && (
                <span className="dx-mcp"> (MCP)</span>
              )}
            </span>
          )}
          {plain && children}
          {!plain && children !== undefined && (
            <span className="dx-result">
              <span className="dx-elbow">⎿</span>
              {children}
            </span>
          )}
        </span>
      </div>
    </Collapse>
  );
}
