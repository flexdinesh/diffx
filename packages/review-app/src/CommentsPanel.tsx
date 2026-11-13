import { useState } from "react";
import {
  ChevronDownIcon,
  MessageSquareWarningIcon,
  Trash2Icon,
} from "lucide-react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { Notice } from "@diffx/ui/components/notice";
import { SplitButton } from "@diffx/ui/components/split-button";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { TabsContent } from "@diffx/ui/components/ui/tabs";
import {
  useDiffSource,
  useDraft,
  useNavigation,
  useReviewState,
} from "./app-state.tsx";
import { DraftComment, ReviewCommentCard } from "./review.tsx";
import { capabilityEnabled, useSession } from "./session-context.tsx";
import {
  authorMatches,
  commentStale,
  placedInline,
  placementFor,
  type AuthorFilter,
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

const AUTHOR_FILTERS: readonly AuthorFilter[] = ["everyone", "me", "agents"];

function authorLabel(filter: AuthorFilter) {
  if (filter === "me") return "Me";
  if (filter === "agents") return "Agents";
  return "Everyone";
}

function emptyLabel(filter: CommentFilter, author: AuthorFilter) {
  const status = filter === "all" ? "" : `${filter} `;
  const by =
    author === "me" ? " by you" : author === "agents" ? " by agents" : "";
  return `No ${status}comments${by}.`;
}

function filterLabel(filter: CommentFilter) {
  if (filter === "open") return "Open";
  if (filter === "resolved") return "Resolved";
  if (filter === "outdated") return "Outdated";
  return "All";
}

export function CommentsPanel() {
  const [filter, setFilter] = useState<CommentFilter>("open");
  const [author, setAuthor] = useState<AuthorFilter>("everyone");
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
  const countsIn = (comments: ReviewComment[]) => ({
    open: comments.filter((comment) => matches(comment, "open")).length,
    resolved: comments.filter((comment) => matches(comment, "resolved")).length,
    outdated: comments.filter((comment) => matches(comment, "outdated")).length,
  });
  // Bulk deletion spans every author; the list and its counts follow both filters.
  const counts = countsIn(review.comments);
  const authored = review.comments.filter((comment) =>
    authorMatches(comment, author),
  );
  const shownCounts = countsIn(authored);
  const activeCount = filter === "all" ? authored.length : shownCounts[filter];
  const deleteCount =
    deleteSelection === null
      ? 0
      : deleteSelection === "all"
        ? review.comments.length
        : counts[deleteSelection];
  const shown = (comment: ReviewComment) =>
    matches(comment, filter) && authorMatches(comment, author);
  const currentComments = review.currentComments.filter(shown);
  const outdatedComments = review.outdatedComments.filter(shown);
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
                      aria-label={`Filter comments: ${filterLabel(filter)}${author === "everyone" ? "" : `, ${authorLabel(author)}`}`}
                    />
                  }
                >
                  {filterLabel(filter)}
                  {author !== "everyone" && ` · ${authorLabel(author)}`}
                  <CountBadge count={activeCount} />
                  <ChevronDownIcon className="size-3" aria-hidden="true" />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="start"
                  className="w-max min-w-30"
                  aria-label="Filter comments"
                >
                  {/* Status and author share one menu so the panel's
                      controls keep one row in the default sidebar. */}
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Status</DropdownMenuLabel>
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
                                ? authored.length
                                : shownCounts[value]
                            }
                          />
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuGroup>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Author</DropdownMenuLabel>
                    <DropdownMenuRadioGroup
                      value={author}
                      onValueChange={(value: unknown) => {
                        if (
                          value === "everyone" ||
                          value === "me" ||
                          value === "agents"
                        )
                          setAuthor(value);
                      }}
                    >
                      {AUTHOR_FILTERS.map((value) => (
                        <DropdownMenuRadioItem
                          key={value}
                          value={value}
                          closeOnClick
                          indicator="start"
                          className="gap-1"
                        >
                          <span>{authorLabel(value)}</span>
                          <CountBadge
                            className="ml-auto"
                            count={
                              review.comments.filter((comment) =>
                                authorMatches(comment, value),
                              ).length
                            }
                          />
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuGroup>
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
            <p className="comment-empty">{emptyLabel(filter, author)}</p>
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
