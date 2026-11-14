"use client";
import { Check, GitPullRequest, Inbox } from "lucide-react";
import { Collapse, Count, Stage, type Act } from "../stageshow/index.ts";
import { BrandMark, GitHubMark } from "./parts.tsx";
import "./landing.css";

// GitHub: connect the account, follow a repository, and its open pull
// requests arrive on the Reviews bench on their own.

type Repo = "api" | "web";
type S = { connected: boolean; followed: Repo[] };

const initial: S = { connected: false, followed: [] };
const done: S = { connected: true, followed: ["api", "web"] };

const pulls: readonly {
  id: string;
  repo: Repo;
  number: number;
  title: string;
  author: string;
}[] = [
  {
    id: "482",
    repo: "api",
    number: 482,
    title: "Add retry backoff",
    author: "@maya",
  },
  {
    id: "479",
    repo: "api",
    number: 479,
    title: "Cap upload size",
    author: "@lee",
  },
  {
    id: "91",
    repo: "web",
    number: 91,
    title: "Fix nav overflow",
    author: "@sam",
  },
];

async function script(a: Act<S>) {
  await a.wait(900);
  await a.move({ at: "connect", x: 0.7, y: 0.6 });
  await a.wait(200);
  await a.click();
  await a.set({ connected: true });
  await a.wait(1100);
  await a.move({ at: "follow-api", x: 0.5, y: 0.5 });
  await a.wait(200);
  await a.click();
  await a.set({ followed: ["api"] });
  await a.wait(1400);
  await a.move({ at: "follow-web", x: 0.5, y: 0.5 });
  await a.wait(200);
  await a.click();
  await a.set({ followed: ["api", "web"] });
  await a.wait(600);
  await a.move("rest");
}

export function GitHubDemo() {
  return (
    <Stage
      width={620}
      height={420}
      compact={{ below: 520, width: 358, height: 440 }}
      initial={initial}
      poster={done}
      script={script}
      play="once"
      cursor={{
        at: { wide: { x: 210, y: 372 }, compact: { x: 300, y: 250 } },
      }}
      className="dxl dx-github"
    >
      {(s, _rect, layout) => {
        const compact = layout === "compact";
        const followed = (repo: Repo) => s.followed.includes(repo);
        const arrived = pulls.filter((pull) => followed(pull.repo)).length;
        return (
          <>
            <div
              className="dx-app"
              style={
                compact
                  ? { left: 16, top: 16, width: 326, height: 470 }
                  : { left: 28, top: 28, width: 440, height: 430 }
              }
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                <span className="dx-crumb dx-crumb--title">Settings</span>
              </div>
              <div className="dx-body">
                {!compact && (
                  <aside
                    className="dx-sidebar dx-settings-nav"
                    style={{ width: 132 }}
                  >
                    <NavItem label="Account" />
                    <NavItem label="Notifications" />
                    <NavItem label="GitHub" selected />
                    <NavItem label="API tokens" />
                    <NavItem label="Agents" />
                  </aside>
                )}
                <main className="dx-main dx-settings">
                  <div className="dx-settings-title">GitHub</div>
                  <ol className="dx-steps">
                    <li>
                      <div className="dx-step-title">
                        Connect your GitHub account
                      </div>
                      <Collapse open={!s.connected}>
                        <div className="dx-step-copy">
                          Lets diffx check which repositories you can read.
                        </div>
                        <span
                          className="dx-button"
                          data-variant="primary"
                          data-size="md"
                          data-target="connect"
                        >
                          <GitHubMark size={14} />
                          Connect GitHub
                        </span>
                      </Collapse>
                      <Collapse open={s.connected}>
                        <div className="dx-step-copy">
                          Connected as <strong>@maya</strong>. Pull requests of
                          followed repositories update from GitHub events.
                        </div>
                      </Collapse>
                    </li>
                    <li>
                      <div className="dx-step-title">Follow repositories</div>
                      <Collapse open={s.connected}>
                        <div className="dx-repos">
                          <RepoRow
                            name="acme/api"
                            checked={followed("api")}
                            target="follow-api"
                          />
                          <RepoRow
                            name="acme/web"
                            checked={followed("web")}
                            target="follow-web"
                          />
                          {!compact && (
                            <RepoRow name="acme/docs" checked={false} />
                          )}
                        </div>
                      </Collapse>
                    </li>
                  </ol>
                </main>
              </div>
            </div>
            <div
              className="dx-satellite dx-mini-bench"
              data-show={arrived > 0 || undefined}
              style={
                compact
                  ? { left: 40, top: 278, width: 286 }
                  : { left: 336, top: 258, width: 256 }
              }
            >
              <div className="dx-mini-head">
                <Inbox />
                Needs attention
                <span className="dx-count dx-end">
                  <Count value={arrived} />
                </span>
              </div>
              {pulls.map((pull) => (
                <Collapse
                  key={pull.id}
                  open={followed(pull.repo)}
                  reveal="drop"
                >
                  <div className="dx-mini-row" data-unread>
                    <span className="dx-unread" />
                    <GitPullRequest />
                    <span className="dx-mini-number">#{pull.number}</span>
                    <span className="dx-mini-title">{pull.title}</span>
                    <span className="dx-mini-meta">{pull.author}</span>
                  </div>
                </Collapse>
              ))}
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 300 : 210,
                top: compact ? 250 : 372,
              }}
            />
          </>
        );
      }}
    </Stage>
  );
}

function NavItem({ label, selected }: { label: string; selected?: boolean }) {
  return (
    <div className="dx-facet" data-selected={selected || undefined}>
      <span className="dx-facet-name">{label}</span>
    </div>
  );
}

function RepoRow({
  name,
  checked,
  target,
}: {
  name: string;
  checked: boolean;
  target?: string;
}) {
  return (
    <div className="dx-repo">
      <span
        className="dx-check"
        data-on={checked || undefined}
        data-target={target}
      >
        <Check />
      </span>
      <span>{name}</span>
    </div>
  );
}
