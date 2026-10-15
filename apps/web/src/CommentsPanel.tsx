import { useState } from "react";
import {
  ChevronDownIcon,
  MessageSquareWarningIcon,
  Trash2Icon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/components/ui/dialog";
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
      aria-label="Review comments"
    >
      <div className="comment-panel-header">
        {!!review.comments.length && (
          <>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    type="button"
                    className="comment-filter-trigger"
                    variant="outline"
                    size="xs"
                    aria-label={`Filter comments: ${filterLabel(filter)}`}
                  />
                }
              >
                {filterLabel(filter)}
                <Badge variant="outline" className="comment-filter-count">
                  {activeCount}
                </Badge>
                <ChevronDownIcon aria-hidden="true" />
              </DropdownMenuTrigger>
              <DropdownMenuContent
                align="start"
                className="comment-filter-menu"
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
                      className="comment-filter-option"
                    >
                      <span>{filterLabel(value)}</span>
                      <Badge variant="outline" className="comment-filter-count">
                        {value === "all"
                          ? review.comments.length
                          : counts[value]}
                      </Badge>
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <div className="bulk-delete-actions">
              <Button
                type="button"
                className="bulk-delete-primary"
                variant="destructive"
                size="xs"
                onClick={() => setDeleteSelection("all")}
              >
                <Trash2Icon aria-hidden="true" />
                Delete
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      className="bulk-delete-menu-trigger"
                      variant="outline"
                      size="icon-xs"
                      aria-label="Delete options"
                      title="Delete options"
                    />
                  }
                >
                  <ChevronDownIcon aria-hidden="true" />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="start"
                  className="bulk-delete-menu"
                  aria-label="Delete comments"
                >
                  {DELETE_FILTERS.map((value) => {
                    const count =
                      value === "all" ? review.comments.length : counts[value];
                    return (
                      <DropdownMenuItem
                        key={value}
                        className="bulk-delete-option"
                        disabled={count === 0}
                        onClick={() => setDeleteSelection(value)}
                      >
                        <span>{filterLabel(value)}</span>
                        <Badge
                          variant="outline"
                          className="comment-filter-count"
                        >
                          {count}
                        </Badge>
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </>
        )}
      </div>
      {draft &&
        (!placedInline(placementFor(draft, repository).applicability) ? (
          <DraftComment />
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
            Written against earlier code. Resolve them, keep them open on their
            file, or delete them.
          </p>
          {outdatedComments.map((comment) => (
            <ReviewCommentCard key={comment.id} comment={comment} sidebar />
          ))}
        </section>
      )}
      {!review.comments.length && (
        <p className="comment-empty">
          Hover a code line and click + to leave feedback. Select several lines
          to comment on a range. Saved comments persist through the local server
          and can be copied for your coding agent.
        </p>
      )}
      {!!review.comments.length &&
        !currentComments.length &&
        !outdatedComments.length && (
          <p className="comment-empty">No {filter} comments.</p>
        )}
      <Dialog
        open={deleteSelection !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteSelection(null);
        }}
      >
        <DialogContent
          className="delete-comment-dialog"
          showCloseButton={false}
        >
          <DialogTitle>
            Delete{" "}
            {deleteSelection ? filterLabel(deleteSelection).toLowerCase() : ""}{" "}
            comments?
          </DialogTitle>
          <DialogDescription>
            This permanently removes {deleteCount}{" "}
            {deleteSelection === "all" ? "" : `${deleteSelection} `}
            {deleteCount === 1 ? "comment" : "comments"}. This cannot be undone.
          </DialogDescription>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setDeleteSelection(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              className="delete-comment-confirm"
              variant="destructive"
              onClick={() => {
                if (deleteSelection)
                  void review.removeComments(deleteSelection);
                setDeleteSelection(null);
              }}
            >
              <Trash2Icon aria-hidden="true" />
              Delete comments
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
    <div className="outdated-notice" role="status" hidden={!visible}>
      {visible && (
        <>
          <MessageSquareWarningIcon
            className="size-(--icon-sm)"
            aria-hidden="true"
          />
          <p>
            {open} open {open === 1 ? "comment is" : "comments are"} outdated by
            code changes
          </p>
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={showOutdatedComments}
          >
            Review outdated
          </Button>
        </>
      )}
    </div>
  );
}
