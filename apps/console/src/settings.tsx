import { api, errorDetail, type ConsoleSettings } from "@diffx/console-api";
import { useEffect, useState } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import { Page, prose } from "./page.tsx";

const options: {
  key: keyof ConsoleSettings;
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
];

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

/** Who can join the diffx app, and with which methods. */
export function Settings() {
  const { settings, setSettings, error: loadError, reload } = useSettings();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
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
    } catch (failure: unknown) {
      setStatus("");
      setError(errorDetail(failure, "Unable to save settings. Try again."));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Page
      title="Settings"
      description="Who can join the diffx app, and how. Changes apply at once. Invites work either way, and existing accounts keep signing in."
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
          {error && <ErrorState message={error} />}
        </div>
      )}
      <p className="mt-4 text-sm leading-copy text-secondary" role="status">
        {status}
      </p>
    </Page>
  );
}
