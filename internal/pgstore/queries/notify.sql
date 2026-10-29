-- name: Notify :exec
-- Listeners receive the payload only when the surrounding transaction commits.
SELECT pg_notify(@channel::text, @payload::text);
