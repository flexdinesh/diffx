-- name: AppSettings :one
SELECT signup_open, password_signup, public_pull_requests, public_retention_days FROM app_settings;

-- name: LockAppSettings :one
-- Holds the settings against a concurrent change until the account commits.
SELECT signup_open, password_signup FROM app_settings FOR SHARE;

-- name: PutAppSettings :exec
UPDATE app_settings SET signup_open = @signup_open, password_signup = @password_signup,
  public_pull_requests = @public_pull_requests, public_retention_days = @public_retention_days,
  updated_at = @updated_at, updated_by = NULLIF(@updated_by::text, '');

-- name: PublicRetentionDays :one
SELECT public_retention_days FROM app_settings;
