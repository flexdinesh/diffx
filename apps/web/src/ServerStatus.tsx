import { api, type ApiServerMetrics } from "@diffx/api";
import {
  CircleAlertIcon,
  CircleCheckIcon,
  LoaderCircleIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { KeyValue, KeyValueList } from "@diffx/ui/components/key-value-list";
import { Button } from "@diffx/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
} from "@diffx/ui/components/ui/popover";
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
    let pending = false;
    const controller = new AbortController();
    const { signal } = controller;
    // A slow response is not replaced by the next tick, so it can still report health.
    async function refresh() {
      if (pending) return;
      pending = true;
      try {
        const { data } = await api.GET("/api/v2/metrics", { signal });
        if (disposed) return;
        if (data) {
          setMetrics(data);
          setHealth("ready");
        } else setHealth("unavailable");
      } catch {
        if (!disposed && !signal.aborted) setHealth("unavailable");
      } finally {
        pending = false;
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
    <Popover>
      {/* The hover popover is this trigger's detail, so it carries no tooltip. */}
      <PopoverTrigger
        aria-label={`Server status: ${label}`}
        openOnHover
        delay={150}
        render={<Button variant="ghost" size="icon-xs" />}
      >
        <StatusIcon health={health} />
      </PopoverTrigger>
      <PopoverContent>
        <PopoverHeader>
          <PopoverTitle>
            <StatusIcon health={health} />
            {label}
          </PopoverTitle>
          {health === "unavailable" && (
            <PopoverDescription>
              Can’t reach the diffx server. Retrying automatically.
            </PopoverDescription>
          )}
        </PopoverHeader>
        <PopoverSection
          title="Diffx server process"
          aria-label="Diffx server process"
        >
          <KeyValueList>
            <KeyValue label="CPU" font="mono" id="server-cpu">
              {cpu}
            </KeyValue>
            <KeyValue label="RAM" font="mono" id="server-ram">
              {ram}
            </KeyValue>
          </KeyValueList>
        </PopoverSection>
      </PopoverContent>
    </Popover>
  );
}

function StatusIcon({ health }: { health: ServerHealth }) {
  if (health === "ready")
    return (
      <CircleCheckIcon
        className="size-icon-sm text-success"
        aria-hidden="true"
      />
    );
  if (health === "checking")
    return (
      <LoaderCircleIcon
        className="size-icon-sm motion-safe:animate-spinner"
        aria-hidden="true"
      />
    );
  return (
    <CircleAlertIcon className="size-icon-sm text-error" aria-hidden="true" />
  );
}
