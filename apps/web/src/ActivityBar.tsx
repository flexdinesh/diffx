import { FileDiffIcon, FolderGit2Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AccountButton } from "./account.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCompactChrome } from "./use-compact-chrome.ts";

const views = [
  { id: "diff", label: "Diff", Icon: FileDiffIcon },
  { id: "projects", label: "Repos", Icon: FolderGit2Icon },
] satisfies {
  id: "projects" | "diff";
  label: string;
  Icon: typeof FileDiffIcon;
}[];

/** Fixed view switcher left of the file sidebar, hidden in compact chrome. */
export function ActivityBar({ diffReady = true }: { diffReady?: boolean }) {
  const compact = useCompactChrome();
  const sidebar = useSidebarState();
  if (compact) return null;
  return (
    <nav className="activity-bar" aria-label="Views">
      <ul className="activity-views">
        {views.map(({ id, label, Icon }) => (
          <li key={id}>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              id={`${id}-view`}
              disabled={id === "diff" && !diffReady}
              aria-current={
                sidebar.view === id && (id !== "diff" || diffReady)
                  ? "page"
                  : undefined
              }
              aria-controls={
                id === "projects"
                  ? "projects-nav"
                  : diffReady
                    ? "sidebar"
                    : undefined
              }
              aria-expanded={
                (id !== "diff" || diffReady) &&
                sidebar.view === id &&
                sidebar.expanded
              }
              onClick={() => sidebar.selectView(id)}
              aria-label={label}
              title={label}
            >
              <Icon aria-hidden="true" />
            </Button>
          </li>
        ))}
      </ul>
      <div className="activity-actions">
        <AccountButton />
      </div>
    </nav>
  );
}
