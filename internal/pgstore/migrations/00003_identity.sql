-- +goose Up
-- Accounts and credentials. Only hashes of secrets are stored.

CREATE TABLE remote_accounts (
  user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  username text NOT NULL UNIQUE,
  password_hash text NOT NULL
);

CREATE TABLE browser_sessions (
  secret_hash text PRIMARY KEY,
  user_id text NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  csrf_token text NOT NULL,
  expires_at timestamptz NOT NULL
);
CREATE INDEX browser_sessions_owner ON browser_sessions(user_id);
CREATE INDEX browser_sessions_expiry ON browser_sessions(expires_at);

CREATE TABLE identity_tokens (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  name text NOT NULL,
  purpose text NOT NULL CHECK (purpose IN ('collector', 'api')),
  secret_hash text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL,
  last_used_at timestamptz,
  expires_at timestamptz NOT NULL
);
CREATE INDEX identity_tokens_owner ON identity_tokens(user_id);

-- The singleton keys allow at most one admin and one pending setup secret.
CREATE TABLE admin_account (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  user_id text NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE
);

CREATE TABLE admin_setup (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  token_hash text NOT NULL,
  expires_at timestamptz NOT NULL
);

CREATE TABLE disabled_accounts (
  user_id text PRIMARY KEY REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  disabled_at timestamptz NOT NULL
);

-- Google's subject identifies the person; email is the latest one Google reported.
CREATE TABLE google_identities (
  sub text PRIMARY KEY,
  user_id text NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  email text NOT NULL,
  updated_at timestamptz NOT NULL
);
CREATE INDEX google_identities_email ON google_identities(email);

CREATE TABLE account_invites (
  id text PRIMARY KEY,
  token_hash text NOT NULL UNIQUE,
  email text NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  used_by text REFERENCES remote_accounts(user_id) ON DELETE SET NULL,
  revoked_at timestamptz
);
CREATE INDEX account_invites_email ON account_invites(email);

CREATE TABLE google_auth_states (
  state_hash text PRIMARY KEY,
  binding_hash text NOT NULL,
  nonce text NOT NULL,
  pkce_verifier text NOT NULL,
  invite_id text REFERENCES account_invites(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);
