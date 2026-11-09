import type { ApiContext } from "@diffx/api";
import {
  CheckCheckIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronUpIcon,
  MailIcon,
} from "lucide-react";
import { useEffect } from "react";
import { IconButton } from "@diffx/ui/components/icon-button";
import { LinkButton } from "@diffx/ui/components/link-button";
import { benchTrail } from "./bench-trail.ts";
import { streamKey } from "./project-nav.ts";
import {
  adjacentReview,
  benchList,
  benchOrder,
  benchRows,
  benchSections,
  benchState,
  type BenchQuery,
} from "./review-bench.ts";
import { useCatalog, useSession } from "./session-context.tsx";
import { NewMark, streamClick } from "./StreamParts.tsx";

// Moving between reviews without a column: the header's Reviews breadcrumb,
// the stepper through the bench's order, and the next review once one is done.

const attention: BenchQuery = { view: "attention", repo: "", agent: "", q: "" };

/**
 * The open stream's neighbours in the bench order this tab last listed. A
 * review opened from elsewhere leads the current Needs attention order.
 */
export function useReviewNeighbours() {
  const { heads, marks, current } = useCatalog();
  const stream = current ? streamKey(current) : "";
  const byStream = new Map(
    heads.contexts.map((context) => [streamKey(context), context]),
  );
  const trail = benchTrail().order.filter((id) => byStream.has(id));
  const order = trail.includes(stream)
    ? trail
    : [
        stream,
        ...benchOrder(
          benchSections(
            benchList(benchRows(heads.contexts, marks), attention),
            "attention",
          ),
        )
          .map((row) => row.id)
          .filter((id) => id !== stream),
      ];
  const at = (step: 1 | -1) => {
    const id = adjacentReview(order, stream, step);
    return id === undefined ? undefined : byStream.get(id);
  };
  return {
    position: order.indexOf(stream) + 1,
    total: order.length,
    previous: at(-1),
    next: at(1),
  };
}

/** "Reviews" back to the bench, marked while anything needs attention. */
export function BenchLink() {
  const { heads, marks, openBench } = useCatalog();
  const unread = benchList(benchRows(heads.contexts, marks), attention).length;
  return (
    <>
      <LinkButton
        variant="shell"
        size="sm"
        touch
        href={benchTrail().path}
        aria-label={unread ? `Reviews, ${unread} need attention` : "Reviews"}
        title="Back to Reviews (Alt+Shift+B)"
        onClick={(event) => streamClick(event, openBench)}
      >
        <ChevronLeftIcon aria-hidden="true" />
        <span className="max-md:hidden">Reviews</span>
        {unread > 0 && <NewMark />}
      </LinkButton>
      <span
        className="context-identity-separator bench-link-separator"
        aria-hidden="true"
      >
        /
      </span>
    </>
  );
}

/** Previous/next review in bench order, plus the open stream's viewed mark. */
export function ReviewStepper() {
  const { openStream, heads, marks, markViewed, markUnread } = useCatalog();
  const session = useSession();
  const { position, total, previous, next } = useReviewNeighbours();
  const latest =
    heads.contexts.find(
      (context) => streamKey(context) === streamKey(session),
    ) ?? session;
  const viewed = benchState(latest, marks) === "viewed";
  const open = (head: ApiContext | undefined) => {
    if (head) openStream(head, { restoreFocus: false });
  };
  const toggleViewed = () => {
    if (viewed) markUnread(latest);
    else markViewed([latest]);
  };
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        !event.altKey ||
        !event.shiftKey ||
        event.metaKey ||
        event.ctrlKey ||
        event.repeat ||
        event
          .composedPath()
          .some(
            (target) =>
              target instanceof HTMLInputElement ||
              target instanceof HTMLTextAreaElement ||
              (target instanceof HTMLElement && target.isContentEditable),
          )
      )
        return;
      const action = {
        KeyJ: () => open(next),
        KeyK: () => open(previous),
        KeyV: () => markViewed([latest]),
        KeyU: () => markUnread(latest),
      }[event.code];
      if (!action) return;
      event.preventDefault();
      action();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  });
  return (
    <div className="review-stepper">
      <IconButton
        variant="shell"
        touch
        label={viewed ? "Mark unread" : "Mark viewed"}
        shortcut={{ alt: true, shift: true, key: viewed ? "U" : "V" }}
        aria-pressed={viewed}
        onClick={toggleViewed}
      >
        {viewed ? (
          <MailIcon aria-hidden="true" />
        ) : (
          <CheckCheckIcon aria-hidden="true" />
        )}
      </IconButton>
      <IconButton
        variant="shell"
        touch
        label="Previous review"
        shortcut={{ alt: true, shift: true, key: "K" }}
        disabled={!previous}
        onClick={() => open(previous)}
      >
        <ChevronUpIcon aria-hidden="true" />
      </IconButton>
      <span
        className="review-stepper-position"
        aria-label={`Review ${position} of ${total}`}
      >
        {position} of {total}
      </span>
      <IconButton
        variant="shell"
        touch
        label="Next review"
        shortcut={{ alt: true, shift: true, key: "J" }}
        disabled={!next}
        onClick={() => open(next)}
      >
        <ChevronDownIcon aria-hidden="true" />
      </IconButton>
    </div>
  );
}

/** Alt+Shift+B returns to the bench from a review or its loading shell. */
export function useBenchShortcut(enabled: boolean) {
  const { openBench, bench } = useCatalog();
  useEffect(() => {
    if (!enabled || bench) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.altKey &&
        event.shiftKey &&
        !event.metaKey &&
        !event.ctrlKey &&
        event.code === "KeyB"
      ) {
        event.preventDefault();
        openBench();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [enabled, bench, openBench]);
}
