-- name: ScopeReview :one
-- Resolves a retained context's scope to its stream review. The key-share
-- lock keeps the review until this write commits, or reports it ended.
SELECT s.diff_id, r.id AS review_id FROM observations o
JOIN observation_scopes s ON s.context_id = o.context_id AND s.mode = @mode JOIN diffs d ON d.id = s.diff_id
JOIN reviews r ON r.owner_id = o.owner_id AND r.stream_key = o.stream_key AND r.mode = s.mode
WHERE o.context_id = @context_id AND (d.expires_at IS NULL OR d.expires_at > @now::timestamptz)
FOR KEY SHARE OF r;

-- name: VersionInDiff :one
SELECT count(*) FROM diff_versions WHERE id = $1 AND diff_id = $2;

-- name: Comments :many
SELECT c.data FROM comments c JOIN reviews r ON r.id = c.review_id
JOIN observations o ON o.owner_id = r.owner_id AND o.stream_key = r.stream_key
JOIN contexts x ON x.id = o.context_id JOIN diffs d ON d.id = x.anchor_diff_id
WHERE o.context_id = @context_id AND (d.expires_at IS NULL OR d.expires_at > @now::timestamptz)
ORDER BY c.created_at, c.id COLLATE "C";

-- name: StoredComment :one
SELECT data FROM comments WHERE id = $1 AND review_id = $2;

-- name: UpdateComment :execrows
-- Pruned snapshot versions leave the reference in data only.
UPDATE comments SET version_id = (SELECT v.id FROM diff_versions v WHERE v.id = @version_id::text), file_id = @file_id, data = @data
WHERE comments.id = @id::text AND review_id = @review_id;

-- name: InsertComment :execrows
INSERT INTO comments(id, review_id, version_id, file_id, data, created_at)
VALUES (@id, @review_id, (SELECT v.id FROM diff_versions v WHERE v.id = @version_id::text), @file_id, @data, @created_at)
ON CONFLICT (id) DO UPDATE SET version_id = excluded.version_id, file_id = excluded.file_id, data = excluded.data
WHERE comments.review_id = excluded.review_id;

-- name: DeleteStreamComments :execrows
DELETE FROM comments WHERE id = ANY(@ids::text[]) AND review_id IN (
  SELECT r.id FROM reviews r JOIN observations o ON o.owner_id = r.owner_id AND o.stream_key = r.stream_key WHERE o.context_id = @context_id
);

-- name: Marks :many
-- Stream marks whose file patch is unchanged in this context, projected onto its snapshot.
SELECT s.diff_id, s.version_id, m.file_id, f.fingerprint
FROM observations o JOIN observation_scopes s ON s.context_id = o.context_id AND s.mode = @mode JOIN diffs d ON d.id = s.diff_id
JOIN reviews r ON r.owner_id = o.owner_id AND r.stream_key = o.stream_key AND r.mode = s.mode
JOIN marks m ON m.review_id = r.id
JOIN diff_files f ON f.version_id = s.version_id AND f.file_id = m.file_id AND f.patch_digest = m.patch_digest
WHERE o.context_id = @context_id AND (d.expires_at IS NULL OR d.expires_at > @now::timestamptz)
ORDER BY m.file_id COLLATE "C";

-- name: ScopeFileDigest :one
SELECT f.patch_digest FROM observation_scopes s JOIN diff_files f ON f.version_id = s.version_id
WHERE s.context_id = $1 AND s.mode = $2 AND f.file_id = $3;

-- name: UpsertMark :exec
INSERT INTO marks(review_id, file_id, version_id, patch_digest, data) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (review_id, file_id) DO UPDATE SET version_id = excluded.version_id, patch_digest = excluded.patch_digest, data = excluded.data;

-- name: DeleteVisibleMarks :exec
-- Removes only marks visible in this context; a null file clears the scope.
DELETE FROM marks WHERE (review_id, file_id) IN (
  SELECT m.review_id, m.file_id
  FROM observations o JOIN observation_scopes s ON s.context_id = o.context_id AND s.mode = @mode
  JOIN reviews r ON r.owner_id = o.owner_id AND r.stream_key = o.stream_key AND r.mode = s.mode
  JOIN marks m ON m.review_id = r.id
  JOIN diff_files f ON f.version_id = s.version_id AND f.file_id = m.file_id AND f.patch_digest = m.patch_digest
  WHERE o.context_id = @context_id AND (sqlc.narg('file_id')::text IS NULL OR m.file_id = sqlc.narg('file_id'))
);

-- name: StoredVersion :one
SELECT v.manifest, o.metadata,
  (SELECT array_agg(a.mode) FROM observation_scopes a WHERE a.context_id = o.context_id)::text[] AS modes
FROM diff_versions v JOIN diffs d ON d.id = v.diff_id
JOIN observation_scopes s ON s.diff_id = d.id JOIN observations o ON o.context_id = s.context_id
WHERE d.owner_id = @owner_id AND d.id = @diff_id AND v.id = @version_id AND (d.expires_at IS NULL OR d.expires_at > @now::timestamptz);
