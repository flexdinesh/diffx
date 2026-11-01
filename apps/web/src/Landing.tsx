import { GitPullRequestIcon } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { ListRow } from "@diffx/ui/components/list-row";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { AuthPage, PageLink, prose } from "./account.tsx";
import { navigate } from "./identity-context.tsx";
import {
  demoPullRequests,
  parsePullRequestInput,
  pullRequestName,
  pullRequestPath,
} from "./public-pull-request.ts";

const invalidLink =
  "Enter a GitHub pull request link, like github.com/owner/repo/pull/123.";

/**
 * The signed-out home while public pull request previews are on: paste a
 * pull request link, or try a demo, to open its read-only preview.
 */
export function Landing() {
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
    <AuthPage title="Review a pull request">
      <p className={prose}>
        Paste a link to a pull request in a public GitHub repository.
      </p>
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
      <section className="mt-8" aria-labelledby="demo-pull-requests">
        <h2
          id="demo-pull-requests"
          className="mb-2 text-sm font-medium text-secondary"
        >
          Try one
        </h2>
        <ul className="flex flex-col gap-1">
          {demoPullRequests.map((demo) => {
            const path = pullRequestPath(demo);
            return (
              <li key={path}>
                <ListRow
                  variant="option"
                  render={
                    <a
                      href={path}
                      onClick={(event) => {
                        event.preventDefault();
                        navigate(path);
                      }}
                    />
                  }
                >
                  <GitPullRequestIcon aria-hidden="true" />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{demo.title}</span>
                    <span className="truncate font-mono text-xs text-muted">
                      {pullRequestName(demo)} #{demo.number}
                    </span>
                  </span>
                </ListRow>
              </li>
            );
          })}
        </ul>
      </section>
      <p className="mt-6 text-sm leading-copy text-secondary">
        Or replace <code className="font-mono">github.com</code> with{" "}
        <code className="font-mono">{window.location.host}</code> in any pull
        request URL.
      </p>
      <p className="mt-6 leading-copy text-secondary">
        Have an account? <PageLink to="/signin">Sign in</PageLink>
      </p>
    </AuthPage>
  );
}
