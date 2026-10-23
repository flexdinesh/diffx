import type { ApiRepositoryDiff as RepositoryDiff } from "@diffx/api";
import {
  CheckCircle2Icon,
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
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DisclosureIcon } from "@/components/disclosure";
import { IconButton } from "@/components/icon-button";
import { Shortcut } from "@/components/shortcut";
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
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { formatShortcut } from "./format.ts";
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

// Inline annotations keep generous outer spacing; sidebar cards stack.
function cardSpacing(sidebar: boolean) {
  return sidebar ? "my-2" : "mx-6 my-4 max-md:mx-1 max-md:my-2";
}

// Both the diff annotations and sidebar edit the same page-level draft.
export function DraftComment({ sidebar = false }: { sidebar?: boolean }) {
  const { draft, setDraft } = useDraft();
  const review = useReviewState();
  return draft ? (
    <CommentEditor
      draft={draft}
      sidebar={sidebar}
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
  sidebar = false,
  onChange,
  onSave,
  onCancel,
  isSaving,
  saveError,
}: {
  draft: ReviewComment;
  sidebar?: boolean;
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
      onSubmit={(event) => {
        event.preventDefault();
        saveComment();
      }}
    >
      <Card
        active
        density={sidebar ? "panel" : "inline"}
        className={cardSpacing(sidebar)}
      >
        <CardHeader>
          <CardTitle className="gap-2 [overflow-wrap:anywhere]">
            <span>{draftLocation(draft)}</span>
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
        <CardContent>
          <label
            className="mb-1 block text-sm font-semibold text-fg"
            htmlFor={inputId}
          >
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
            <p id={errorId} className="mt-1 text-sm text-error" role="alert">
              {error}
            </p>
          )}
        </CardContent>
        <CardFooter>
          {!sidebar && (
            <span className="mr-auto text-xs text-muted max-md:hidden">
              <Shortcut keys={{ mod: true, key: "Enter" }} /> to save
            </span>
          )}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            touch
            onClick={onCancel}
          >
            <XIcon aria-hidden="true" />
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            touch
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
        density={sidebar ? "panel" : "inline"}
        className={cn(
          cardSpacing(sidebar),
          // Unified and one-sided rows span the file; bound them to reading width.
          !sidebar &&
            (layout === "unified" || oneSided) &&
            "w-[min(var(--reading-width),calc(100%-2*var(--space-6)))] max-md:w-[calc(100%-2*var(--space-1))]",
        )}
        data-expanded={expanded}
        data-diff-layout={sidebar ? undefined : layout}
        data-one-sided={!sidebar && oneSided ? "" : undefined}
      >
        <CardHeader>
          <IconButton
            label={`${expanded ? "Collapse" : "Expand"} ${stale ? "outdated " : resolved ? "resolved " : ""}comment at ${location(comment, placement)}`}
            variant="subtle"
            size="icon-xs"
            aria-controls={contentId}
            aria-expanded={expanded}
            onClick={() => setExpanded((current) => !current)}
          >
            <DisclosureIcon open={expanded} />
          </IconButton>
          <CardTitle>
            {sidebar ? (
              <Button
                type="button"
                className="min-w-0 shrink truncate"
                variant="link"
                size="xs"
                touch
                title={location(comment, placement)}
                onClick={() => onNavigate(comment)}
              >
                <span className="min-w-0 truncate font-mono font-normal">
                  {location(comment, placement)}
                </span>
              </Button>
            ) : (
              <>
                <strong className="font-semibold whitespace-nowrap text-fg">
                  Review comment
                </strong>
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
              variant="status"
              tone={stale ? "warning" : resolved ? "success" : "neutral"}
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
        <div id={contentId} hidden={!expanded}>
          <CardContent>
            <p className="text-md whitespace-pre-wrap [overflow-wrap:anywhere]">
              {comment.body}
            </p>
            {note && <p className="my-2 text-xs text-muted">{note}</p>}
            {(sidebar || stale) && comment.code && (
              <details
                open={stale && !sidebar}
                className="mt-2 text-xs text-muted"
              >
                <summary className="min-h-control-compact cursor-pointer max-md:min-h-touch pointer-coarse:min-h-touch">
                  {stale ? "Original code" : "Code context"}
                </summary>
                <pre className="max-h-48 overflow-auto rounded-sm bg-bg p-2 font-mono text-sm leading-copy whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">
                  {comment.code}
                </pre>
              </details>
            )}
          </CardContent>
          <CardFooter>
            <IconButton
              label={`Copy comment at ${location(comment, placement)}`}
              size={sidebar ? "icon-xs" : "icon-sm"}
              touch
              onClick={() => onCopy(comment)}
            >
              <CopyIcon aria-hidden="true" />
            </IconButton>
            <Button
              type="button"
              variant="ghost"
              size={sidebar ? "xs" : "sm"}
              touch
              className="mr-auto"
              onClick={() => onEdit(comment)}
            >
              <PencilIcon aria-hidden="true" />
              Edit
            </Button>
            <Button
              type="button"
              variant="outline"
              size={sidebar ? "xs" : "sm"}
              touch
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
                touch
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
              touch
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2Icon aria-hidden="true" />
              Delete
            </Button>
          </CardFooter>
        </div>
      </Card>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete comment?"
        description="This permanently removes the comment. This cannot be undone."
        confirmLabel="Delete comment"
        onConfirm={() => {
          onDelete(comment);
          setConfirmDelete(false);
        }}
      />
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
        initialFocus={content.clipboard === "manual" ? input : undefined}
      >
        <DialogTitle id="copy-dialog-title">{content.title}</DialogTitle>
        <DialogDescription>
          {content.clipboard === "copied"
            ? "Content copied to clipboard."
            : `Copy the selected text with ${formatShortcut({ mod: true, key: "C" })}.`}
        </DialogDescription>
        {content.clipboard === "manual" && (
          <Textarea
            ref={input}
            variant="code"
            rows={15}
            value={content.text}
            readOnly
            aria-label={content.label}
          />
        )}
        <Button type="button" variant="outline" touch onClick={onClose}>
          Close
        </Button>
      </DialogContent>
    </Dialog>
  );
}
