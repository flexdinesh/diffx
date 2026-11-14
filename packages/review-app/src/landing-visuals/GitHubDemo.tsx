import { Check, GitPullRequest, Inbox, Lock } from "lucide-react";
import { Collapse, Count, Stage, type Act } from "@diffx/stageshow";
import { BrandMark } from "./parts.tsx";
import "./landing-visuals.css";

// Settings > GitHub: connect the account, follow repositories, and their open
// pull requests arrive in the bench's Needs attention on their own.

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

const WIDE = { width: 670, height: 460 };
const COMPACT = { width: 358, height: 560 };

export function GitHubDemo() {
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
        at: { wide: { x: 96, y: 330 }, compact: { x: 290, y: 200 } },
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
                  ? { left: 16, top: 16, width: 326, height: 600 }
                  : { left: 32, top: 32, width: 480, height: 500 }
              }
            >
              <div className="dx-header">
                <BrandMark />
                <span className="dx-divider" />
                <span className="dx-crumb dx-crumb--title">Settings</span>
              </div>
              <div className="dx-body">
                {!compact && (
                  <nav className="dx-settings-nav">
                    <NavItem label="Account" />
                    <NavItem label="Notifications" />
                    <NavItem label="GitHub" selected />
                    <NavItem label="API tokens" />
                    <NavItem label="Agents" />
                  </nav>
                )}
                <main className="dx-main dx-settings">
                  <div className="dx-settings-title">GitHub</div>
                  <div className="dx-step-title">
                    Connect your GitHub account
                  </div>
                  <div className="dx-swap dx-connect">
                    <div data-show={!s.connected || undefined}>
                      <div className="dx-step-copy">
                        Lets diffx check which repositories you can read.
                      </div>
                      <span
                        className="dx-button"
                        data-variant="primary"
                        data-size="md"
                        data-target="connect"
                      >
                        Connect GitHub
                      </span>
                    </div>
                    <div data-show={s.connected || undefined}>
                      <div className="dx-step-copy">
                        Connected as <strong>@maya</strong>. Pull requests of
                        followed repositories update from GitHub events.
                      </div>
                    </div>
                  </div>
                  <div className="dx-step-title">Repositories</div>
                  <div className="dx-step-copy">
                    Follow repositories to review their open pull requests,
                    drafts included.
                  </div>
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
                      <RepoRow name="acme/billing" checked={false} locked />
                      <RepoRow name="acme/docs" checked={false} />
                    </div>
                  </Collapse>
                </main>
              </div>
            </div>
            <div
              className="dx-satellite dx-mini-bench"
              data-show={arrived > 0 || undefined}
              style={
                compact
                  ? { left: 32, top: 412, width: 310 }
                  : { left: 368, top: 296, width: 270 }
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
                    <span className="dx-mini-title">{pull.title}</span>
                    <span className="dx-mini-number">#{pull.number}</span>
                  </div>
                </Collapse>
              ))}
            </div>
            <span
              data-target="rest"
              style={{
                position: "absolute",
                left: compact ? 290 : 96,
                top: compact ? 200 : 330,
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
    <span className="dx-nav-item" data-selected={selected || undefined}>
      {label}
    </span>
  );
}

function RepoRow({
  name,
  checked,
  locked,
  target,
}: {
  name: string;
  checked: boolean;
  locked?: boolean;
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
      {locked && <Lock />}
    </div>
  );
}
