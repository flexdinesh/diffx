import { Popover } from "@base-ui/react/popover";
import { api, type ApiServerMetrics } from "@diffx/api";
import {
  CircleAlertIcon,
  CircleCheckIcon,
  LoaderCircleIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { formatBytes } from "./server-metrics.ts";

const refreshMilliseconds = 2_500;

type ServerHealth = "checking" | "ready" | "unavailable";

const statusLabels: Record<ServerHealth, string> = {
  checking: "Connecting",
  ready: "Ready",
  unavailable: "Server unavailable",
};

export function ServerStatus() {
  const [health, setHealth] = useState<ServerHealth>("checking");
  const [metrics, setMetrics] = useState<ApiServerMetrics | null>(null);
  useEffect(() => {
    let disposed = false;
    let controller = new AbortController();
    async function refresh() {
      controller.abort();
      controller = new AbortController();
      const { signal } = controller;
      try {
        const { data } = await api.GET("/api/v2/metrics", { signal });
        if (disposed) return;
        if (data) {
          setMetrics(data);
          setHealth("ready");
        } else setHealth("unavailable");
      } catch {
        if (!disposed && !signal.aborted) setHealth("unavailable");
      }
    }
    void refresh();
    const timer = window.setInterval(() => void refresh(), refreshMilliseconds);
    return () => {
      disposed = true;
      controller.abort();
      window.clearInterval(timer);
    };
  }, []);

  const label = statusLabels[health];
  const current = health === "ready" ? metrics : null;
  const cpu = current ? `${current.cpuUsage.toFixed(1)}%` : "--";
  const ram = current ? formatBytes(current.rssBytes) : "--";
  return (
    <Popover.Root>
      <Popover.Trigger
        id="server-status"
        data-status={health}
        aria-label={`Server status: ${label}`}
        openOnHover
        delay={150}
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="server-status-trigger"
          />
        }
      >
        <StatusIcon health={health} />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner
          side="top"
          align="end"
          sideOffset={8}
          collisionPadding={16}
          className="z-50"
        >
          <Popover.Popup className="server-status-popup" data-status={health}>
            <Popover.Title className="server-status-title">
              <StatusIcon health={health} />
              {label}
            </Popover.Title>
            {health === "unavailable" && (
              <Popover.Description className="server-status-description">
                Can’t reach the diffx server. Retrying automatically.
              </Popover.Description>
            )}
            <section
              className="server-status-section"
              aria-labelledby="server-process-heading"
            >
              <h2 id="server-process-heading">Diffx server process</h2>
              <dl>
                <div>
                  <dt>CPU</dt>
                  <dd id="server-cpu">{cpu}</dd>
                </div>
                <div>
                  <dt>RAM</dt>
                  <dd id="server-ram">{ram}</dd>
                </div>
              </dl>
            </section>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}

function StatusIcon({ health }: { health: ServerHealth }) {
  const Icon =
    health === "ready"
      ? CircleCheckIcon
      : health === "checking"
        ? LoaderCircleIcon
        : CircleAlertIcon;
  return <Icon className="size-(--icon-sm)" aria-hidden="true" />;
}
