import {
  api,
  errorDetail,
  type ApiPushDevice,
  type ApiPushSettings,
} from "@diffx/api";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import type { GitHubFollowing } from "./account-github.tsx";
import { useInstallPrompt } from "./pwa.ts";
import {
  disablePush,
  discardSubscription,
  enablePush,
  pushEnvironment,
  pushSupport,
  removeDevice,
  thisDevice,
} from "./push.ts";

const prose = "mb-4 leading-copy text-secondary";

function date(value: number) {
  return new Date(value).toLocaleDateString();
}

type Settings =
  | { status: "loading" }
  | { status: "absent" }
  | { status: "error"; message: string }
  | { status: "ready"; settings: ApiPushSettings };

/**
 * Web Push for this account, scoped to this device first. Absent when the
 * server has no push endpoint (local mode or an older server).
 */
export function AccountNotifications({
  admin,
  github,
  onEnabledChange,
}: {
  admin: boolean;
  /** Null until the GitHub section knows; then whether anything can notify. */
  github: GitHubFollowing | null;
  /** Reports whether this server sends push notifications. */
  onEnabledChange?: (enabled: boolean) => void;
}) {
  const [settings, setSettings] = useState<Settings>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void api
      .GET("/api/v2/push", { signal: controller.signal })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setSettings({ status: "absent" });
          return;
        }
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load notifications. Try again."),
          );
        setSettings({ status: "ready", settings: data });
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setSettings({
            status: "error",
            message: errorDetail(
              failure,
              "Unable to load notifications. Try again.",
            ),
          });
      });
    return () => controller.abort();
  }, [attempt]);
  const enabled = settings.status === "ready" && settings.settings.enabled;
  useEffect(() => {
    onEnabledChange?.(enabled);
  }, [enabled, onEnabledChange]);
  if (settings.status === "absent" || settings.status === "loading")
    return null;
  const ready = settings.status === "ready" ? settings.settings : null;
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="notifications-title"
    >
      <h2 id="notifications-title" className="mb-2 text-md font-semibold">
        Notifications
      </h2>
      {settings.status === "error" && (
        <ErrorState
          message={settings.message}
          className="mb-4"
          touch
          onRetry={() => setAttempt((value) => value + 1)}
        />
      )}
      {ready && !ready.enabled && (
        <p className={prose}>
          {admin ? (
            <>
              Push notifications need an HTTPS public URL. Start the server with{" "}
              <code>--public-url</code> set to its HTTPS address.
            </>
          ) : (
            "Push notifications are off on this server. Ask its admin to turn them on."
          )}
        </p>
      )}
      {ready?.enabled && (
        <p className={prose}>
          Get a notification when a pull request opens or gets new commits in a
          repository you follow.
          {github === "none" && (
            <>
              {" "}
              Choose repositories to follow under{" "}
              <a
                href="#github-title"
                className="text-accent underline underline-offset-(--space-1)"
              >
                GitHub
              </a>
              .
            </>
          )}
          {github === "following" &&
            " Turn off the bell beside a repository under GitHub to mute it."}
          {github === "unavailable" && " GitHub is not set up on this server."}
        </p>
      )}
      {ready?.enabled && ready.publicKey && (
        <Devices publicKey={ready.publicKey} devices={ready.devices} />
      )}
    </section>
  );
}

type Busy = "enable" | "disable" | "test" | null;

