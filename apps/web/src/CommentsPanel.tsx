import { useState } from "react";
import {
  ChevronDownIcon,
  MessageSquareWarningIcon,
  Trash2Icon,
} from "lucide-react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CountBadge } from "@/components/count-badge";
import { Notice } from "@/components/notice";
import { SplitButton } from "@/components/split-button";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { TabsContent } from "@/components/ui/tabs";
import {
  useDiffSource,
  useDraft,
  useNavigation,
  useReviewState,
} from "./app-state.tsx";
import { DraftComment, ReviewCommentCard } from "./review.tsx";
import { capabilityEnabled, useSession } from "./session-context.tsx";
import {
  commentStale,
  placedInline,
  placementFor,
  type ReviewComment,
} from "./review-model.ts";

type CommentFilter = "open" | "resolved" | "outdated" | "all";

const COMMENT_FILTERS: readonly CommentFilter[] = [
  "open",
  "resolved",
  "outdated",
  "all",
];
const DELETE_FILTERS: readonly CommentFilter[] = [
  "all",
  "open",
  "resolved",
  "outdated",
];

function filterLabel(filter: CommentFilter) {
  if (filter === "open") return "Open";
  if (filter === "resolved") return "Resolved";
  if (filter === "outdated") return "Outdated";
  return "All";
}

export function CommentsPanel() {
  const [filter, setFilter] = useState<CommentFilter>("open");
  const [deleteSelection, setDeleteSelection] = useState<CommentFilter | null>(
    null,
  );
  const { repository } = useDiffSource();
  const { draft } = useDraft();
  const review = useReviewState();
  const { navigateComment } = useNavigation();
  // Lifecycle filters by status; Outdated selects comments written against earlier code.
  const matches = (comment: ReviewComment, value: CommentFilter) =>
    value === "all" ||
    (value === "outdated"
      ? commentStale(comment, repository)
      : comment.status === value);
  const counts = {
    open: review.comments.filter((comment) => matches(comment, "open")).length,
    resolved: review.comments.filter((comment) => matches(comment, "resolved"))
      .length,
    outdated: review.comments.filter((comment) => matches(comment, "outdated"))
      .length,
  };
  const activeCount =
    filter === "all" ? review.comments.length : counts[filter];
  const deleteCount =
    deleteSelection === null
      ? 0
      : deleteSelection === "all"
        ? review.comments.length
        : counts[deleteSelection];
  const currentComments = review.currentComments.filter((comment) =>
    matches(comment, filter),
  );
  const outdatedComments = review.outdatedComments.filter((comment) =>
    matches(comment, filter),
  );
  return (
    <TabsContent
      value="comments"
      id="comments-panel"
      className="comments-panel min-h-0"
      aria-label="Review comments"
    >
      <div className="px-4 pb-3">
        <div className="mb-3 flex items-center justify-start gap-2">
          {!!review.comments.length && (
            <>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      variant="outline"
                      size="xs"
                      touch
                      aria-label={`Filter comments: ${filterLabel(filter)}`}
                    />
                  }
                >
                  {filterLabel(filter)}
                  <CountBadge count={activeCount} />
                  <ChevronDownIcon className="size-3" aria-hidden="true" />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="start"
                  className="w-max min-w-30"
                  aria-label="Filter comments"
                >
                  <DropdownMenuRadioGroup
                    value={filter}
                    onValueChange={(value: unknown) => {
                      if (
                        value === "open" ||
                        value === "resolved" ||
                        value === "outdated" ||
                        value === "all"
                      )
                        setFilter(value);
                    }}
                  >
                    {COMMENT_FILTERS.map((value) => (
                      <DropdownMenuRadioItem
                        key={value}
                        value={value}
                        closeOnClick
                        indicator="start"
                        className="gap-1"
                      >
                        <span>{filterLabel(value)}</span>
                        <CountBadge
                          className="ml-auto"
                          count={
                            value === "all"
                              ? review.comments.length
                              : counts[value]
                          }
                        />
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              <SplitButton
                variant="destructive"
                size="xs"
                touch
                className="ml-auto"
                menuLabel="Delete options"
                menuAlign="start"
                menu={DELETE_FILTERS.map((value) => {
                  const count =
                    value === "all" ? review.comments.length : counts[value];
                  return (
                    <DropdownMenuItem
                      key={value}
                      className="gap-1"
                      disabled={count === 0}
                      onClick={() => setDeleteSelection(value)}
                    >
                      <span>{filterLabel(value)}</span>
                      <CountBadge className="ml-auto" count={count} />
                    </DropdownMenuItem>
                  );
                })}
                onClick={() => setDeleteSelection("all")}
              >
                <Trash2Icon aria-hidden="true" />
                Delete
              </SplitButton>
            </>
          )}
        </div>
        {draft &&
          (!placedInline(placementFor(draft, repository).applicability) ? (
            <DraftComment sidebar />
          ) : (
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => navigateComment(draft)}
            >
              Continue draft
            </Button>
          ))}
        {!!currentComments.length && (
          <section className="review-round" aria-label="Current comments">
            <h2>Current</h2>
            {currentComments.map((comment) => (
              <ReviewCommentCard key={comment.id} comment={comment} sidebar />
            ))}
          </section>
        )}
        {!!outdatedComments.length && (
          <section
            id="outdated-comments"
            className="review-round outdated-review"
            aria-label="Outdated comments"
          >
            <h2>Outdated</h2>
            <p className="review-round-hint">
              Written against earlier code. Resolve them, keep them open on
              their file, or delete them.
            </p>
            {outdatedComments.map((comment) => (
              <ReviewCommentCard key={comment.id} comment={comment} sidebar />
            ))}
          </section>
        )}
        {!review.comments.length && (
          <p className="comment-empty">
            Hover a code line and click + to leave feedback. Select several
            lines to comment on a range. Saved comments persist through the
            local server and can be copied for your coding agent.
          </p>
        )}
        {!!review.comments.length &&
          !currentComments.length &&
          !outdatedComments.length && (
            <p className="comment-empty">No {filter} comments.</p>
          )}
        <ConfirmDialog
          open={deleteSelection !== null}
          onOpenChange={(open) => {
            if (!open) setDeleteSelection(null);
          }}
          title={`Delete ${deleteSelection ? filterLabel(deleteSelection).toLowerCase() : ""} comments?`}
          description={`This permanently removes ${deleteCount} ${deleteSelection === "all" ? "" : `${deleteSelection} `}${deleteCount === 1 ? "comment" : "comments"}. This cannot be undone.`}
          confirmLabel="Delete comments"
          onConfirm={() => {
            if (deleteSelection) void review.removeComments(deleteSelection);
            setDeleteSelection(null);
          }}
        />
      </div>
    </TabsContent>
  );
}

/** Points to open comments whose code changed since they were written. */
export function OutdatedCommentsNotice() {
  const { capabilities } = useSession();
  const review = useReviewState();
  const { showOutdatedComments } = useNavigation();
  const open = review.outdatedComments.filter(
    (comment) => comment.status === "open",
  ).length;
  const visible = capabilityEnabled(capabilities.review.comments) && open > 0;
  return (
    <Notice
      hidden={!visible}
      icon={visible && <MessageSquareWarningIcon aria-hidden="true" />}
      action={
        visible && (
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={showOutdatedComments}
          >
            Review outdated
          </Button>
        )
      }
    >
      {visible &&
        `${open} open ${open === 1 ? "comment is" : "comments are"} outdated by code changes`}
    </Notice>
  );
}
