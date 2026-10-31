-- +goose Up
-- The app's admission settings, which admins change in the console. Replicas
-- cache them and reload on diffx_settings notifications. Both start on for
-- every database; production admins turn them off after deploying.
CREATE TABLE app_settings (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  signup_open boolean NOT NULL,
  password_signup boolean NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by text REFERENCES remote_accounts(user_id) ON DELETE SET NULL
);
INSERT INTO app_settings(signup_open, password_signup, updated_at) VALUES (true, true, now());

-- Admins come only from setup or promotion, so pending admin invites end.
UPDATE account_invites SET revoked_at = now() WHERE admin AND used_at IS NULL AND revoked_at IS NULL;
ALTER TABLE account_invites DROP COLUMN admin;

-- A Google flow may complete first-admin setup with the setup secret's hash.
ALTER TABLE google_auth_states ADD COLUMN setup_hash text;

-- The previous release reads account_invites.admin, so it must refuse this schema.
UPDATE schema_compatibility SET floor = 10;
