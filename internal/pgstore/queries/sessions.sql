-- name: UpsertAgentSession :exec
-- Names follow collection order; unnamed observations keep the last name.
INSERT INTO agent_sessions(owner_id, source_id, harness, session_id, name, named_at) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (owner_id, source_id, harness, session_id) DO UPDATE SET name = excluded.name, named_at = excluded.named_at
WHERE excluded.name <> '' AND excluded.named_at >= agent_sessions.named_at;

-- name: UpsertObservationSession :exec
INSERT INTO observation_sessions(context_id, owner_id, source_id, harness, session_id, first_observed_at, last_observed_at)
VALUES (@context_id, @owner_id, @source_id, @harness, @session_id, @observed_at, @observed_at)
ON CONFLICT (context_id, source_id, harness, session_id) DO UPDATE
SET first_observed_at = LEAST(observation_sessions.first_observed_at, excluded.first_observed_at),
  last_observed_at = GREATEST(observation_sessions.last_observed_at, excluded.last_observed_at);
