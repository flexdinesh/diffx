-- name: CountAuthAttempt :one
-- One statement counts the attempt: a client's window starts at its first
-- attempt and restarts with the first attempt after it ends.
INSERT INTO auth_attempts AS a (client, window_start, attempts) VALUES (@client, @now, 1)
ON CONFLICT (client) DO UPDATE SET
  window_start = CASE WHEN a.window_start <= @expired::timestamptz THEN EXCLUDED.window_start ELSE a.window_start END,
  attempts = CASE WHEN a.window_start <= @expired::timestamptz THEN 1 ELSE a.attempts + 1 END
RETURNING attempts;

-- name: PruneAuthAttempts :exec
-- Skips rows other statements hold, so pruning never waits or deadlocks.
DELETE FROM auth_attempts d WHERE d.client IN (
  SELECT s.client FROM auth_attempts s WHERE s.window_start <= @expired::timestamptz FOR UPDATE SKIP LOCKED
);
