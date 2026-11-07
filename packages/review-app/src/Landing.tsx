import { CheckIcon, CopyIcon, GitPullRequestIcon } from "lucide-react";
import {
  useEffect,
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
import { demoAgentReview, demoPath, demoStat } from "./landing-demo.ts";
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
const installCommand = "brew install flexdinesh/tap/diffx";

type Act = "run" | "review" | "handoff";
const acts: readonly { id: Act; label: string; caption: string }[] = [
  { id: "run", label: "Run", caption: "diffx" },
  { id: "review", label: "Review", caption: demoPath },
  { id: "handoff", label: "Hand off", caption: "code-review-comments.xml" },
];

function actElementId(act: Act) {
  return `landing-act-${act}`;
}

function reducedMotion() {
  return window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
}

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
 * The signed-out home on a remote server: what diffx does, shown as one
 * review session (run, review, hand off) on the real review surface. With
 * public pull request previews on, pasting a link is the primary action;
 * otherwise "Go to app" is.
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
      <div className="landing-topbar-band">
        <header className="landing-topbar landing-frame">
          <a className="brand" href="/">
            <BrandMark />
            diffx
          </a>
          <nav className="landing-topbar-actions" aria-label="diffx">
            <LinkButton variant="ghost" href={docsUrl} touch>
              Docs
            </LinkButton>
            <LinkButton variant="ghost" href={repositoryUrl} touch>
              GitHub
            </LinkButton>
            <LinkButton variant="outline" touch {...appLink("/signin")}>
              Go to app
            </LinkButton>
          </nav>
        </header>
      </div>
      <main>
        <section
          className="landing-hero landing-frame"
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
        <div className="landing-frame">
          <ReviewSession demoDiff={demoDiff} />
        </div>
        <section
          className="landing-close"
          aria-labelledby="landing-close-title"
        >
          <h2 id="landing-close-title" className="landing-heading">
            Start with your own changes
          </h2>
          <p className="landing-copy">
            Install the CLI and run it in any Git repository. Your browser opens
            the captured changes.
          </p>
          <CommandRow command={installCommand} label="Copy install command" />
          <div className="landing-actions">
            <LinkButton variant="default" touch {...appLink("/signin")}>
              Go to app
            </LinkButton>
            <LinkButton
              variant="outline"
              touch
              href={`${docsUrl}/getting-started`}
            >
              Getting started
            </LinkButton>
          </div>
        </section>
      </main>
      <div className="landing-footer-band">
        <footer className="landing-footer landing-frame">
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

/** The current act: the last one whose top has passed the strip. */
function useCurrentAct() {
  const [current, setCurrent] = useState<Act>("run");
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const visible = new Map<Act, boolean>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const act = acts.find(
            (act) => actElementId(act.id) === entry.target.id,
          );
          if (act) visible.set(act.id, entry.isIntersecting);
        }
        const first = acts.find((act) => visible.get(act.id));
        if (first) setCurrent(first.id);
      },
      { rootMargin: "-30% 0px -50% 0px" },
    );
    for (const act of acts) {
      const element = document.getElementById(actElementId(act.id));
      if (element) observer.observe(element);
    }
    return () => observer.disconnect();
  }, []);
  return { current, setCurrent };
}

/** One review session in three acts on the review surface. */
function ReviewSession({ demoDiff }: { demoDiff: ReactNode }) {
  const { current, setCurrent } = useCurrentAct();
  return (
    <section
      className="landing-stage"
      aria-label="A diffx review, step by step"
    >
      <nav className="landing-acts" aria-label="Steps">
        <ol>
          {acts.map((act) => (
            <li key={act.id}>
              <a
                href={`#${actElementId(act.id)}`}
                aria-current={current === act.id ? "step" : undefined}
                onClick={(event) => {
                  event.preventDefault();
                  setCurrent(act.id);
                  document
                    .getElementById(actElementId(act.id))
                    ?.scrollIntoView({
                      behavior: reducedMotion() ? "auto" : "smooth",
                      block: "start",
                    });
                }}
              >
                <span className="landing-act-label">{act.label}</span>
                <span className="landing-act-caption">{act.caption}</span>
              </a>
            </li>
          ))}
        </ol>
        <DiffStat />
      </nav>
      <div id={actElementId("run")} className="landing-act landing-act-run">
        <p className="landing-act-intro">Run it in any Git repository.</p>
        <CommandRow command="diffx" label="Copy command" />
      </div>
      <div id={actElementId("review")} className="landing-act">
        <p className="landing-act-intro">
          Read the diff and leave comments on the lines that need work.
          <span className="landing-demo-note">
            {" "}
            Demo change, not a real repository.
          </span>
        </p>
        {demoDiff}
      </div>
      <div id={actElementId("handoff")} className="landing-act">
        <p className="landing-act-intro">
          Copy the review into any coding agent. Agents also read the same
          comments over <a href={`${docsUrl}/mcp`}>MCP</a>, browser agents
          through <a href={`${docsUrl}/webmcp`}>WebMCP</a>, and{" "}
          <a href={`${docsUrl}/plugins`}>agent plugins</a> sync each checkout to
          a <a href={`${docsUrl}/agent-hooks`}>remote diffx server</a>.
        </p>
        <AgentReview />
      </div>
    </section>
  );
}

/** The demo's added and deleted lines, drawn in proportion. */
function DiffStat() {
  const total = demoStat.additions + demoStat.deletions;
  const blocks = 10;
  const added = Math.round((demoStat.additions / total) * blocks);
  return (
    <p className="landing-diffstat">
      <span className="sr-only">
        {demoStat.additions} additions and {demoStat.deletions} deletions
      </span>
      <span aria-hidden="true" className="landing-diffstat-counts">
        <span className="text-success">+{demoStat.additions}</span>{" "}
        <span className="text-error">−{demoStat.deletions}</span>
      </span>
      <span aria-hidden="true" className="landing-diffstat-bar">
        {Array.from({ length: blocks }, (_, index) => (
          <span
            key={index}
            data-kind={index < added ? "addition" : "deletion"}
          />
        ))}
      </span>
    </p>
  );
}

/** Shows the exported review once, revealed as it scrolls into view. */
function AgentReview() {
  const block = useRef<HTMLPreElement>(null);
  const [reveal, setReveal] = useState<"idle" | "pending" | "revealed">("idle");
  useEffect(() => {
    const element = block.current;
    if (
      !element ||
      reducedMotion() ||
      typeof IntersectionObserver === "undefined"
    )
      return;
    const top = element.getBoundingClientRect().top;
    if (top < window.innerHeight) return;
    setReveal("pending");
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        setReveal("revealed");
        observer.disconnect();
      },
      { rootMargin: "0px 0px -20% 0px" },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return (
    <div className="landing-agent">
      <div className="landing-agent-header">
        <span className="font-mono">code-review-comments.xml</span>
        <CopyButton text={demoAgentReview} label="Copy demo review" />
      </div>
      <pre ref={block} data-reveal={reveal} className="landing-agent-xml">
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
      <CopyButton text={command} label={label} />
    </div>
  );
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <>
      <Button
        variant="ghost"
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
        {copied ? "Copied" : "Copy"}
      </Button>
      <span className="sr-only" role="status">
        {copied ? "Copied" : ""}
      </span>
    </>
  );
}
