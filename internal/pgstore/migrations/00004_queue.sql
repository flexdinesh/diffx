-- +goose Up
-- Durable remote admission. payload is the exact admitted request; a lease
-- token fences workers whose lease expired.
CREATE TABLE ingestion_jobs (
  id text PRIMARY KEY,
  sequence bigint NOT NULL UNIQUE REFERENCES ingestion_records(sequence),
  payload bytea NOT NULL,
  state text NOT NULL,
  attempt integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL,
  lease_until timestamptz NOT NULL DEFAULT '-infinity',
  lease_token text NOT NULL DEFAULT '',
  detail text NOT NULL DEFAULT '',
  context_id text NOT NULL DEFAULT ''
);
CREATE INDEX ingestion_jobs_ready ON ingestion_jobs(state, available_at, lease_until);
