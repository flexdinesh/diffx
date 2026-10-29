-- +goose Up
-- Observations, their immutable snapshots and per-stream review state.
-- jsonb holds documents SQL queries into; bytea holds opaque encoded values.

CREATE TABLE diffs (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL,
  mode text NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz
);
CREATE INDEX diffs_owner_expiry ON diffs(owner_id, kind, expires_at);
CREATE INDEX diffs_expiry ON diffs(expires_at);

CREATE TABLE diff_versions (
  id text PRIMARY KEY,
  diff_id text NOT NULL REFERENCES diffs(id) ON DELETE CASCADE,
  revision text NOT NULL,
  manifest jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE(diff_id, revision)
);

CREATE TABLE diff_files (
  version_id text NOT NULL REFERENCES diff_versions(id) ON DELETE CASCADE,
  file_id text NOT NULL,
  path text NOT NULL,
  fingerprint text NOT NULL,
  patch bytea NOT NULL,
  patch_digest text NOT NULL,
  PRIMARY KEY(version_id, file_id)
);

-- Review state belongs to a stream and scope, spanning its snapshots. It ends
-- when the stream's last observation is pruned or deleted.
CREATE TABLE reviews (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  stream_key text NOT NULL,
  mode text NOT NULL,
  UNIQUE(owner_id, stream_key, mode)
);

CREATE TABLE comments (
  id text PRIMARY KEY,
  review_id text NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
  version_id text REFERENCES diff_versions(id) ON DELETE SET NULL,
  file_id text NOT NULL,
  data bytea NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE INDEX comments_review_created ON comments(review_id, created_at);

-- Marks carry to snapshots whose stored file patch has the same digest.
CREATE TABLE marks (
  review_id text NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
  file_id text NOT NULL,
  version_id text REFERENCES diff_versions(id) ON DELETE SET NULL,
  patch_digest text NOT NULL,
  data bytea NOT NULL,
  PRIMARY KEY(review_id, file_id)
);

CREATE TABLE contexts (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind = 'observation'),
  anchor_diff_id text NOT NULL UNIQUE REFERENCES diffs(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL,
  last_submitted_at timestamptz NOT NULL,
  submitted_from text
);
CREATE INDEX contexts_owner_order ON contexts(owner_id, last_submitted_at DESC, id DESC);

CREATE TABLE observation_repositories (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  repository_key text NOT NULL,
  UNIQUE(owner_id, repository_key)
);

-- comparison_policy is ingestion.ComparisonPolicy of metadata, stored so the
-- catalog joins stream heads without reinterpreting metadata.
CREATE TABLE observations (
  context_id text PRIMARY KEY REFERENCES contexts(id) ON DELETE CASCADE,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id text NOT NULL,
  submission_id text NOT NULL,
  payload_hash text NOT NULL,
  repository_id text REFERENCES observation_repositories(id),
  metadata jsonb NOT NULL,
  stream_key text NOT NULL,
  comparison_policy text NOT NULL,
  UNIQUE(owner_id, source_id, submission_id)
);
CREATE INDEX observations_source ON observations(owner_id, source_id);
CREATE INDEX observations_stream ON observations(owner_id, stream_key);
CREATE INDEX observations_branch ON observations(owner_id, (metadata->>'branch'));
CREATE INDEX observations_checkout ON observations(owner_id, (metadata->>'repositoryKey'), (metadata->>'checkoutKey'));

-- The all-scope diff anchors the observation; extra scopes have independent
-- review identities.
CREATE TABLE observation_scopes (
  context_id text NOT NULL REFERENCES observations(context_id) ON DELETE CASCADE,
  mode text NOT NULL,
  diff_id text NOT NULL UNIQUE REFERENCES diffs(id) ON DELETE CASCADE,
  version_id text NOT NULL REFERENCES diff_versions(id) ON DELETE CASCADE,
  PRIMARY KEY(context_id, mode)
);

-- Replay identity: account + source + submission. Records survive snapshot
-- deletion and expiry so retries never resurrect content. The sequence orders
-- arrivals; context_id is empty until publication commits.
CREATE TABLE ingestion_records (
  sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id text NOT NULL,
  submission_id text NOT NULL,
  payload_hash text NOT NULL,
  context_id text NOT NULL DEFAULT '',
  UNIQUE(owner_id, source_id, submission_id)
);

CREATE TABLE observation_submissions (
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id text NOT NULL,
  submission_id text NOT NULL,
  context_id text NOT NULL REFERENCES observations(context_id) ON DELETE CASCADE,
  payload_hash text NOT NULL,
  metadata jsonb NOT NULL,
  PRIMARY KEY(owner_id, source_id, submission_id)
);
CREATE INDEX observation_submissions_context ON observation_submissions(context_id);
CREATE INDEX observation_submissions_run ON observation_submissions(owner_id, (metadata->>'runId'), context_id);

-- Stream heads outlive snapshot retention; an existing head never regresses
-- when its snapshot has been pruned.
CREATE TABLE observation_stream_heads (
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id text NOT NULL,
  repository_key text NOT NULL,
  checkout_key text NOT NULL,
  branch text NOT NULL,
  comparison_policy text NOT NULL DEFAULT '',
  context_id text NOT NULL,
  collected_at bigint NOT NULL,
  arrival_sequence bigint NOT NULL DEFAULT 0,
  PRIMARY KEY(owner_id, source_id, repository_key, checkout_key, branch, comparison_policy)
);

CREATE TABLE observation_identities (
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  identity_hash text NOT NULL,
  context_id text NOT NULL REFERENCES observations(context_id) ON DELETE CASCADE,
  PRIMARY KEY(owner_id, identity_hash)
);

-- A branch observation's captured commits, oldest first. Each non-merge
-- commit's diff is a 'commit' mode diff sharing the observation's expiry; it is
-- never a review scope.
CREATE TABLE observation_commits (
  context_id text NOT NULL REFERENCES observations(context_id) ON DELETE CASCADE,
  ordinal integer NOT NULL,
  oid text NOT NULL,
  data jsonb NOT NULL,
  diff_id text UNIQUE REFERENCES diffs(id) ON DELETE SET NULL,
  version_id text REFERENCES diff_versions(id) ON DELETE SET NULL,
  file_count integer NOT NULL,
  additions integer NOT NULL,
  deletions integer NOT NULL,
  PRIMARY KEY(context_id, ordinal),
  UNIQUE(context_id, oid)
);

CREATE TABLE observation_commit_ranges (
  context_id text PRIMARY KEY REFERENCES observations(context_id) ON DELETE CASCADE,
  total integer NOT NULL
);

CREATE TABLE agent_sessions (
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id text NOT NULL,
  harness text NOT NULL,
  session_id text NOT NULL,
  name text NOT NULL,
  named_at bigint NOT NULL,
  PRIMARY KEY(owner_id, source_id, harness, session_id)
);
CREATE INDEX agent_sessions_name ON agent_sessions(owner_id, source_id, name);

CREATE TABLE observation_sessions (
  context_id text NOT NULL REFERENCES observations(context_id) ON DELETE CASCADE,
  owner_id text NOT NULL,
  source_id text NOT NULL,
  harness text NOT NULL,
  session_id text NOT NULL,
  first_observed_at bigint NOT NULL,
  last_observed_at bigint NOT NULL,
  PRIMARY KEY(context_id, source_id, harness, session_id),
  FOREIGN KEY(owner_id, source_id, harness, session_id) REFERENCES agent_sessions(owner_id, source_id, harness, session_id) ON DELETE CASCADE
);
CREATE INDEX observation_sessions_identity ON observation_sessions(owner_id, source_id, harness, session_id, context_id);
