import type { ApiRepositoryDiff as RepositoryDiff } from "@diffx/api";
import {
  BotIcon,
  CheckCircle2Icon,
  CircleDotIcon,
  CopyIcon,
  LinkIcon,
  PencilIcon,
  PinIcon,
  RotateCcwIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { DisclosureIcon } from "@diffx/ui/components/disclosure";
import { IconButton } from "@diffx/ui/components/icon-button";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@diffx/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@diffx/ui/components/ui/dialog";
import { Textarea } from "@diffx/ui/components/ui/textarea";
import { cn } from "@diffx/ui/lib/utils";
import { formatShortcut } from "@diffx/ui/lib/format";
import {
  useDiffSource,
  useDraft,
  useNavigation,
  useReviewState,
} from "./app-state.tsx";
import { useAppearance } from "./appearance-context.tsx";
import { CopyPathButton } from "./CopyPathButton.tsx";
import { relativeAge } from "./project-nav.ts";
import {
  authorName,
  commentInFile,
  commentStale,
  placedInline,
  placedOutdated,
  placementFor,
  writtenByAgent,
  type CommentAuthor,
  type CommentPlacement,
  type CommentReply,
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
  return sidebar ? "my-2" : "mx-6 my-2 max-md:mx-1";
}

// Both the diff annotations and sidebar edit the same page-level draft.
export function DraftComment({ sidebar = false }: { sidebar?: boolean }) {
  const { draft, setDraft } = useDraft();
  const review = useReviewState();
  return draft ? (
    <CommentEditor
      draft={draft}
      editing={review.comments.some((comment) => comment.id === draft.id)}
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
      onCopyLink={review.copyLink}
      thread={{
        draft: review.replyDrafts.get(comment.id),
        onDraft: (body) => review.setReplyDraft(comment.id, body),
        onReply: (body) => review.addReply(comment, body),
        onEdit: (reply, body) => review.editReply(comment, reply, body),
        onDelete: (reply) => void review.deleteReply(comment, reply),
        pending: (id) => review.pending.has(`${comment.id}:${id}`),
      }}
    />
  );
}

/** Thread state and writes for one comment's replies and reply composer. */
interface ThreadActions {
  /** The open composer's text; undefined while no composer is open. */
  draft: string | undefined;
  /** Updates the composer's text; null closes it. */
  onDraft: (body: string | null) => void;
  onReply: (body: string) => Promise<boolean>;
  onEdit: (reply: CommentReply, body: string) => Promise<boolean>;
  onDelete: (reply: CommentReply) => void;
  /** A write is in flight for a reply ID, or "reply" for a new reply. */
  pending: (id: string) => boolean;
}

const UNVERIFIED_NAME = "Name reported by the agent";

/** The user reads as "You"; agents as their reported name beside a bot icon. */
function AuthorLabel({ author }: { author: CommentAuthor | undefined }) {
  const agent = writtenByAgent(author);
  const unverified = agent && !author?.verified;
  return (
    <span
      // "You" stays whole; a reported name shares the row with the location.
      className={cn(
        "flex items-center gap-1",
        agent ? "min-w-0 shrink" : "shrink-0",
      )}
      title={unverified ? UNVERIFIED_NAME : undefined}
    >
      {agent && (
        <BotIcon className="size-icon-sm shrink-0" aria-hidden="true" />
      )}
      <span className="min-w-0 truncate">{authorName(author)}</span>
      {unverified && <span className="sr-only">{` (${UNVERIFIED_NAME})`}</span>}
    </span>
  );
}

function timestamp(milliseconds: number) {
  const date = new Date(milliseconds);
  return { iso: date.toISOString(), full: date.toLocaleString() };
}

/** A compact reply or reply-edit field with its own actions. */
function ReplyComposer({
  label,
  value,
  submitLabel,
  sidebar,
  pending,
  focusRequest,
  onChange,
  onSubmit,
  onCancel,
}: {
  label: string;
  value: string;
  submitLabel: string;
  sidebar: boolean;
  pending: boolean;
  /** Each new non-zero value moves focus to the field. */
  focusRequest: number;
  onChange: (value: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  const inputId = useId();
  const errorId = useId();
  const [error, setError] = useState("");
  useEffect(() => {
    if (!focusRequest) return;
    input.current?.focus({ preventScroll: true });
    input.current?.scrollIntoView({ block: "nearest" });
  }, [focusRequest]);
  function submit() {
    if (pending) return;
    if (!value.trim()) {
      setError("Enter a reply before sending.");
      input.current?.focus();
      return;
    }
    setError("");
    onSubmit();
  }
  return (
    <form
      className="mt-2"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <label className="sr-only" htmlFor={inputId}>
        {label}
      </label>
      <Textarea
        id={inputId}
        ref={input}
        placeholder="Reply…"
        aria-invalid={!!error}
        aria-describedby={error ? errorId : undefined}
        rows={2}
        variant="comment"
        value={value}
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
            submit();
          }
        }}
      />
      {!!error && (
        <p id={errorId} className="mt-1 text-sm text-error" role="alert">
          {error}
        </p>
      )}
      <div className="mt-2 flex items-center justify-end gap-1">
        {!sidebar && (
          <span className="mr-auto text-xs text-muted max-md:hidden">
            {formatShortcut({ mod: true, key: "Enter" })} to{" "}
            {submitLabel.toLowerCase()}
          </span>
        )}
        <Button
          type="button"
          variant="ghost"
          size="xs"
          touch
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button
          type="submit"
          size="xs"
          touch
          disabled={pending}
          aria-busy={pending}
        >
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

/** One reply: author, age, body, and the user's edit and delete actions. */
function ReplyItem({
  reply,
  sidebar,
  thread,
}: {
  reply: CommentReply;
  sidebar: boolean;
  thread: ThreadActions;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const item = useRef<HTMLLIElement>(null);
  // Agents write append-only; the user edits only their own replies but may
  // delete any reply in their review.
  const own = !writtenByAgent(reply.author);
  const created = timestamp(reply.createdAt);
  const pending = thread.pending(reply.id);
  const restoreFocus = () =>
    requestAnimationFrame(() =>
      item.current
        ?.querySelector<HTMLElement>('[data-reply-action="edit"]')
        ?.focus(),
    );
  return (
    <li ref={item} className="border-t border-separator py-1.5 last:pb-0">
      <div className="flex min-h-control-compact items-center gap-1 text-xs text-muted">
        <AuthorLabel author={reply.author} />
        <span aria-hidden="true">·</span>
        <time className="shrink-0" dateTime={created.iso} title={created.full}>
          {relativeAge(reply.createdAt, Date.now())}
        </time>
        {!!reply.editedAt && <span className="shrink-0">· edited</span>}
        <span className="ml-auto flex shrink-0 items-center gap-1">
          {own && editing === null && (
            <IconButton
              label="Edit reply"
              size="icon-xs"
              touch
              data-reply-action="edit"
              onClick={() => setEditing(reply.body)}
            >
              <PencilIcon aria-hidden="true" />
            </IconButton>
          )}
          <IconButton
            label="Delete reply"
            variant="destructive"
            size="icon-xs"
            touch
            disabled={pending}
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2Icon aria-hidden="true" />
          </IconButton>
        </span>
      </div>
      {editing === null ? (
        <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
          {reply.body}
        </p>
      ) : (
        <ReplyComposer
          label="Edit reply"
          value={editing}
          submitLabel="Save"
          sidebar={sidebar}
          pending={pending}
          focusRequest={1}
          onChange={setEditing}
          onCancel={() => {
            setEditing(null);
            restoreFocus();
          }}
          onSubmit={() => {
            void thread.onEdit(reply, editing).then((saved) => {
              if (!saved) return;
              setEditing(null);
              restoreFocus();
            });
          }}
        />
      )}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete reply?"
        description="This permanently removes the reply. This cannot be undone."
        confirmLabel="Delete reply"
        onConfirm={() => {
          thread.onDelete(reply);
          setConfirmDelete(false);
        }}
      />
    </li>
  );
}

export function CommentEditor({
  draft,
  editing = false,
  sidebar = false,
  onChange,
  onSave,
  onCancel,
  isSaving,
  saveError,
}: {
  draft: ReviewComment;
  /** The draft edits a saved comment rather than starting a new one. */
  editing?: boolean;
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
        density={sidebar ? "panel" : "inline"}
        className={cardSpacing(sidebar)}
      >
        <CardHeader>
          <CardTitle className="gap-2">
            <span className="shrink-0">
              {editing ? "Edit comment" : "New comment"}
            </span>
            <span className="min-w-0 truncate font-mono text-xs">
              {draftLocation(draft)}
            </span>
          </CardTitle>
          <CardAction>
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
          </CardAction>
        </CardHeader>
        <CardContent>
          <label className="sr-only" htmlFor={inputId}>
            Review comment
          </label>
          <Textarea
            id={inputId}
            ref={input}
            placeholder="Leave a review comment…"
            aria-invalid={!!error}
            aria-describedby={error ? errorId : undefined}
            rows={2}
            variant="comment"
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
              {formatShortcut({ mod: true, key: "Enter" })} to{" "}
              {editing ? "save" : "comment"}
            </span>
          )}
          <Button
            type="button"
            variant="ghost"
            size="xs"
            touch
            onClick={onCancel}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="xs"
            touch
            disabled={isSaving}
            aria-busy={isSaving}
          >
            {editing ? "Save" : "Comment"}
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
  onCopyLink,
  thread,
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
  onCopyLink: (comment: ReviewComment) => void;
  thread: ThreadActions;
}) {
  const resolved = comment.status === "resolved";
  const replies = comment.replies ?? [];
  const replyButton = useRef<HTMLButtonElement>(null);
  // Only the card whose Reply was pressed takes focus; the composer's text is
  // shared with the comment's other card (inline or sidebar).
  const [focusReply, setFocusReply] = useState(0);
  const closeReply = () => {
    thread.onDraft(null);
    setFocusReply(0);
    requestAnimationFrame(() => replyButton.current?.focus());
  };
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
              <span>
                {comment.target === "file"
                  ? "File"
                  : placedInline(applicability)
                    ? `Line ${lines(placement.start, placement.end)}`
                    : `Line ${lines(comment.start, comment.end)}`}
              </span>
            )}
            <span aria-hidden="true">·</span>
            <AuthorLabel author={comment.author} />
          </CardTitle>
          <CardAction>
            <Badge
              variant="status"
              tone={stale ? "warning" : resolved ? "success" : "neutral"}
              title={`Lifecycle: ${comment.status}${comment.resolvedBy ? ` by ${authorName(comment.resolvedBy)}` : ""}; applicability: ${applicability}`}
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
            <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
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
                <pre className="max-h-48 overflow-auto bg-bg p-2 font-mono text-sm leading-copy whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">
                  {comment.code}
                </pre>
              </details>
            )}
            {!!replies.length && (
              <ol
                className="mt-2"
                aria-label={`${replies.length} ${replies.length === 1 ? "reply" : "replies"}`}
              >
                {replies.map((reply) => (
                  <ReplyItem
                    key={reply.id}
                    reply={reply}
                    sidebar={sidebar}
                    thread={thread}
                  />
                ))}
              </ol>
            )}
            {/* The resolution closes the thread; the status badge stays short
                so the sidebar header keeps the location and author. */}
            {resolved && comment.resolvedBy && (
              <p
                className={cn(
                  "flex min-h-control-compact items-center gap-1 border-t border-separator pt-1.5 text-xs text-muted",
                  replies.length ? "mt-1.5" : "mt-2",
                )}
              >
                <span className="shrink-0">Resolved by</span>{" "}
                <AuthorLabel author={comment.resolvedBy} />
                {!!comment.resolvedAt && (
                  <>
                    <span aria-hidden="true">·</span>
                    <time
                      className="shrink-0"
                      dateTime={timestamp(comment.resolvedAt).iso}
                      title={timestamp(comment.resolvedAt).full}
                    >
                      {relativeAge(comment.resolvedAt, Date.now())}
                    </time>
                  </>
                )}
              </p>
            )}
            {thread.draft !== undefined && (
              <ReplyComposer
                label={`Reply to comment at ${location(comment, placement)}`}
                value={thread.draft}
                submitLabel="Reply"
                sidebar={sidebar}
                pending={thread.pending("reply")}
                focusRequest={focusReply}
                onChange={thread.onDraft}
                onCancel={closeReply}
                onSubmit={() => {
                  void thread.onReply(thread.draft ?? "").then((saved) => {
                    if (saved) closeReply();
                  });
                }}
              />
            )}
          </CardContent>
          <CardFooter>
            <IconButton
              label={`Copy comment at ${location(comment, placement)}`}
              size="icon-xs"
              touch
              onClick={() => onCopy(comment)}
            >
              <CopyIcon aria-hidden="true" />
            </IconButton>
            <IconButton
              label={`Copy link to comment at ${location(comment, placement)}`}
              size="icon-xs"
              touch
              onClick={() => onCopyLink(comment)}
            >
              <LinkIcon aria-hidden="true" />
            </IconButton>
            {/* Agents' comments are append-only; replies answer them instead. */}
            {!writtenByAgent(comment.author) && (
              <IconButton
                label={`Edit comment at ${location(comment, placement)}`}
                hint="Edit"
                size="icon-xs"
                touch
                onClick={() => onEdit(comment)}
              >
                <PencilIcon aria-hidden="true" />
              </IconButton>
            )}
            {/* Text-only, so the thread and lifecycle actions share one row in
                the default sidebar. */}
            <Button
              ref={replyButton}
              type="button"
              variant="ghost"
              size="xs"
              touch
              className="mr-auto"
              aria-expanded={thread.draft !== undefined}
              onClick={() => {
                setFocusReply((current) => current + 1);
                thread.onDraft(thread.draft ?? "");
              }}
            >
              Reply
            </Button>
            <Button
              type="button"
              variant="outline"
              size="xs"
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
                size="xs"
                touch
                title="Keep this comment open on its file in the current snapshot"
                onClick={() => onCarry(comment)}
              >
                <PinIcon aria-hidden="true" />
                Keep open
              </Button>
            )}
            <IconButton
              label={`Delete comment at ${location(comment, placement)}`}
              hint="Delete"
              variant="destructive"
              size="icon-xs"
              touch
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2Icon aria-hidden="true" />
            </IconButton>
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
