import { CheckCheckIcon } from "lucide-react";
import { Notice } from "@diffx/ui/components/notice";
import { Button } from "@diffx/ui/components/ui/button";
import { pluralize } from "@diffx/ui/lib/format";
import { useDiffSource, useReviewedFilesState } from "./app-state.tsx";
import { streamTitle } from "./project-nav.ts";
import { useReviewNeighbours } from "./review-navigation.tsx";
import { useCatalog } from "./session-context.tsx";
import { useShellPolicy } from "./use-shell-policy.ts";

/** Once every file is reviewed, offers the next review in bench order. */
export function ReviewCompleteNotice() {
  const { reviewNavigation } = useShellPolicy();
  return reviewNavigation ? <CompleteNotice /> : null;
}

function CompleteNotice() {
  const { repository, marking } = useDiffSource();
  const { isReviewed } = useReviewedFilesState();
  const { openStream, openBench } = useCatalog();
  const { next } = useReviewNeighbours();
  const files = repository?.files ?? [];
  const done = marking && files.length > 0 && files.every(isReviewed);
  return (
    <Notice
      hidden={!done}
      icon={done && <CheckCheckIcon aria-hidden="true" />}
      action={
        done && (
          <span className="flex gap-2">
            <Button
              type="button"
              variant="outline-muted"
              size="sm"
              onClick={openBench}
            >
              Back to Reviews
            </Button>
            {next && (
              <Button
                type="button"
                size="sm"
                onClick={() => openStream(next, { restoreFocus: false })}
              >
                Next review
              </Button>
            )}
          </span>
        )
      }
    >
      {done &&
        `All ${pluralize(files.length, "file")} reviewed.${next ? ` Next: ${next.name} · ${streamTitle(next)}` : ""}`}
    </Notice>
  );
}
