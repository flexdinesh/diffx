-- name: PruneExpiredObservationDiffs :exec
-- Every scope and commit diff of an expired observation ends with its anchor.
DELETE FROM diffs WHERE id IN (
  SELECT s.diff_id FROM observation_scopes s JOIN contexts c ON c.id = s.context_id JOIN diffs d ON d.id = c.anchor_diff_id WHERE d.expires_at <= $1
  UNION SELECT k.diff_id FROM observation_commits k JOIN contexts c ON c.id = k.context_id JOIN diffs d ON d.id = c.anchor_diff_id WHERE d.expires_at <= $1 AND k.diff_id IS NOT NULL
);

-- name: PruneExpiredDiffs :exec
DELETE FROM diffs WHERE expires_at <= $1;

-- name: EndStreams :exec
-- Review state ends with its stream's last observation.
DELETE FROM reviews WHERE NOT EXISTS (
  SELECT 1 FROM observations o WHERE o.owner_id = reviews.owner_id AND o.stream_key = reviews.stream_key
);
