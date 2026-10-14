import {
  useDiffSource,
  useReviewState,
  useReviewedFilesState,
} from "./app-state.tsx";
import { Button } from "@/components/ui/button";
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
      <p id="comment-feedback" role="status" aria-live="polite">
        {feedback}
      </p>
      {error && (
        <div className="reviewed-error" role="alert">
          <p>{error}</p>
          <Button type="button" variant="outline" size="sm" onClick={retry}>
            Retry reviewed files
          </Button>
        </div>
      )}
      {collectError && (
        <div className="reviewed-error" role="alert">
          <p>{collectError}</p>
          <Button type="button" variant="outline" size="sm" onClick={refresh}>
            Retry refresh
          </Button>
        </div>
      )}
    </div>
  );
}