/** This device's state and actions, then the account's other devices. */
function Devices({
  publicKey,
  devices: initialDevices,
}: {
  publicKey: string;
  devices: ApiPushDevice[];
}) {
  const [environment] = useState(pushEnvironment);
  const support = pushSupport(environment);
  const [permission, setPermission] = useState<NotificationPermission>(() =>
    support === "supported" ? Notification.permission : "default",
  );
  const [device, setDevice] = useState<ApiPushDevice | null>(null);
  const [checking, setChecking] = useState(
    support === "supported" && permission === "granted",
  );
  const [devices, setDevices] = useState(initialDevices);
  const [busy, setBusy] = useState<Busy>(null);
  const [removing, setRemoving] = useState<ApiPushDevice | null>(null);
  const [removeBusy, setRemoveBusy] = useState(false);
  const [removeError, setRemoveError] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [listStatus, setListStatus] = useState("");
  const install = useInstallPrompt();
  useEffect(() => {
    if (!checking) return;
    let current = true;
    void thisDevice(publicKey)
      .then((found) => {
        if (current) setDevice(found);
      })
      .catch((failure: unknown) => {
        if (current)
          setError(
            errorDetail(
              failure,
              "Unable to check notifications on this device. Reload to try again.",
            ),
          );
      })
      .finally(() => {
        if (current) setChecking(false);
      });
    return () => {
      current = false;
    };
  }, [checking, publicKey]);
  async function enable() {
    setBusy("enable");
    setError("");
    setStatus("");
    try {
      const enabled = await enablePush(publicKey);
      setPermission(Notification.permission);
      if (enabled) {
        setDevice(enabled);
        setStatus("Notifications are on for this device.");
      } else if (Notification.permission === "default")
        setStatus("Notifications stay off until you allow them.");
    } catch (failure: unknown) {
      setPermission(Notification.permission);
      setError(
        errorDetail(failure, "Unable to turn on notifications. Try again."),
      );
    } finally {
      setBusy(null);
    }
  }
  async function disable(id: string) {
    setBusy("disable");
    setError("");
    setStatus("");
    try {
      await disablePush(id);
      setDevice(null);
      setStatus("Notifications are off for this device.");
    } catch (failure: unknown) {
      setError(
        errorDetail(failure, "Unable to turn off notifications. Try again."),
      );
    } finally {
      setBusy(null);
    }
  }
  async function test(id: string) {
    setBusy("test");
    setError("");
    setStatus("");
    try {
      const { response } = await api.POST(
        "/api/v2/push/subscriptions/{subscriptionId}/test",
        { params: { path: { subscriptionId: id } } },
      );
      if (response.ok) {
        setStatus("Test notification sent. It can take a few seconds.");
        return;
      }
      if (response.status === 404) {
        setDevice(null);
        await discardSubscription().catch(() => {});
        setError(
          "This device's subscription expired. Turn notifications on again.",
        );
        return;
      }
      setError(
        response.status === 429
          ? "Too many test notifications. Wait a minute, then try again."
          : response.status === 502
            ? "The browser's push service did not accept the notification. Try again later."
            : "Unable to send a test notification. Try again.",
      );
    } catch {
      setError("Unable to send a test notification. Try again.");
    } finally {
      setBusy(null);
    }
  }
  async function remove(target: ApiPushDevice) {
    setRemoveBusy(true);
    setRemoveError("");
    try {
      await removeDevice(target.id);
      setDevices((value) => value.filter((item) => item.id !== target.id));
      setListStatus(`Removed ${target.label}.`);
      setRemoving(null);
    } catch (failure: unknown) {
      setRemoveError(
        errorDetail(failure, "Unable to remove device. Try again."),
      );
    } finally {
      setRemoveBusy(false);
    }
  }
  const others = devices.filter((item) => item.id !== device?.id);
  return (
    <>
      <section aria-labelledby="this-device-title">
        <h3 id="this-device-title" className="mb-2 text-base font-semibold">
          This device
        </h3>
        {support === "unsupported" && (
          <p className={prose}>
            This browser cannot receive push notifications.
          </p>
        )}
        {support === "ios-install" && (
          <>
            <p className="mb-2 leading-copy text-secondary">
              On iPhone and iPad, notifications work in the Home Screen app.
            </p>
            <ol className="mb-4 list-decimal pl-6 leading-copy text-secondary">
              <li>In Safari, tap Share, then Add to Home Screen.</li>
              <li>
                Open diffx from the Home Screen, then turn on notifications in
                Account settings.
              </li>
            </ol>
          </>
        )}
        {support === "supported" && permission === "denied" && (
          <p className={prose}>
            Notifications are blocked for this site. Allow them in your
            browser&apos;s site settings, then reload this page.
          </p>
        )}
        {support === "supported" && permission !== "denied" && checking && (
          <p className={prose}>Checking this device…</p>
        )}
        {support === "supported" && permission !== "denied" && !checking && (
          <>
            {permission === "default" && !device ? (
              <div className="mb-4 flex">
                <Button
                  touch
                  disabled={busy !== null}
                  aria-busy={busy === "enable"}
                  onClick={() => void enable()}
                >
                  Turn on notifications
                </Button>
              </div>
            ) : (
              <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
                <div className="flex min-h-control items-center gap-2 max-md:min-h-touch pointer-coarse:min-h-touch">
                  <Checkbox
                    id="push-this-device"
                    checked={device !== null}
                    disabled={busy !== null}
                    aria-busy={busy === "enable" || busy === "disable"}
                    onCheckedChange={(checked) => {
                      if (checked) void enable();
                      else if (device) void disable(device.id);
                    }}
                  />
                  <label
                    htmlFor="push-this-device"
                    className="flex items-center self-stretch"
                  >
                    Send notifications to this device
                  </label>
                </div>
                {device && (
                  <Button
                    variant="outline"
                    touch
                    disabled={busy !== null}
                    aria-busy={busy === "test"}
                    onClick={() => void test(device.id)}
                  >
                    Send test notification
                  </Button>
                )}
              </div>
            )}
          </>
        )}
        {error && <ErrorState message={error} className="mb-4" />}
        <p role="status" className={status ? prose : undefined}>
          {status}
        </p>
        {support !== "ios-install" &&
          (environment.standalone ? (
            <p className="mb-4 text-sm text-muted">Installed as an app.</p>
          ) : (
            install && (
              <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
                <p className="leading-copy text-secondary">
                  Install diffx to open it in its own window.
                </p>
                <Button variant="outline" touch onClick={() => void install()}>
                  Install app
                </Button>
              </div>
            )
          ))}
      </section>
      {others.length > 0 && (
        <section aria-labelledby="other-devices-title">
          <h3
            id="other-devices-title"
            className="mt-6 mb-2 text-base font-semibold"
          >
            Other devices
          </h3>
          <ul className="mb-4">
            {others.map((item) => (
              <li
                key={item.id}
                className="flex items-center justify-between gap-4 border-b border-separator py-3"
              >
                <div className="min-w-0 wrap-anywhere">
                  <strong className="block font-medium">{item.label}</strong>
                  <span className="mt-1 block text-sm text-muted">
                    Added {date(item.createdAt)} ·{" "}
                    {item.lastSuccessAt === null
                      ? "Not notified yet"
                      : `Last notified ${date(item.lastSuccessAt)}`}
                  </span>
                </div>
                <Button
                  variant="destructive"
                  touch
                  aria-label={`Remove ${item.label}`}
                  onClick={() => {
                    setRemoveError("");
                    setListStatus("");
                    setRemoving(item);
                  }}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
        }}
        title={`Remove ${removing?.label ?? "device"}?`}
        description="It stops getting notifications until they are turned on again on that device."
        error={removeError}
        confirmLabel="Remove device"
        busyLabel="Removing…"
        busy={removeBusy}
        onConfirm={() => {
          if (removing) void remove(removing);
        }}
      />
      <p role="status" className={listStatus ? prose : undefined}>
        {listStatus}
      </p>
    </>
  );
}
