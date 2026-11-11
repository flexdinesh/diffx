-- +goose Up
-- Pull request notification preferences. Additive only: the previous release
-- ignores the new table and column, so upgrades stay rolling.

-- Whether the last captured snapshot was a draft, so becoming ready for
-- review announces the pull request.
ALTER TABLE github_pr_heads ADD COLUMN draft boolean NOT NULL DEFAULT false;

-- One row per account that saved its preferences; an absent row means the
-- defaults. Preferences belong to the account, not its GitHub connection, so
-- they survive disconnecting GitHub.
CREATE TABLE github_notification_preferences (
  owner_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  opened_by_me boolean NOT NULL,
  opened_by_others boolean NOT NULL,
  new_commits_by_me boolean NOT NULL,
  new_commits_by_others boolean NOT NULL,
  merged_by_me boolean NOT NULL,
  merged_by_others boolean NOT NULL,
  closed_by_me boolean NOT NULL,
  closed_by_others boolean NOT NULL,
  skip_drafts boolean NOT NULL,
  only_authors text[] NOT NULL DEFAULT '{}',
  never_authors text[] NOT NULL DEFAULT '{}',
  base_branches text[] NOT NULL DEFAULT '{}',
  muted_title_terms text[] NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL
);
