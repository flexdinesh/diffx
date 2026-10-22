import type { ApiRepositoryDiff as RepositoryDiff } from "@diffx/api";
import {
  CheckCircle2Icon,
  ChevronDownIcon,
  CircleDotIcon,
  CopyIcon,
  PencilIcon,
  PinIcon,
  RotateCcwIcon,
  SendIcon,
  Trash2Icon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import {
  useDiffSource,
  useDraft,
  useNavigation,
  useReviewState,
} from "./app-state.tsx";
import { useAppearance } from "./appearance-context.tsx";
import { CopyPathButton } from "./CopyPathButton.tsx";
import {
  commentInFile,
  commentStale,
  placedInline,
  placedOutdated,
  placementFor,
  type CommentPlacement,
  type ReviewComment,
} from "./review-model.ts";

function lines(start: number, end: number) {
  return `${start}${end !== start ? `–${end}` : ""}`;
}

// Inline comments name their current lines; outdated ones their original lines.
function location(comment: ReviewComment, placement: CommentPlacement) {
  if (comment.target === "file") return comment.path;
  return placedInline(placement.applicability)
    ? `${comment.path}:${lines(placement.start, placement.end)}`
    : `${comment.path}:${lines(comment.start, comment.end)}`;
}

function draftLocation(draft: ReviewComment) {
  return draft.target === "file"
    ? draft.path
    : `${draft.path}:${lines(draft.start, draft.end)}`;
}

function placementNote(comment: ReviewComment, placement: CommentPlacement) {
  if (comment.carriedFrom)
    return `Outdated — kept open from line ${lines(comment.carriedFrom.start, comment.carriedFrom.end)} of earlier code`;
  switch (placement.applicability) {
    case "moved":
      return `Moved from line ${lines(comment.start, comment.end)}`;
    case "outdated":
      return "Outdated — the commented code changed or left the diff";
    case "unchanged":
      return "Outdated — the commented code is no longer changed";
    case "unknown":
      return "Original code preserved";
    default:
      return "";
  }
}

// Both the diff annotations and sidebar edit the same page-level draft.
export function DraftComment() {
  const { draft, setDraft } = useDraft();
  const review = useReviewState();
  return draft ? (
    <CommentEditor
      draft={draft}
      onChange={(body) =>
        setDraft((previous) => (previous ? { ...previous, body } : null))
      }
      onSave={review.submit}
      onCancel={review.cancel}
      isSaving={review.pending.has(draft.id) || review.pending.has("*")}
      saveError={
        review.saveError?.draftId === draft.id ? review.saveError.message : ""
      }
    />
  ) : null;
}

export function ReviewCommentCard({
  comment,
  sidebar = false,
}: {
  comment: ReviewComment;
  sidebar?: boolean;
}) {
  const { repository } = useDiffSource();
  const { layout } = useAppearance();
  const review = useReviewState();
  const { navigateComment } = useNavigation();
  const file = repository?.files.find((file) => file.path === comment.path);
  const oneSided =
    file?.status === "A" || file?.status === "D" || file?.status === "?";
  return (
    <CommentCard
      comment={comment}
      sidebar={sidebar}
      layout={layout}
      oneSided={oneSided}
      repository={repository}
      onNavigate={navigateComment}
      onEdit={(comment) => {
        if (review.edit(comment)) navigateComment(comment);
      }}
      onToggle={review.toggle}
      onCarry={review.carry}
      onDelete={review.remove}
      onCopy={sidebar ? review.copySidebarComment : review.copyComment}
    />
  );
}

export function CommentEditor({
  draft,
  onChange,
  onSave,
  onCancel,
  isSaving,
  saveError,
}: {
  draft: ReviewComment;
  onChange: (body: string) => void;
  onSave: () => void;
  onCancel: () => void;
  isSaving: boolean;
  saveError: string;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  const inputId = useId();
  const errorId = useId();
  const [validationError, setError] = useState("");
  const error = validationError || saveError;
  const id = draft.id;
  useEffect(() => {
    if (!id) return;
    input.current?.focus({ preventScroll: true });
    input.current?.scrollIntoView({ block: "nearest" });
  }, [id]);
  function saveComment() {
    if (isSaving) return;
    if (!draft.body.trim()) {
      setError("Enter a comment before saving.");
      input.current?.focus();
      return;
    }
    setError("");
    onSave();
  }
  return (
    <form
      className="comment-editor-form"
      onSubmit={(event) => {
        event.preventDefault();
        saveComment();
      }}
    >
      <Card size="sm" className="comment-editor">
        <CardHeader className="comment-editor-header">
          <CardTitle className="comment-editor-title">
            <small>{draftLocation(draft)}</small>
            <CopyPathButton
              value={
                draft.target === "file"
                  ? draft.path
                  : `${draft.path}:${draft.start}${draft.end !== draft.start ? `-${draft.end}` : ""}`
              }
              label={
                draft.target === "file" ? "Copy path" : "Copy path and lines"
              }
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="comment-editor-content">
          <label className="comment-editor-label" htmlFor={inputId}>
            Review comment
          </label>
          <Textarea
            id={inputId}
            ref={input}
            placeholder="Leave a review comment…"
            aria-invalid={!!error}
            aria-describedby={error ? errorId : undefined}
            rows={3}
            value={draft.body}
            onChange={(event) => {
              if (event.target.value.trim()) setError("");
              onChange(event.target.value);
            }}
            onKeyDown={(event) => {
              event.stopPropagation();
              if (event.key === "Escape") {
                event.preventDefault();
                onCancel();
              }
              if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                event.preventDefault();
                saveComment();
              }
            }}
          />
          {!!error && (
            <p id={errorId} className="comment-editor-error" role="alert">
              {error}
            </p>
          )}
        </CardContent>
        <CardFooter className="comment-actions">
          <span>⌘ / Ctrl + Enter to save</span>
          <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
            <XIcon aria-hidden="true" />
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            disabled={isSaving}
            aria-busy={isSaving}
          >
            <SendIcon aria-hidden="true" />
            Save comment
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}

export function CommentCard({
  comment,
  sidebar = false,
  layout,
  oneSided,
  repository,
  onNavigate,
  onEdit,
  onToggle,
  onCarry,
  onDelete,
  onCopy,
}: {
  comment: ReviewComment;
  sidebar?: boolean;
  layout: "split" | "unified";
  oneSided: boolean;
  repository: RepositoryDiff | null;
  onNavigate: (comment: ReviewComment) => void;
  onEdit: (comment: ReviewComment) => void;
  onToggle: (comment: ReviewComment) => void;
  onCarry: (comment: ReviewComment) => void;
  onDelete: (comment: ReviewComment) => void;
  onCopy: (comment: ReviewComment) => void;
}) {
  const resolved = comment.status === "resolved";
  const placement = placementFor(comment, repository);
  const { applicability } = placement;
  const stale = commentStale(comment, repository);
  const note =
    applicability === "other-scope" && sidebar
      ? `From ${comment.scope} changes`
      : placementNote(comment, placement);
  // Keep open re-targets the comment to its file in the viewed snapshot.
  const carryable =
    !resolved &&
    !comment.carriedFrom &&
    placedOutdated(applicability) &&
    !!repository?.files.some((file) => commentInFile(comment, file));
  const [expanded, setExpanded] = useState(!resolved && !stale);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const contentId = useId();
  useEffect(
    () => setExpanded(!resolved && !stale),
    [comment.id, resolved, stale],
  );
  return (
    <article data-comment-id={comment.id}>
      <Card
        size="sm"
        className={`comment-card${resolved ? " resolved" : ""}${stale ? " stale" : ""}`}
        data-expanded={expanded}
        data-diff-layout={sidebar ? undefined : layout}
        data-one-sided={!sidebar && oneSided ? "" : undefined}
      >
        <CardHeader className="comment-card-header">
          <Button
            type="button"
            className="comment-collapse"
            variant="ghost"
            size="icon-xs"
            aria-controls={contentId}
            aria-expanded={expanded}
            aria-label={`${expanded ? "Collapse" : "Expand"} ${stale ? "outdated " : resolved ? "resolved " : ""}comment at ${location(comment, placement)}`}
            onClick={() => setExpanded((current) => !current)}
          >
            <ChevronDownIcon aria-hidden="true" />
          </Button>
          <CardTitle className="comment-card-title">
            {sidebar ? (
              <Button
                type="button"
                className="comment-location"
                variant="ghost"
                size="xs"
                title={location(comment, placement)}
                onClick={() => onNavigate(comment)}
              >
                {location(comment, placement)}
              </Button>
            ) : (
              <>
                <strong>Review comment</strong>
                <span>
                  {comment.target === "file"
                    ? "· file"
                    : placedInline(applicability)
                      ? `· line ${lines(placement.start, placement.end)}`
                      : `· line ${lines(comment.start, comment.end)}`}
                </span>
              </>
            )}
          </CardTitle>
          <CardAction>
            <Badge
              variant="outline"
              className="comment-state"
              title={`Lifecycle: ${comment.status}; applicability: ${applicability}`}
            >
              {stale ? (
                <TriangleAlertIcon aria-hidden="true" />
              ) : resolved ? (
                <CheckCircle2Icon aria-hidden="true" />
              ) : (
                <CircleDotIcon aria-hidden="true" />
              )}
              {stale ? "Outdated" : resolved ? "Resolved" : "Open"}
            </Badge>
          </CardAction>
        </CardHeader>
        <div
          id={contentId}
          className="comment-collapse-panel"
          hidden={!expanded}
        >
          <CardContent className="comment-card-content">
            <p className="comment-body">{comment.body}</p>
            {note && <p className="comment-outdated">{note}</p>}
            {(sidebar || stale) && comment.code && (
              <details open={stale && !sidebar}>
                <summary>{stale ? "Original code" : "Code context"}</summary>
                <pre>{comment.code}</pre>
              </details>
            )}
          </CardContent>
          <CardFooter className="comment-actions">
            <Button
              type="button"
              className="copy-comment"
              variant="ghost"
              size={sidebar ? "icon-xs" : "icon-sm"}
              aria-label={`Copy comment at ${location(comment, placement)}`}
              title="Copy comment"
              onClick={() => onCopy(comment)}
            >
              <CopyIcon aria-hidden="true" />
            </Button>
            <Button
              type="button"
              className={sidebar ? "edit-comment" : undefined}
              variant="ghost"
              size={sidebar ? "icon-xs" : "sm"}
              aria-label={sidebar ? "Edit" : undefined}
              title={sidebar ? "Edit comment" : undefined}
              onClick={() => onEdit(comment)}
            >
              <PencilIcon aria-hidden="true" />
              {!sidebar && "Edit"}
            </Button>
            <Button
              type="button"
              variant="outline"
              size={sidebar ? "xs" : "sm"}
              onClick={() => onToggle(comment)}
            >
              {resolved ? (
                <RotateCcwIcon aria-hidden="true" />
              ) : (
                <CheckCircle2Icon aria-hidden="true" />
              )}
              {resolved ? "Reopen" : "Resolve"}
            </Button>
            {carryable && (
              <Button
                type="button"
                variant="outline"
                size={sidebar ? "xs" : "sm"}
                title="Keep this comment open on its file in the current snapshot"
                onClick={() => onCarry(comment)}
              >
                <PinIcon aria-hidden="true" />
                Keep open
              </Button>
            )}
            <Button
              type="button"
              variant="destructive"
              size={sidebar ? "xs" : "sm"}
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2Icon aria-hidden="true" />
              Delete
            </Button>
          </CardFooter>
        </div>
      </Card>
      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent
          className="delete-comment-dialog"
          showCloseButton={false}
        >
          <DialogTitle>Delete comment?</DialogTitle>
          <DialogDescription>
            This permanently removes the comment. This cannot be undone.
          </DialogDescription>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setConfirmDelete(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              className="delete-comment-confirm"
              variant="destructive"
              onClick={() => {
                onDelete(comment);
                setConfirmDelete(false);
              }}
            >
              <Trash2Icon aria-hidden="true" />
              Delete comment
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </article>
  );
}

export function CopyDialog({
  content,
  onClose,
}: {
  content: {
    clipboard: "copied" | "manual";
    label: string;
    text: string;
    title: string;
  };
  onClose: () => void;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  return (
    <Dialog
      open
      onOpenChange={(open) => !open && onClose()}
      onOpenChangeComplete={(open) => {
        if (open && content.clipboard === "manual") input.current?.select();
      }}
    >
      <DialogContent
        className="copy-dialog"
        showCloseButton={false}
        initialFocus={content.clipboard === "manual" ? input : undefined}
      >
        <DialogTitle id="copy-dialog-title">{content.title}</DialogTitle>
        <DialogDescription>
          {content.clipboard === "copied"
            ? "Content copied to clipboard."
            : "Copy the selected text with ⌘C / Ctrl+C."}
        </DialogDescription>
        {content.clipboard === "manual" && (
          <Textarea
            ref={input}
            value={content.text}
            readOnly
            aria-label={content.label}
          />
        )}
        <Button type="button" variant="outline" onClick={onClose}>
          Close
        </Button>
      </DialogContent>
    </Dialog>
  );
}
