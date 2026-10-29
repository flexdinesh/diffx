-- Advisory lock namespaces (two-key form; distinct from single-key schema locks):
-- 7469101 serializes an owner's reuse decisions per content identity;
-- 7469102 serializes adding observations to, and ending, a review stream.

-- name: LockIdentity :exec
SELECT pg_advisory_xact_lock(7469101, hashtext(@owner_id::text || '/' || @identity_hash::text));

-- name: LockStream :exec
SELECT pg_advisory_xact_lock(7469102, hashtext(@owner_id::text || '/' || @stream_key::text));

-- name: ReusableObservation :one
-- The context row lock orders reuse before or after deletion and pruning.
SELECT i.context_id FROM observation_identities i
JOIN contexts c ON c.id = i.context_id JOIN diffs d ON d.id = c.anchor_diff_id
WHERE i.owner_id = @owner_id AND i.identity_hash = @identity_hash AND d.expires_at > @now::timestamptz
FOR UPDATE OF c;

-- name: RefreshContext :exec
UPDATE contexts SET last_submitted_at = @now WHERE id = @id;

-- name: ExtendObservation :exec
UPDATE diffs SET expires_at = @expires_at::timestamptz WHERE id IN (
  SELECT diff_id FROM observation_scopes WHERE context_id = @context_id::text
  UNION SELECT diff_id FROM observation_commits WHERE context_id = @context_id::text
);

-- name: ForgetIdentity :exec
DELETE FROM observation_identities WHERE owner_id = $1 AND identity_hash = $2;

-- name: InsertRepository :exec
INSERT INTO observation_repositories(id, owner_id, repository_key) VALUES ($1, $2, $3)
ON CONFLICT (owner_id, repository_key) DO NOTHING;

-- name: RepositoryID :one
SELECT id FROM observation_repositories WHERE owner_id = $1 AND repository_key = $2;

-- name: InsertDiff :exec
INSERT INTO diffs(id, owner_id, kind, mode, created_at, expires_at)
VALUES (@id, @owner_id, 'observation', @mode, @created_at, @expires_at::timestamptz);

-- name: InsertDiffVersion :exec
INSERT INTO diff_versions(id, diff_id, revision, manifest, source, comparison_kind, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertDiffFiles :exec
INSERT INTO diff_files(version_id, file_id, path, fingerprint, patch, patch_digest)
SELECT @version_id::text, unnest(@file_ids::text[]), unnest(@paths::text[]), unnest(@fingerprints::text[]),
  unnest(@patches::bytea[]), unnest(@patch_digests::text[]);

-- name: EnsureReview :exec
INSERT INTO reviews(id, owner_id, stream_key, mode) VALUES ($1, $2, $3, $4)
ON CONFLICT (owner_id, stream_key, mode) DO NOTHING;

-- name: InsertContext :exec
INSERT INTO contexts(id, owner_id, kind, anchor_diff_id, created_at, last_submitted_at, submitted_from)
VALUES (@id, @owner_id, 'observation', @id, @now, @now, @submitted_from);

-- name: InsertObservation :exec
INSERT INTO observations(context_id, owner_id, source_id, submission_id, payload_hash, repository_id, metadata, stream_key, comparison_policy)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: InsertScope :exec
INSERT INTO observation_scopes(context_id, mode, diff_id, version_id) VALUES ($1, $2, $3, $4);

-- name: InsertObservationSubmission :exec
INSERT INTO observation_submissions(owner_id, source_id, submission_id, context_id, payload_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: BindSubmission :exec
UPDATE ingestion_records SET context_id = $2 WHERE sequence = $1;

-- name: AdvanceStreamHead :exec
-- Equal collection times use arrival order. Retries bypass this write.
INSERT INTO observation_stream_heads(owner_id, source_id, repository_key, checkout_key, branch, comparison_policy, context_id, collected_at, arrival_sequence)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (owner_id, source_id, repository_key, checkout_key, branch, comparison_policy) DO UPDATE
SET context_id = excluded.context_id, collected_at = excluded.collected_at, arrival_sequence = excluded.arrival_sequence
WHERE excluded.collected_at > observation_stream_heads.collected_at
  OR (excluded.collected_at = observation_stream_heads.collected_at AND excluded.arrival_sequence > observation_stream_heads.arrival_sequence);

-- name: InsertIdentity :exec
INSERT INTO observation_identities(owner_id, identity_hash, context_id) VALUES ($1, $2, $3);

-- name: ObservationRepository :one
SELECT o.repository_id FROM observations o JOIN contexts c ON c.id = o.context_id JOIN diffs d ON d.id = c.anchor_diff_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND d.expires_at > @now::timestamptz;

-- name: ObservationScopes :many
SELECT mode, diff_id FROM observation_scopes WHERE context_id = $1;

-- name: ObservationSnapshot :one
SELECT v.manifest, o.metadata,
  (SELECT array_agg(a.mode) FROM observation_scopes a WHERE a.context_id = o.context_id)::text[] AS modes
FROM observations o JOIN observation_scopes s ON s.context_id = o.context_id
JOIN diff_versions v ON v.id = s.version_id JOIN diffs d ON d.id = s.diff_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND s.mode = @mode AND d.expires_at > @now::timestamptz;

-- name: ObservationFileVersion :one
-- Missing files retain the scope identity, so callers reject stale scope
-- references before file errors.
SELECT s.diff_id, s.version_id, f.fingerprint
FROM observations o JOIN observation_scopes s ON s.context_id = o.context_id
JOIN diffs d ON d.id = s.diff_id
LEFT JOIN diff_files f ON f.version_id = s.version_id AND f.file_id = @file_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND s.mode = @mode AND d.expires_at > @now::timestamptz;

-- name: ObservationPatch :one
SELECT f.patch FROM observations o JOIN observation_scopes s ON s.context_id = o.context_id
JOIN diff_files f ON f.version_id = s.version_id JOIN diffs d ON d.id = s.diff_id
WHERE o.owner_id = @owner_id AND o.context_id = @context_id AND s.mode = @mode
  AND f.file_id = @file_id AND f.fingerprint = @fingerprint AND d.expires_at > @now::timestamptz;

-- name: LockExpiredContexts :exec
-- Pruning locks contexts before their diffs, in the order deletion and reuse
-- lock them, so concurrent writers cannot deadlock with it.
SELECT c.id FROM contexts c JOIN diffs d ON d.id = c.anchor_diff_id
WHERE d.expires_at <= @now::timestamptz ORDER BY c.id FOR UPDATE OF c;

-- name: LockEndingStreams :exec
-- Streams left without observations, locked in key order before ending them.
SELECT pg_advisory_xact_lock(7469102, s.key) FROM (
  SELECT DISTINCT hashtext(r.owner_id || '/' || r.stream_key) AS key FROM reviews r
  WHERE NOT EXISTS (SELECT 1 FROM observations o WHERE o.owner_id = r.owner_id AND o.stream_key = r.stream_key)
  ORDER BY key
) s;
