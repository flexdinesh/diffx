import {
  ArrowRightIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CopyIcon,
  GitPullRequestIcon,
} from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type ComponentProps,
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
  demoAgentReview,
  demoRepository,
  demoSummary,
  demoTree,
} from "./landing-demo.ts";
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
const installCommand =
  "curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh";

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

/**
 * The signed-out home on a remote server: one boxed column that says what
 * diffx does, shows a review in the app's own window, and what it hands an
 * agent. With public pull request previews on, pasting a link is the primary
 * action; otherwise "Go to app" is.
 */
export function Landing({
  previews,
  demoDiff,
}: {
  previews: boolean;
  /** The rendered demo review (LandingDemoDiff); tests may pass a stand-in. */
  demoDiff: ReactNode;
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
          <section
            className="landing-band landing-demo"
            aria-label="A diffx review"
          >
            <DemoWindow demoDiff={demoDiff} />
            <div className="landing-handoff">
              <p className="landing-handoff-intro">
                Copy the review into any coding agent. Agents also read the same
                comments over <a href={`${docsUrl}/mcp`}>MCP</a> and{" "}
                <a href={`${docsUrl}/webmcp`}>WebMCP</a>.
              </p>
              <AgentReview />
            </div>
          </section>
          <section className="landing-band landing-steps" aria-label="Steps">
            <ol>
              <li>
                <h2>
                  <span aria-hidden="true">01</span> Run
                </h2>
                <p>
                  Run <code>diffx</code> in any Git repository. Your browser
                  opens all, staged, or unstaged changes.
                </p>
              </li>
              <li>
                <h2>
                  <span aria-hidden="true">02</span> Review
                </h2>
                <p>
                  Read the diff, mark files reviewed, and comment on the lines
                  that need work.
                </p>
              </li>
              <li>
                <h2>
                  <span aria-hidden="true">03</span> Hand off
                </h2>
                <p>
                  Copy the review as agent-ready XML, or let agents read it over{" "}
                  <a href={`${docsUrl}/mcp`}>MCP</a> and{" "}
                  <a href={`${docsUrl}/webmcp`}>WebMCP</a>.
                </p>
              </li>
            </ol>
          </section>
          <section
            className="landing-band landing-close"
            aria-labelledby="landing-close-title"
          >
            <h2 id="landing-close-title" className="landing-heading">
              Also works locally, without signing in
            </h2>
            <p className="landing-copy">
              This hosted app needs a sign-in. The diffx CLI runs on your
              machine: install it, run it in any Git repository, and review your
              changes in the browser. No account needed.
            </p>
            <CommandRow command={installCommand} label="Copy install command" />
            <p className="landing-note">
              macOS and Linux. No Go or Node.js required.{" "}
              <a href={`${docsUrl}/getting-started`}>Getting started</a>
            </p>
          </section>
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

/**
 * The demo review inside a static copy of the app's window: header, file
 * tree, the Pierre diff and the status bar. Only the diff is content; the
 * chrome is decoration and stays out of the accessibility tree.
 */
function DemoWindow({ demoDiff }: { demoDiff: ReactNode }) {
  return (
    <figure className="landing-window">
      <div className="landing-window-frame">
        <div className="landing-window-header" aria-hidden="true">
          <BrandMark />
          <span className="landing-window-sep">/</span>
          <span className="landing-window-repo">{demoRepository.name}</span>
          <span className="landing-window-branch">{demoRepository.branch}</span>
          <span className="landing-window-controls">
            <span className="landing-window-segment">
              <span data-selected="">All</span>
              <span>Staged</span>
              <span>Unstaged</span>
            </span>
            <span className="landing-window-segment">
              <span>Split</span>
              <span data-selected="">Unified</span>
            </span>
          </span>
        </div>
        <div className="landing-window-body">
          <div className="landing-window-files" aria-hidden="true">
            <div className="landing-window-pane-header">
              <span>
                Changes{" "}
                <span className="landing-window-count">
                  {demoSummary.files}
                </span>
              </span>
              <kbd>Alt+/</kbd>
            </div>
            <ul className="landing-window-tree">
              {demoTree.map((entry) => (
                <li
                  key={`${entry.depth}-${entry.name}`}
                  data-kind={entry.kind}
                  data-selected={entry.selected ? "" : undefined}
                  style={{
                    paddingLeft: `calc(var(--space-3) + ${entry.depth} * var(--space-4))`,
                  }}
                >
                  {entry.kind === "directory" &&
                    (entry.open ? <ChevronDownIcon /> : <ChevronRightIcon />)}
                  <span className="landing-window-name">{entry.name}</span>
                  {entry.status && (
                    <span
                      className="landing-window-status"
                      data-status={entry.status}
                    >
                      {entry.status}
                    </span>
                  )}
                </li>
              ))}
            </ul>
            <div className="landing-window-summary">
              <dl>
                <div>
                  <dt>Files changed</dt>
                  <dd>{demoSummary.files}</dd>
                </div>
                <div>
                  <dt>Additions</dt>
                  <dd className="text-success">+{demoSummary.additions}</dd>
                </div>
                <div>
                  <dt>Deletions</dt>
                  <dd className="text-error">−{demoSummary.deletions}</dd>
                </div>
              </dl>
              <span
                className="landing-window-bar"
                style={{
                  gridTemplateColumns: `${demoSummary.additions}fr ${demoSummary.deletions}fr`,
                }}
              >
                <span data-kind="addition" />
                <span data-kind="deletion" />
              </span>
            </div>
          </div>
          <div className="landing-window-diff">{demoDiff}</div>
        </div>
        <div className="landing-window-status-bar" aria-hidden="true">
          <span className="landing-window-live">Local</span>
          <span>Working tree · uncommitted</span>
          <span className="landing-window-keys">Alt+J / Alt+K files</span>
        </div>
      </div>
      <figcaption>
        The review, with a demo change. Not a real repository.
      </figcaption>
    </figure>
  );
}

/** Shows exactly what "Copy review" exports for the demo comment. */
function AgentReview() {
  return (
    <div className="landing-agent">
      <div className="landing-agent-header">
        <span className="font-mono">code-review-comments.xml</span>
        <CopyButton text={demoAgentReview} label="Copy demo review">
          Copy review
        </CopyButton>
      </div>
      <pre className="landing-agent-xml">
        <code>{demoAgentReview}</code>
      </pre>
    </div>
  );
}

function CommandRow({ command, label }: { command: string; label: string }) {
  return (
    <div className="landing-command">
      <code>
        <span aria-hidden="true" className="landing-prompt">
          ${" "}
        </span>
        {command}
      </code>
      <CopyButton text={command} label={label} variant="default" />
    </div>
  );
}

function CopyButton({
  text,
  label,
  variant = "outline",
  children = "Copy",
}: {
  text: string;
  label: string;
  variant?: ComponentProps<typeof Button>["variant"];
  children?: ReactNode;
}) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <>
      <Button
        variant={variant}
        size="sm"
        touch
        aria-label={label}
        onClick={() => {
          void navigator.clipboard
            ?.writeText(text)
            .then(() => setCopied(true))
            .catch(() => {});
        }}
      >
        {copied ? (
          <CheckIcon aria-hidden="true" />
        ) : (
          <CopyIcon aria-hidden="true" />
        )}
        {copied ? "Copied" : children}
      </Button>
      <span className="sr-only" role="status">
        {copied ? "Copied" : ""}
      </span>
    </>
  );
}
