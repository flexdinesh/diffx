-- +goose Up
-- OAuth authorization for MCP clients. Additive only: the previous release
-- ignores the new tables, so upgrades stay rolling. Only hashes of codes and
-- tokens are stored.

-- Clients registered dynamically (RFC 7591). Clients identified by a metadata
-- document URL are fetched, never stored.
CREATE TABLE oauth_clients (
  client_id text PRIMARY KEY,
  client_name text NOT NULL,
  redirect_uris text[] NOT NULL,
  created_at timestamptz NOT NULL
);

-- Validated authorization requests awaiting the signed-in user's consent.
-- The id is a public handle: consent needs a browser session and CSRF token.
CREATE TABLE oauth_requests (
  id text PRIMARY KEY,
  client_id text NOT NULL,
  client_name text NOT NULL,
  verified boolean NOT NULL,
  redirect_uri text NOT NULL,
  state text NOT NULL,
  code_challenge text NOT NULL,
  scopes text[] NOT NULL,
  resource text NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX oauth_requests_expiry ON oauth_requests(expires_at);

-- One consent per account and client.
CREATE TABLE oauth_grants (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  client_id text NOT NULL,
  client_name text NOT NULL,
  verified boolean NOT NULL,
  scopes text[] NOT NULL,
  created_at timestamptz NOT NULL,
  last_used_at timestamptz,
  UNIQUE (user_id, client_id)
);

-- Single-use codes. A redeemed code is kept until it expires so a replay
-- revokes the token family it started.
CREATE TABLE oauth_codes (
  code_hash text PRIMARY KEY,
  user_id text NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  client_id text NOT NULL,
  client_name text NOT NULL,
  verified boolean NOT NULL,
  redirect_uri text NOT NULL,
  code_challenge text NOT NULL,
  scopes text[] NOT NULL,
  resource text NOT NULL,
  expires_at timestamptz NOT NULL,
  family_id text
);
CREATE INDEX oauth_codes_owner ON oauth_codes(user_id);
CREATE INDEX oauth_codes_expiry ON oauth_codes(expires_at);

-- Access and refresh tokens. A family is the chain one code redemption
-- starts; each refresh rotates it. A rotated refresh token is kept until it
-- expires so presenting it again revokes its family.
CREATE TABLE oauth_tokens (
  secret_hash text PRIMARY KEY,
  grant_id text NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
  family_id text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('access', 'refresh')),
  scopes text[] NOT NULL,
  resource text NOT NULL,
  expires_at timestamptz NOT NULL,
  rotated_at timestamptz
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens(grant_id);
CREATE INDEX oauth_tokens_family ON oauth_tokens(family_id);
CREATE INDEX oauth_tokens_expiry ON oauth_tokens(expires_at);
