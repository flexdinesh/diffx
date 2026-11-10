-- +goose Up
-- Durable pull request history. Additive only: the previous release ignores
-- the new table and column, so upgrades stay rolling.

-- Admins choose how long history is kept in the console.
ALTER TABLE app_settings ADD COLUMN history_retention_days integer NOT NULL DEFAULT 30
  CHECK (history_retention_days BETWEEN 30 AND 365);

-- One row per followed repository's pull request, written whenever the
-- integration reads it from GitHub. Rows are repository facts shared by every
-- follower; reads narrow them to the reader's GitHub user and active follows.
-- updated_at is GitHub's, so an older read never replaces a newer one.
CREATE TABLE github_pull_request_history (
  repository_id bigint NOT NULL,
  number integer NOT NULL,
  full_name text NOT NULL,
  title text NOT NULL,
  html_url text NOT NULL,
  author_id bigint NOT NULL,
  author_login text NOT NULL,
  draft boolean NOT NULL,
  state text NOT NULL CHECK (state IN ('open', 'closed', 'merged')),
  created_at timestamptz NOT NULL,
  merged_at timestamptz,
  closed_at timestamptz,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY(repository_id, number)
);
CREATE INDEX github_pull_request_history_created ON github_pull_request_history(author_id, created_at);
CREATE INDEX github_pull_request_history_merged ON github_pull_request_history(author_id, merged_at) WHERE merged_at IS NOT NULL;
