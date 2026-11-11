import { api, errorDetail, type ApiNotificationPreferences } from "@diffx/api";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import { Input } from "@diffx/ui/components/ui/input";
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import type { GitHubFollowing } from "./account-github.tsx";
import { navigate } from "./identity-context.tsx";
import { settingsPath } from "./settings-layout.tsx";

const prose = "mb-4 leading-copy text-secondary";

type Audience = "byMe" | "byOthers";
type EventKey = "opened" | "newCommits" | "merged" | "closed";
type ListKey =
  | "onlyAuthors"
  | "neverAuthors"
  | "baseBranches"
  | "mutedTitleTerms";

type Preferences = ApiNotificationPreferences;

const events: readonly { key: EventKey; label: string; hint?: string }[] = [
  { key: "opened", label: "Opened", hint: "New, reopened or ready for review" },
  { key: "newCommits", label: "New commits" },
  { key: "merged", label: "Merged" },
  { key: "closed", label: "Closed without merging" },
];

const audiences: readonly { key: Audience; label: string }[] = [
  { key: "byMe", label: "By me" },
  { key: "byOthers", label: "By others" },
];

const filters: readonly {
  key: ListKey;
  label: string;
  placeholder: string;
  help: string;
}[] = [
  {
    key: "onlyAuthors",
    label: "Only from",
    placeholder: "Anyone",
    help: "GitHub usernames, separated by commas. Applies to others' pull requests.",
  },
  {
    key: "neverAuthors",
    label: "Never from",
    placeholder: "dependabot[bot], renovate[bot]",
    help: "Their pull requests never notify.",
  },
  {
    key: "baseBranches",
    label: "Into branches",
    placeholder: "Any branch",
    help: "Separate with commas. A trailing * matches a prefix, as in release/*.",
  },
  {
    key: "mutedTitleTerms",
    label: "Mute titles containing",
    placeholder: "chore:, WIP",
    help: "Case-insensitive.",
  },
];

/** The server's limits on each list. */
const maxEntries = 20;
const maxEntryLength = 100;

/**
 * Splits comma-separated text the way the server normalizes it: trimmed,
 * blanks dropped and duplicates removed ignoring case, first spelling kept.
 */
function parseList(text: string): { entries: string[] } | { error: string } {
  const entries: string[] = [];
  const seen = new Set<string>();
  for (const part of text.split(",")) {
    const entry = part.trim();
    const key = entry.toLowerCase();
    if (entry === "" || seen.has(key)) continue;
    seen.add(key);
    entries.push(entry);
  }
  if (entries.length > maxEntries)
    return { error: `Enter at most ${maxEntries}, separated by commas.` };
  if (entries.some((entry) => [...entry].length > maxEntryLength))
    return {
      error: `Shorten each entry to ${maxEntryLength} characters or fewer.`,
    };
  return { entries };
}

const sameList = (a: readonly string[], b: readonly string[]) =>
  a.length === b.length && a.every((value, index) => value === b[index]);

type ByField = Partial<Record<ListKey, string>>;

function omit(fields: ByField, key: ListKey): ByField {
  const { [key]: _, ...rest } = fields;
  return rest;
}

/** An in-app link to the GitHub settings page. */
export function GitHubSettingsLink({ children }: { children: ReactNode }) {
  return (
    <a
      href={settingsPath("github")}
      onClick={(event) => {
        event.preventDefault();
        navigate(settingsPath("github"));
      }}
      className="text-accent underline underline-offset-(--space-1)"
    >
      {children}
    </a>
  );
}

type Load =
  | { status: "loading" }
  | { status: "absent" }
  | { status: "error"; message: string }
  | { status: "ready" };

const saveFailure = "Unable to save notification preferences. Try again.";

/**
 * The account's preferences, saved whole and one request at a time. Edits
 * apply at once; while a save is in flight only the latest edit is queued,
 * and a response never replaces newer edits. A failure restores the last
 * saved preferences and drops queued edits.
 */
