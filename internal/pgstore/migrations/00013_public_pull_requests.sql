-- +goose Up
-- Public pull request previews. Additive only: the previous release ignores
-- every new column and the reserved owner, so upgrades stay rolling.

-- Admins turn previews on in the console; previews are kept for their own
-- retention after each fetch.
ALTER TABLE app_settings ADD COLUMN public_pull_requests boolean NOT NULL DEFAULT false;
ALTER TABLE app_settings ADD COLUMN public_retention_days integer NOT NULL DEFAULT 1
  CHECK (public_retention_days BETWEEN 1 AND 30);

-- The reserved owner of every shared public snapshot. It has no
-- remote_accounts row, so it never signs in, is never listed and never
-- receives notifications.
INSERT INTO users(id, name, created_at) VALUES ('github-public', 'Public pull requests', now())
ON CONFLICT (id) DO NOTHING;

-- Public demand on a pull request's refresh row. public_generation is the
-- generation a public request advanced to (0: never requested); a refresh
-- whose lease covers it admits the public owner. The last public admission
-- names its ingestion job, fetch time and captured head; public_pull is the
-- GitHub pull request last read for a public request, and public_checked_at
-- when a public request last read GitHub.
ALTER TABLE github_pull_requests ADD COLUMN public_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE github_pull_requests ADD COLUMN public_job_id text NOT NULL DEFAULT '';
ALTER TABLE github_pull_requests ADD COLUMN public_fetched_at timestamptz;
ALTER TABLE github_pull_requests ADD COLUMN public_head text NOT NULL DEFAULT '';
ALTER TABLE github_pull_requests ADD COLUMN public_checked_at timestamptz;
ALTER TABLE github_pull_requests ADD COLUMN public_pull bytea NOT NULL DEFAULT '';
-- Public status is addressed by GitHub's case-insensitive name.
CREATE INDEX github_pull_requests_name ON github_pull_requests(lower(full_name), number);
