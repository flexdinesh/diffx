import { api, errorDetail, type ConsoleSettings } from "@diffx/console-api";
import { useEffect, useState, type FormEvent } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Button } from "@diffx/ui/components/ui/button";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import { Input } from "@diffx/ui/components/ui/input";
import { Page, prose } from "./page.tsx";

/** The settings each checkbox switches on and off. */
type Toggle = {
  [K in keyof ConsoleSettings]: ConsoleSettings[K] extends boolean ? K : never;
}[keyof ConsoleSettings];

const options: {
  key: Toggle;
  label: string;
  hint: string;
}[] = [
  {
    key: "signup",
    label: "Open signup",
    hint: "When on, anyone can create an app account. When off, only invited people can join.",
  },
  {
    key: "passwordSignup",
    label: "Password signup",
    hint: "When on, new accounts can join with an email and password, by signup or invite. When off, new accounts use Google; existing password accounts keep signing in.",
  },
  {
    key: "publicPullRequests",
    label: "Public pull requests",
    hint: "Let anyone, without signing in, open read-only previews of public GitHub pull requests.",
  },
];

const retentionDays = { min: 1, max: 30 };
const retentionInvalid = `Enter a whole number of days from ${retentionDays.min} to ${retentionDays.max}.`;

/** The whole number of days in `value`, or null outside the allowed range. */
function parseRetention(value: string) {
  const days = Number(value);
  return value.trim() !== "" &&
    Number.isInteger(days) &&
    days >= retentionDays.min &&
    days <= retentionDays.max
    ? days
    : null;
}

/** Loads the settings and reloads them on demand. */
function useSettings() {
  const [settings, setSettings] = useState<ConsoleSettings | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const failure = "Unable to load settings. Try again.";
    setError("");
    void api
      .GET("/api/v1/settings", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data) throw new Error(errorDetail(error, failure));
        setSettings(data);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorDetail(cause, failure));
      });
    return () => controller.abort();
  }, [attempt]);
  return {
    settings,
    setSettings,
    error,
    reload: () => setAttempt((value) => value + 1),
  };
}

/** Who can join the diffx app, with which methods, and who can preview public pull requests. */
export function Settings() {
  const { settings, setSettings, error: loadError, reload } = useSettings();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  // An unsaved retention edit; null shows the saved value.
  const [retention, setRetention] = useState<string | null>(null);
  const [retentionError, setRetentionError] = useState("");
  /** Saves every setting at once; resolves whether the server saved them. */
  async function save(next: ConsoleSettings) {
    setBusy(true);
    setError("");
    setStatus("Saving…");
    try {
      const { data, error } = await api.PUT("/api/v1/settings", {
        body: next,
      });
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to save settings. Try again."),
        );
      setSettings(data);
      setStatus("Settings saved.");
      return true;
    } catch (failure: unknown) {
      setStatus("");
      setError(errorDetail(failure, "Unable to save settings. Try again."));
      return false;
    } finally {
      setBusy(false);
    }
  }
  async function saveRetention(event: FormEvent, current: ConsoleSettings) {
    event.preventDefault();
    if (busy) return;
    const days = parseRetention(
      retention ?? String(current.publicRetentionDays),
    );
    if (days === null) {
      setRetentionError(retentionInvalid);
      return;
    }
    if (await save({ ...current, publicRetentionDays: days }))
      setRetention(null);
  }
  return (
    <Page
      title="Settings"
      description="Who can join the diffx app, and how, and who can preview public pull requests. Changes apply at once. Invites work either way, and existing accounts keep signing in."
    >
      {loadError && (
        <ErrorState
          message={loadError}
          className="mb-4"
          touch
          onRetry={reload}
        />
      )}
      {settings === null && !loadError && (
        <p className={prose} role="status">
          Loading settings…
        </p>
      )}
      {settings !== null && (
        <div className="console-form">
          {options.map((option) => (
            <div key={option.key}>
              <div className="console-check">
                <Checkbox
                  id={`setting-${option.key}`}
                  checked={settings[option.key]}
                  onCheckedChange={(checked) =>
                    void save({ ...settings, [option.key]: checked })
                  }
                  disabled={busy}
                  aria-describedby={`setting-${option.key}-hint`}
                />
                <label
                  htmlFor={`setting-${option.key}`}
                  className="flex min-h-control-small flex-1 items-center font-medium max-md:min-h-touch pointer-coarse:min-h-touch"
                >
                  {option.label}
                </label>
              </div>
              <p
                id={`setting-${option.key}-hint`}
                className="mb-2 text-sm leading-copy text-secondary"
              >
                {option.hint}
              </p>
            </div>
          ))}
          <form
            className="console-form"
            onSubmit={(event) => void saveRetention(event, settings)}
          >
            <label htmlFor="setting-publicRetentionDays">
              Public retention (days)
            </label>
            <Input
              id="setting-publicRetentionDays"
              type="number"
              inputMode="numeric"
              min={retentionDays.min}
              max={retentionDays.max}
              step={1}
              required
              touch
              value={retention ?? String(settings.publicRetentionDays)}
              onChange={(event) => {
                setRetention(event.target.value);
                setRetentionError("");
              }}
              onInvalid={() => setRetentionError(retentionInvalid)}
              disabled={busy}
              aria-invalid={retentionError !== ""}
              aria-describedby={
                retentionError
                  ? "setting-publicRetentionDays-hint setting-publicRetentionDays-error"
                  : "setting-publicRetentionDays-hint"
              }
            />
            <p
              id="setting-publicRetentionDays-hint"
              className="text-sm leading-copy text-secondary"
            >
              Days a preview is kept after its last fetch. Applies to previews
              fetched after the change.
            </p>
            {retentionError && (
              <p
                id="setting-publicRetentionDays-error"
                className="text-sm leading-copy text-error"
                role="alert"
              >
                {retentionError}
              </p>
            )}
            <Button
              type="submit"
              variant="outline"
              touch
              className="mt-2 justify-self-start"
              disabled={busy}
              aria-busy={busy}
            >
              Save retention
            </Button>
          </form>
          <p className="text-sm leading-copy wrap-anywhere text-muted">
            Needs the server&apos;s GitHub App and
            DIFFX_SERVER_GITHUB_PUBLIC_INSTALLATION_ID. Without
            DIFFX_SERVER_CLIENT_IP_HEADER, fetches are limited server-wide (30
            per hour) instead of per visitor (5 per hour).
          </p>
          {error && <ErrorState message={error} />}
        </div>
      )}
      <p className="mt-4 text-sm leading-copy text-secondary" role="status">
        {status}
      </p>
    </Page>
  );
}
