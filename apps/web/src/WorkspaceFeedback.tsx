import {
  useDiffSource,
  useReviewState,
  useReviewedFilesState,
} from "./app-state.tsx";
import { ErrorState } from "@diffx/ui/components/error-state";
import { useCatalog } from "./session-context.tsx";

export function WorkspaceFeedback() {
  const { feedback } = useReviewState();
  const { error, retry } = useReviewedFilesState();
  const { collectError } = useCatalog();
  const { refresh } = useDiffSource();
  return (
    <div
      className="workspace-feedback"
      hidden={!feedback && !error && !collectError}
    >
      <p
        id="comment-feedback"
        className="text-xs leading-copy text-accent [overflow-wrap:anywhere]"
        role="status"
        aria-live="polite"
      >
        {feedback}
      </p>
      {error && <ErrorState message={error} onRetry={retry} />}
      {collectError && <ErrorState message={collectError} onRetry={refresh} />}
    </div>
  );
}
