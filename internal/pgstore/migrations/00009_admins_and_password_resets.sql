-- +goose Up
-- Any number of accounts hold the admin role; setup still creates only the
-- first. Previous releases keep working: they insert user_id alone and test
-- membership with EXISTS.
ALTER TABLE admin_account DROP CONSTRAINT admin_account_pkey;
ALTER TABLE admin_account DROP COLUMN singleton;
ALTER TABLE admin_account DROP CONSTRAINT admin_account_user_id_key;
ALTER TABLE admin_account ADD PRIMARY KEY (user_id);

-- An admin invite makes the accepting account an admin.
ALTER TABLE account_invites ADD COLUMN admin boolean NOT NULL DEFAULT false;

-- One pending reset per password account; the link carries the secret.
CREATE TABLE password_resets (
  user_id text PRIMARY KEY REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL
);
CREATE INDEX password_resets_expiry ON password_resets(expires_at);
