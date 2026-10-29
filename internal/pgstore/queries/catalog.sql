-- name: ContextForDelete :one
-- Deletion locks the context first, as reuse and pruning do.
SELECT c.anchor_diff_id, o.stream_key FROM contexts c LEFT JOIN observations o ON o.context_id = c.id
WHERE c.owner_id = $1 AND c.id = $2 FOR UPDATE OF c;

-- name: LockStreamReviews :exec
-- Writers key-share lock reviews before snapshot versions; deletion takes the
-- reviews first so ending a stream cannot deadlock with them.
SELECT id FROM reviews WHERE owner_id = $1 AND stream_key = $2 FOR UPDATE;

-- name: DeleteContextDiffs :exec
DELETE FROM diffs WHERE owner_id = @owner_id::text AND id <> @anchor_id::text AND id IN (
  SELECT diff_id FROM observation_scopes WHERE context_id = @context_id::text
  UNION SELECT diff_id FROM observation_commits WHERE context_id = @context_id::text
);

-- name: DeleteDiff :exec
DELETE FROM diffs WHERE owner_id = $1 AND id = $2;

-- name: EndStream :exec
-- Review state ends with its stream's last observation.
DELETE FROM reviews WHERE owner_id = @owner_id::text AND stream_key = @stream_key::text AND NOT EXISTS (
  SELECT 1 FROM observations o WHERE o.owner_id = @owner_id::text AND o.stream_key = @stream_key::text
);

-- name: ContextExpiry :one
SELECT d.expires_at FROM contexts c LEFT JOIN diffs d ON d.id = c.anchor_diff_id WHERE c.owner_id = $1 AND c.id = $2;
