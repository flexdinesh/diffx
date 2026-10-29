-- name: InsertCommitRange :exec
INSERT INTO observation_commit_ranges(context_id, total) VALUES ($1, $2);

-- name: InsertCommit :exec
INSERT INTO observation_commits(context_id, ordinal, oid, data, diff_id, version_id, file_count, additions, deletions)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ObservationCommitTotal :one
SELECT r.total FROM observations o JOIN contexts c ON c.id = o.context_id JOIN diffs d ON d.id = c.anchor_diff_id
LEFT JOIN observation_commit_ranges r ON r.context_id = o.context_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND d.expires_at > @now::timestamptz;

-- name: ObservationCommits :many
SELECT data, file_count, additions, deletions FROM observation_commits WHERE context_id = $1 ORDER BY ordinal;

-- name: ObservationCommitManifest :one
-- Merge commits and commits outside the observation are not found.
SELECT v.manifest FROM observations o JOIN observation_commits k ON k.context_id = o.context_id
JOIN diffs d ON d.id = k.diff_id JOIN diff_versions v ON v.id = k.version_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND k.oid = @oid AND d.expires_at > @now::timestamptz;

-- name: ObservationCommitFileVersion :one
SELECT d.id AS diff_id, v.id AS version_id, f.fingerprint FROM observations o JOIN observation_commits k ON k.context_id = o.context_id
JOIN diffs d ON d.id = k.diff_id JOIN diff_versions v ON v.id = k.version_id LEFT JOIN diff_files f ON f.version_id = k.version_id AND f.file_id = @file_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND k.oid = @oid AND d.expires_at > @now::timestamptz;

-- name: ObservationCommitPatch :one
SELECT f.patch FROM observations o JOIN observation_commits k ON k.context_id = o.context_id
JOIN diffs d ON d.id = k.diff_id JOIN diff_files f ON f.version_id = k.version_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND k.oid = @oid
  AND f.file_id = @file_id AND f.fingerprint = @fingerprint AND d.expires_at > @now::timestamptz;
