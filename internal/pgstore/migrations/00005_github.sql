-- +goose Up
-- GitHub integration. Tokens are sealed by githubsync before storage.

CREATE TABLE github_oauth_states (
  state_hash text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);

CREATE TABLE github_connections (
  owner_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  user_id bigint NOT NULL,
  login text NOT NULL,
  access_token bytea NOT NULL,
  access_expires_at timestamptz NOT NULL,
  refresh_token bytea NOT NULL,
  refresh_expires_at timestamptz NOT NULL,
  status text NOT NULL,
  connected_at timestamptz NOT NULL
);
CREATE INDEX github_connections_user ON github_connections(user_id);

CREATE TABLE github_follows (
  owner_id text NOT NULL REFERENCES github_connections(owner_id) ON DELETE CASCADE,
  repository_id bigint NOT NULL,
  installation_id bigint NOT NULL,
  full_name text NOT NULL,
  status text NOT NULL,
  verified_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY(owner_id, repository_id)
);
CREATE INDEX github_follows_repository ON github_follows(repository_id, status);
CREATE INDEX github_follows_installation ON github_follows(installation_id);

CREATE TABLE github_deliveries (
  id text PRIMARY KEY,
  event text NOT NULL,
  received_at timestamptz NOT NULL
);
CREATE INDEX github_deliveries_received ON github_deliveries(received_at);

-- One row per pull request coalesces refresh intent: webhooks and
-- reconciliation advance generation; the refresh task records the last
-- generation it processed and the state it observed. A lease fences the
-- fetcher by token; leased holds what it will publish.
CREATE TABLE github_pull_requests (
  repository_id bigint NOT NULL,
  number integer NOT NULL,
  installation_id bigint NOT NULL,
  full_name text NOT NULL,
  generation bigint NOT NULL,
  refreshed_generation bigint NOT NULL DEFAULT 0,
  state text NOT NULL DEFAULT 'open',
  retention_run text NOT NULL DEFAULT '',
  requested_at timestamptz NOT NULL,
  lease_token text NOT NULL DEFAULT '',
  lease_until timestamptz NOT NULL DEFAULT '-infinity',
  leased_generation bigint NOT NULL DEFAULT 0,
  leased_run text NOT NULL DEFAULT '',
  leased bytea NOT NULL DEFAULT '',
  attempt integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL DEFAULT '-infinity',
  detail text NOT NULL DEFAULT '',
  PRIMARY KEY(repository_id, number)
);
CREATE INDEX github_pull_requests_pending ON github_pull_requests(requested_at) WHERE generation > refreshed_generation;

-- The last captured head per pull request decides whether a snapshot is new
-- work; opened records that a webhook announced the pull request.
CREATE TABLE github_pr_heads (
  repository_id bigint NOT NULL,
  number integer NOT NULL,
  head_sha text NOT NULL DEFAULT '',
  opened boolean NOT NULL DEFAULT false,
  PRIMARY KEY(repository_id, number),
  FOREIGN KEY(repository_id, number) REFERENCES github_pull_requests(repository_id, number) ON DELETE CASCADE
);

-- A mute ends with its follow, which ends with its connection.
CREATE TABLE github_follow_mutes (
  owner_id text NOT NULL,
  repository_id bigint NOT NULL,
  PRIMARY KEY(owner_id, repository_id),
  FOREIGN KEY(owner_id, repository_id) REFERENCES github_follows(owner_id, repository_id) ON DELETE CASCADE
);