function usePreferences() {
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [preferences, setPreferences] = useState<Preferences | null>(null);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  /** The latest edit, which renders and seeds the next one. */
  const local = useRef<Preferences | null>(null);
  const saved = useRef<Preferences | null>(null);
  const queued = useRef<Preferences | null>(null);
  const saving = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    const failure = "Unable to load notification preferences. Try again.";
    void api
      .GET("/api/v2/github/notifications", { signal: controller.signal })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setLoad({ status: "absent" });
          return;
        }
        if (!data) throw new Error(errorDetail(error, failure));
        local.current = data;
        saved.current = data;
        setPreferences(data);
        setLoad({ status: "ready" });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setLoad({ status: "error", message: errorDetail(cause, failure) });
      });
    return () => controller.abort();
  }, [attempt]);
  function show(next: Preferences) {
    local.current = next;
    setPreferences(next);
  }
  async function flush() {
    if (saving.current) return;
    saving.current = true;
    try {
      for (let next = queued.current; next; next = queued.current) {
        queued.current = null;
        try {
          const { data, error } = await api.PUT(
            "/api/v2/github/notifications",
            { body: next },
          );
          if (!data) throw new Error(errorDetail(error, saveFailure));
          saved.current = data;
          if (queued.current === null) {
            show(data);
            setStatus("Saved");
          }
        } catch (cause: unknown) {
          queued.current = null;
          if (saved.current) show(saved.current);
          setError(errorDetail(cause, saveFailure));
        }
      }
    } finally {
      saving.current = false;
    }
  }
  function change(update: (current: Preferences) => Preferences) {
    if (!local.current) return;
    const next = update(local.current);
    show(next);
    queued.current = next;
    setError("");
    setStatus("");
    void flush();
  }
  return {
    load,
    preferences,
    error,
    status,
    change,
    retry: () => {
      setLoad({ status: "loading" });
      setAttempt((value) => value + 1);
    },
  };
}

/**
 * Which pull request events notify this account on every device. Absent when
 * the server has no GitHub integration.
 */
export function NotificationPreferences({
  github,
}: {
  /** Null until the GitHub section knows. */
  github: GitHubFollowing | null;
}) {
  const { load, preferences, error, status, change, retry } = usePreferences();
  if (load.status === "absent" || github === "unavailable") return null;
  const loading = load.status === "loading" || github === null;
  return (
    <section
      aria-labelledby="what-notifies-title"
      aria-busy={loading && github !== "disconnected"}
    >
      <h3
        id="what-notifies-title"
        className="mt-6 mb-2 text-base font-semibold"
      >
        What notifies
      </h3>
      {github === "disconnected" ? (
        <p className={prose}>
          <GitHubSettingsLink>Connect GitHub</GitHubSettingsLink> to choose
          which pull requests notify.
        </p>
      ) : (
        <>
          <p className={prose}>
            Applies to all your devices. Pull requests your agents open with
            your GitHub account count as yours.
          </p>
          {load.status === "error" ? (
            <ErrorState
              message={load.message}
              className="mb-4"
              touch
              onRetry={retry}
            />
          ) : loading || !preferences ? (
            <GridSkeleton />
          ) : (
            <Controls preferences={preferences} change={change} />
          )}
          {error && <ErrorState message={error} className="mb-4" />}
          <p role="status" className={status ? prose : undefined}>
            {status}
          </p>
          <p className={prose}>
            Turn repositories on or off with the bells in{" "}
            <GitHubSettingsLink>Settings › GitHub</GitHubSettingsLink>.
          </p>
        </>
      )}
    </section>
  );
}

// The grid and its skeleton share these so loading keeps the same geometry.
const grid = "mb-4 w-full max-w-md border-collapse";
const headerRow = "border-b border-separator text-sm text-secondary";
const columnHeader = "w-20 px-1 pb-2 text-center font-medium";
const row = "border-b border-separator";
const rowHeader = "py-2 pr-2 text-left align-middle font-normal";
const cell = "p-0 align-middle";
const target =
  "flex min-h-control w-full items-center justify-center max-md:min-h-touch pointer-coarse:min-h-touch";

