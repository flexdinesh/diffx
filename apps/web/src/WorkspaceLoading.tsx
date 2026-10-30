import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { useAppearance } from "./appearance-context.tsx";
import { SettingsButton } from "./SettingsButton.tsx";

const lines = [
  "medium",
  "long",
  "short",
  "long",
  "medium",
  "short",
  "medium",
  "long",
  "short",
  "medium",
  "long",
  "short",
];

/** Decorative placeholders never claim file identities, counts or change status. */
export function DiffSkeleton() {
  const { layout } = useAppearance();
  return (
    <div className="diff-skeleton" data-layout={layout} aria-hidden="true">
      {["primary", "secondary"].map((file) => (
        <div className="loading-file" key={file}>
          <div className="loading-file-header">
            <Skeleton shape="icon" />
            <Skeleton className="skeleton-title" />
            <Skeleton shape="label" className="skeleton-action" />
          </div>
          <div className="loading-code">
            {["before", "after"].map((side) => (
              <div className="loading-code-pane" key={side}>
                {lines
                  .slice(0, file === "primary" ? 12 : 8)
                  .map((length, index) => (
                    <div className="loading-code-line" key={index}>
                      <Skeleton className="skeleton-line-number" />
                      <Skeleton
                        shape="code"
                        className={`skeleton-code skeleton-code-${length}`}
                      />
                    </div>
                  ))}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

export function FileTreeSkeleton() {
  return (
    <div className="sidebar loading-sidebar" aria-hidden="true">
      <div className="loading-sidebar-tabs">
        <Skeleton className="skeleton-title" />
        <Skeleton className="skeleton-title" />
      </div>
      <div className="loading-tree">
        <Skeleton shape="field" className="skeleton-search" />
        {["folder", "file", "file", "folder", "file", "file", "file"].map(
          (kind, index) => (
            <div
              className={`loading-tree-row loading-tree-${kind}`}
              key={index}
            >
              <Skeleton shape="icon" />
              <Skeleton className="skeleton-name" />
            </div>
          ),
        )}
      </div>
      <div className="loading-sidebar-summary">
        <Skeleton className="skeleton-title" />
        <Skeleton className="skeleton-name" />
        <Skeleton className="skeleton-name" />
      </div>
    </div>
  );
}

export function ProjectsSkeleton() {
  return (
    <div className="projects-skeleton" aria-hidden="true">
      {["first", "second", "third"].map((group) => (
        <div className="loading-project" key={group}>
          <div className="loading-project-name">
            <Skeleton shape="icon" />
            <Skeleton className="skeleton-name" />
          </div>
          <div className="loading-project-stream">
            <Skeleton className="skeleton-name" />
            <Skeleton className="skeleton-title" />
          </div>
        </div>
      ))}
    </div>
  );
}

export function WorkspaceLoading({ collecting }: { collecting: boolean }) {
  return (
    <>
      <div className="loading-toolbar" aria-hidden="true">
        <Skeleton shape="control" className="skeleton-action" />
        <Skeleton className="skeleton-title" />
        <Skeleton className="skeleton-title" />
        <Skeleton shape="control" className="skeleton-action" />
      </div>
      <DiffSkeleton />
      <div className="loading-status">
        <h1>{collecting ? "Collecting latest changes" : "Loading review"}</h1>
        <p className="motion-safe:animate-status-pulse">
          {collecting ? "Reading checkouts…" : "Reading stored changes…"}
        </p>
        <span className="ml-auto flex items-center">
          <SettingsButton />
        </span>
      </div>
    </>
  );
}
