import { ArrowRightIcon, GitPullRequestIcon } from "lucide-react";
import {
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import { BrandMark } from "@diffx/ui/components/brand-mark";
import { ErrorState } from "@diffx/ui/components/error-state";
import { LinkButton } from "@diffx/ui/components/link-button";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { navigate } from "./identity-context.tsx";
import {
  demoPullRequests,
  parsePullRequestInput,
  pullRequestName,
  pullRequestPath,
} from "./public-pull-request.ts";

const invalidLink =
  "Enter a GitHub pull request link, like github.com/owner/repo/pull/123.";
const docsUrl = "https://docs.diffx.app";
const repositoryUrl = "https://github.com/flexdinesh/diffx";

/** In-app links keep client routing; the URL stays a real href. */
function appLink(to: string) {
  return {
    href: to,
    onClick(event: MouseEvent<HTMLAnchorElement>) {
      event.preventDefault();
      navigate(to);
    },
  };
}

/** The landing figures, one per band; tests may pass stand-ins. */
export interface LandingVisuals {
  hero: ReactNode;
  bench: ReactNode;
  github: ReactNode;
  comments: ReactNode;
  agents: ReactNode;
}

/**
 * The signed-out home on a remote server: one boxed column that says what
 * diffx does, shows a review being read in the app's own window, then one band
 * per feature with its copy beside an animated figure. With public pull
 * request previews on, pasting a link is the primary action; otherwise
 * "Go to app" is.
 */
export function Landing({
  previews,
  visuals,
}: {
  previews: boolean;
  visuals: LandingVisuals;
}) {
  return (
    <div className="landing">
      <div className="landing-box">
        <header className="landing-topbar">
          <a className="brand landing-topbar-brand" href="/">
            <BrandMark />
            diffx
          </a>
          <p className="landing-motto">A review bench for your changes.</p>
          <nav className="landing-topbar-nav" aria-label="diffx">
            <a href={docsUrl}>Docs</a>
            <a href={repositoryUrl}>GitHub</a>
            <a className="landing-topbar-app" {...appLink("/signin")}>
              Go to app
              <ArrowRightIcon aria-hidden="true" />
            </a>
          </nav>
        </header>
        <main>
          <section
            className="landing-band landing-hero"
            aria-labelledby="landing-title"
          >
            <h1 id="landing-title" className="landing-display">
              <span>Review the diff.</span>{" "}
              <span>Hand the fixes to your agent.</span>
            </h1>
            <p className="landing-lede">
              diffx opens your Git changes as a readable diff in the browser,
              keeps your review comments, and turns them into instructions a
              coding agent can act on.
            </p>
            {previews ? (
              <PullRequestAddress />
            ) : (
              <div className="landing-actions">
                <LinkButton
                  variant="default"
                  size="lg"
                  touch
                  {...appLink("/signin")}
                >
                  Go to app
                </LinkButton>
                <LinkButton variant="outline" size="lg" touch href={docsUrl}>
                  Read the docs
                </LinkButton>
              </div>
            )}
          </section>
          <figure className="landing-band landing-showcase">
            <div aria-hidden="true">{visuals.hero}</div>
            <figcaption>
              A demo review, marking files reviewed. Not a real repository.
            </figcaption>
          </figure>
          <Feature id="landing-bench" visual={visuals.bench}>
            <h2 id="landing-bench">Every review on one bench</h2>
            <p>
              diffx opens on Reviews: each branch and pull request with changes,
              newest activity first. Needs attention collects what is new or
              updated since you last looked, including work your agents just
              pushed. Move with ↑ and ↓, peek with Space, open with Enter.
            </p>
            <a href={`${docsUrl}/reviewing-changes`}>Reviewing changes</a>
          </Feature>
          <Feature id="landing-github" visual={visuals.github}>
            <h2 id="landing-github">Pull requests arrive on their own</h2>
            <p>
              Connect GitHub and follow repositories. Their open pull requests,
              drafts included, appear on the bench beside your agents' reviews
              and refresh from GitHub events.
            </p>
            <a href={`${docsUrl}/github-pull-requests`}>GitHub pull requests</a>
          </Feature>
          <Feature id="landing-comments" visual={visuals.comments}>
            <h2 id="landing-comments">Comment on the lines that need work</h2>
            <p>
              Comment on any line or file. Comments and reviewed marks carry
              across new snapshots, re-anchor to code that moved, and copy out
              as agent-ready XML.
            </p>
            <a href={`${docsUrl}/reviewing-changes`}>
              Comments and reviewed marks
            </a>
          </Feature>
          <Feature id="landing-agents" visual={visuals.agents}>
            <h2 id="landing-agents">Let agents address the feedback</h2>
            <p>
              Connect Claude Code, Codex, Cursor or OpenCode to diffx over MCP.
              The agent finds the open threads, fixes each concern, replies with
              what changed and resolves it. The review shows every reply as it
              lands.
            </p>
            <a href={`${docsUrl}/mcp`}>MCP</a>
          </Feature>
        </main>
        <footer className="landing-band landing-footer">
          <span>diffx</span>
          <a href={docsUrl}>Docs</a>
          <a href={repositoryUrl}>GitHub</a>
          <a {...appLink("/signin")}>Sign in</a>
        </footer>
      </div>
    </div>
  );
}

/** Paste a public pull request link, or try a demo, to open its preview. */
function PullRequestAddress() {
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const field = useRef<HTMLInputElement>(null);
  function submit(event: FormEvent) {
    event.preventDefault();
    const pull = parsePullRequestInput(value);
    if (!pull) {
      setError(invalidLink);
      field.current?.focus();
      return;
    }
    navigate(pullRequestPath(pull));
  }
  return (
    <div className="landing-address">
      <div className="landing-address-field">
        <form className="account-form" onSubmit={submit} noValidate>
          <label htmlFor="pull-request-url">Pull request URL</label>
          <div className="flex gap-2">
            <Input
              ref={field}
              id="pull-request-url"
              name="url"
              type="text"
              inputMode="url"
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              placeholder="https://github.com/owner/repo/pull/123"
              touch
              className="flex-1"
              value={value}
              onChange={(event) => {
                setValue(event.target.value);
                if (error) setError("");
              }}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? "pull-request-url-error" : undefined}
            />
            <Button type="submit" touch>
              Open
            </Button>
          </div>
          {error && <ErrorState id="pull-request-url-error" message={error} />}
        </form>
        <p className="landing-hint">
          Any public GitHub pull request. Or replace{" "}
          <code className="font-mono">github.com</code> with{" "}
          <code className="font-mono">{window.location.host}</code> in its URL.
        </p>
      </div>
      <section aria-labelledby="demo-pull-requests">
        <h2 id="demo-pull-requests" className="landing-label">
          Try one
        </h2>
        <ul className="landing-pull-grid">
          {demoPullRequests.map((demo) => {
            const path = pullRequestPath(demo);
            return (
              <li key={path}>
                <a className="landing-pull-row" {...appLink(path)}>
                  <GitPullRequestIcon aria-hidden="true" />
                  <span className="landing-pull-name">
                    {pullRequestName(demo)}
                  </span>
                  <span className="landing-pull-number">#{demo.number}</span>
                  <span className="landing-pull-title">{demo.title}</span>
                </a>
              </li>
            );
          })}
        </ul>
      </section>
    </div>
  );
}

/** A feature band: copy on the left, its figure on the right. */
function Feature({
  id,
  visual,
  children,
}: {
  id: string;
  visual: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="landing-band landing-feature" aria-labelledby={id}>
      <div className="landing-feature-copy">{children}</div>
      <div className="landing-feature-visual" aria-hidden="true">
        {visual}
      </div>
    </section>
  );
}