function GridSkeleton() {
  return (
    <div aria-hidden="true">
      <table className={grid}>
        <thead>
          <tr className={headerRow}>
            <td className="pb-2" />
            {audiences.map((audience) => (
              <td key={audience.key} className={columnHeader}>
                <Skeleton shape="label" className="mx-auto w-12" />
              </td>
            ))}
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr key={event.key} className={row}>
              <td className={rowHeader}>
                <Skeleton shape="label" className="w-28" />
                {event.hint && (
                  <Skeleton shape="line" className="mt-2 w-40 max-w-full" />
                )}
              </td>
              {audiences.map((audience) => (
                <td key={audience.key} className={cell}>
                  <span className={target}>
                    <Skeleton shape="icon" />
                  </span>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Controls({
  preferences,
  change,
}: {
  preferences: Preferences;
  change: (update: (current: Preferences) => Preferences) => void;
}) {
  const [drafts, setDrafts] = useState<ByField>({});
  const [errors, setErrors] = useState<ByField>({});
  function edit(key: ListKey, text: string) {
    setDrafts((value) => ({ ...value, [key]: text }));
    setErrors((value) => omit(value, key));
  }
  /** Saves a field's edited text once it parses and differs. */
  function commit(key: ListKey) {
    const text = drafts[key];
    if (text === undefined) return;
    const parsed = parseList(text);
    if ("error" in parsed) {
      setErrors((value) => ({ ...value, [key]: parsed.error }));
      return;
    }
    setDrafts((value) => omit(value, key));
    if (sameList(parsed.entries, preferences[key])) return;
    change((current) => ({ ...current, [key]: parsed.entries }));
  }
  return (
    <>
      <table className={grid}>
        <caption className="sr-only">Pull request events that notify</caption>
        <thead>
          <tr className={headerRow}>
            <th scope="col" className="pb-2">
              <span className="sr-only">Event</span>
            </th>
            {audiences.map((audience) => (
              <th key={audience.key} scope="col" className={columnHeader}>
                {audience.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr key={event.key} className={row}>
              <th scope="row" className={rowHeader}>
                {event.label}
                {event.hint && (
                  <span className="block text-sm text-muted">{event.hint}</span>
                )}
              </th>
              {audiences.map((audience) => (
                <td key={audience.key} className={cell}>
                  {/* The whole cell toggles, sized for touch. */}
                  <label className={target}>
                    <Checkbox
                      aria-label={`${event.label}, ${audience.label.toLowerCase()}`}
                      checked={preferences[event.key][audience.key]}
                      onCheckedChange={(checked) =>
                        change((current) => ({
                          ...current,
                          [event.key]: {
                            ...current[event.key],
                            [audience.key]: checked,
                          },
                        }))
                      }
                    />
                  </label>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mb-6 flex min-h-control items-center gap-2 max-md:min-h-touch pointer-coarse:min-h-touch">
        <Checkbox
          id="notify-skip-drafts"
          checked={preferences.skipDrafts}
          onCheckedChange={(checked) =>
            change((current) => ({ ...current, skipDrafts: checked }))
          }
        />
        <label
          htmlFor="notify-skip-drafts"
          className="flex items-center self-stretch"
        >
          Skip drafts until they&apos;re ready for review
        </label>
      </div>
      <div className="mb-4 grid max-w-md gap-4">
        {filters.map((filter) => {
          const id = `notify-${filter.key}`;
          const fieldError = errors[filter.key];
          return (
            <div key={filter.key} className="settings-field">
              <label htmlFor={id}>{filter.label}</label>
              <Input
                id={id}
                touch
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                placeholder={filter.placeholder}
                value={drafts[filter.key] ?? preferences[filter.key].join(", ")}
                onChange={(event) => edit(filter.key, event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter") return;
                  event.preventDefault();
                  commit(filter.key);
                }}
                onBlur={() => commit(filter.key)}
                aria-invalid={fieldError ? true : undefined}
                aria-describedby={
                  fieldError ? `${id}-help ${id}-error` : `${id}-help`
                }
              />
              <p
                id={`${id}-help`}
                className="text-sm leading-copy text-secondary"
              >
                {filter.help}
              </p>
              {fieldError && (
                <p
                  id={`${id}-error`}
                  className="text-sm leading-copy text-error"
                  role="alert"
                >
                  {fieldError}
                </p>
              )}
            </div>
          );
        })}
      </div>
    </>
  );
}
