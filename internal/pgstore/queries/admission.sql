-- name: InsertIngestionRecord :exec
INSERT INTO ingestion_records(owner_id, source_id, submission_id, payload_hash)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: IngestionRecord :one
SELECT sequence, payload_hash, context_id FROM ingestion_records
WHERE owner_id = $1 AND source_id = $2 AND submission_id = $3;

-- name: JobBySequence :one
SELECT j.id, r.submission_id, j.state, j.attempt, j.context_id, j.detail
FROM ingestion_jobs j JOIN ingestion_records r ON r.sequence = j.sequence
WHERE j.sequence = $1;

-- name: ActiveJobUsage :one
SELECT count(*)::bigint AS jobs, COALESCE(sum(length(payload)), 0)::bigint AS bytes
FROM ingestion_jobs WHERE state IN ('queued', 'running', 'retrying');

-- name: InsertJob :exec
INSERT INTO ingestion_jobs(id, sequence, payload, state, available_at, context_id)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: InsertNotification :exec
INSERT INTO push_notifications(id, owner_id, job_id, kind, payload, state, available_at, created_at)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6);

-- name: PruneFinishedNotifications :exec
DELETE FROM push_notifications WHERE state <> 'pending' AND created_at <= $1;

-- name: PruneGitHubStates :exec
DELETE FROM github_oauth_states WHERE expires_at <= $1;

-- name: PruneGitHubDeliveries :exec
DELETE FROM github_deliveries WHERE received_at <= $1;
