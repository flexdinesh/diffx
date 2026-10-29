-- name: OwnedJob :one
SELECT j.id, r.submission_id, j.state, j.attempt, j.context_id, j.detail
FROM ingestion_jobs j JOIN ingestion_records r ON r.sequence = j.sequence
WHERE r.owner_id = @owner_id AND j.id = @id;

-- name: ClaimJob :one
-- The oldest available job; SKIP LOCKED lets concurrent workers claim others.
WITH next AS (
  SELECT q.id FROM ingestion_jobs q
  WHERE (q.state IN ('queued', 'retrying') AND q.available_at <= @now::timestamptz) OR (q.state = 'running' AND q.lease_until <= @now::timestamptz)
  ORDER BY q.sequence LIMIT 1
  FOR UPDATE SKIP LOCKED
)
UPDATE ingestion_jobs j SET state = 'running', attempt = j.attempt + 1, lease_token = @lease_token, lease_until = @lease_until
FROM next, ingestion_records r
WHERE j.id = next.id AND r.sequence = j.sequence
RETURNING j.id, r.owner_id, r.submission_id, j.payload, j.attempt;

-- name: RenewJob :execrows
UPDATE ingestion_jobs SET lease_until = @lease_until
WHERE id = @id AND lease_token = @lease_token AND state = 'running' AND lease_until > @now;

-- name: CompleteJob :execrows
UPDATE ingestion_jobs SET state = 'succeeded', context_id = @context_id, payload = '', detail = '', lease_token = ''
WHERE id = @id AND lease_token = @lease_token AND state = 'running' AND lease_until > @now;

-- name: RetryJob :execrows
UPDATE ingestion_jobs SET state = 'retrying', available_at = @available_at, detail = @detail, lease_token = ''
WHERE id = @id AND lease_token = @lease_token AND state = 'running' AND lease_until > @now;

-- name: FailJob :execrows
UPDATE ingestion_jobs SET state = 'failed', detail = @detail, payload = '', lease_token = ''
WHERE id = @id AND lease_token = @lease_token AND state = 'running' AND lease_until > @now;
