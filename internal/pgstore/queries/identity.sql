-- Accounts read from remote_accounts aliased as a. Ordering is bytewise, as
-- in SQLite, independent of the database collation.

-- name: IdentityLock :exec
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: InsertAccountUser :exec
INSERT INTO users(id, name, created_at) VALUES ($1, $2, $3);

-- name: InsertRemoteAccount :execrows
INSERT INTO remote_accounts(user_id, username, password_hash) VALUES ($1, $2, $3)
ON CONFLICT (username) DO NOTHING;

-- name: PutAdminSetup :execrows
INSERT INTO admin_setup(token_hash, expires_at)
SELECT @token_hash::text, @expires_at::timestamptz WHERE NOT EXISTS (SELECT 1 FROM admin_account)
ON CONFLICT (singleton) DO UPDATE SET token_hash = excluded.token_hash, expires_at = excluded.expires_at;

-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM admin_account);

-- name: ConsumeAdminSetup :execrows
DELETE FROM admin_setup
WHERE token_hash = @token_hash AND expires_at > @now AND NOT EXISTS (SELECT 1 FROM admin_account);

-- name: PendingAdminSetup :one
SELECT EXISTS (
  SELECT 1 FROM admin_setup
  WHERE token_hash = @token_hash AND expires_at > @now AND NOT EXISTS (SELECT 1 FROM admin_account)
);

-- name: InsertAdmin :exec
INSERT INTO admin_account(user_id) VALUES ($1);

-- name: AccountSummaries :many
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  COALESCE(g.email, '')::text AS email,
  EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id) AS disabled,
  (CASE WHEN g.sub IS NULL THEN 'password' ELSE 'google' END)::text AS method,
  u.created_at
FROM remote_accounts a JOIN users u ON u.id = a.user_id
LEFT JOIN google_identities g ON g.user_id = a.user_id
ORDER BY a.username COLLATE "C";

-- name: LockTargetAccount :one
-- Locks the account and its owner row against credential issuance and new owned data.
SELECT EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id) AS disabled
FROM remote_accounts a JOIN users u ON u.id = a.user_id
WHERE a.user_id = $1
FOR UPDATE;

-- name: OtherEnabledAdmin :one
SELECT EXISTS (
  SELECT 1 FROM admin_account m
  WHERE m.user_id <> $1 AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = m.user_id)
);

-- name: GrantAdmin :exec
INSERT INTO admin_account(user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING;

-- name: RevokeAdmin :exec
DELETE FROM admin_account WHERE user_id = $1;

-- name: DisableAccount :exec
INSERT INTO disabled_accounts(user_id, disabled_at) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING;

-- name: EnableAccount :exec
DELETE FROM disabled_accounts WHERE user_id = $1;

-- name: DeleteAccountSessions :exec
DELETE FROM browser_sessions WHERE user_id = $1;

-- name: DeleteAccountTokens :exec
DELETE FROM identity_tokens WHERE user_id = $1;

-- name: DeleteAccountJobs :exec
DELETE FROM ingestion_jobs WHERE sequence IN (SELECT sequence FROM ingestion_records WHERE owner_id = $1);

-- name: DeleteAccountUser :exec
DELETE FROM users WHERE id = $1;

-- name: AccountPassword :one
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method,
  a.password_hash
FROM remote_accounts a
WHERE a.username = $1 AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: AccountPasswordByID :one
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method,
  a.password_hash
FROM remote_accounts a
WHERE a.user_id = $1 AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: ReplacePassword :execrows
UPDATE remote_accounts SET password_hash = @replacement WHERE user_id = @user_id AND password_hash = @expected;

-- name: PruneBrowserSessions :exec
-- Skips sessions another transaction holds, so cleanup never waits or deadlocks.
DELETE FROM browser_sessions WHERE secret_hash IN (
  SELECT e.secret_hash FROM browser_sessions e WHERE e.expires_at <= $1 FOR UPDATE SKIP LOCKED
);

-- name: LockSessionAccount :exec
-- Conflicts with password replacement, disable and delete, so issuance reads
-- the account only after they commit and they see the issued session.
SELECT 1 FROM remote_accounts WHERE user_id = $1 FOR SHARE;

-- name: InsertSession :execrows
INSERT INTO browser_sessions(secret_hash, user_id, csrf_token, expires_at)
SELECT @secret_hash::text, a.user_id, @csrf_token::text, @expires_at::timestamptz
FROM remote_accounts a
WHERE a.user_id = @user_id AND a.password_hash = @expected
  AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: ResolveSession :one
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method,
  s.csrf_token, s.expires_at
FROM browser_sessions s JOIN remote_accounts a ON a.user_id = s.user_id
WHERE s.secret_hash = @secret_hash AND s.expires_at > @now
  AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: DeleteSession :exec
DELETE FROM browser_sessions WHERE secret_hash = $1;

-- name: InsertIdentityToken :execrows
INSERT INTO identity_tokens(id, user_id, name, purpose, secret_hash, created_at, expires_at)
SELECT @id::text, a.user_id, @name::text, @purpose::text, @secret_hash::text, @created_at::timestamptz, @expires_at::timestamptz
FROM remote_accounts a WHERE a.user_id = @user_id;

-- name: IdentityTokens :many
SELECT id, name, purpose, created_at, last_used_at, expires_at FROM identity_tokens
WHERE user_id = $1 ORDER BY created_at DESC, id COLLATE "C";

-- name: DeleteIdentityToken :execrows
DELETE FROM identity_tokens WHERE user_id = $1 AND id = $2;

-- name: ResolveIdentityToken :one
-- Records the latest use; last_used_at never moves backwards.
UPDATE identity_tokens t SET last_used_at = GREATEST(t.last_used_at, @now::timestamptz)
FROM remote_accounts a
WHERE a.user_id = t.user_id AND t.secret_hash = @secret_hash AND t.expires_at > @now::timestamptz
  AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id)
RETURNING a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method,
  t.purpose, t.expires_at;

-- name: IdentityAccounts :many
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method
FROM remote_accounts a
WHERE NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id)
ORDER BY a.username COLLATE "C";

-- name: PutPasswordReset :exec
INSERT INTO password_resets(user_id, token_hash, expires_at) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET token_hash = excluded.token_hash, expires_at = excluded.expires_at;

-- name: PasswordReset :one
SELECT a.user_id, a.username,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  CASE WHEN EXISTS (SELECT 1 FROM google_identities g WHERE g.user_id = a.user_id) THEN 'google' ELSE 'password' END::text AS method,
  a.password_hash
FROM password_resets r JOIN remote_accounts a ON a.user_id = r.user_id
WHERE r.token_hash = @token_hash AND r.expires_at > @now
  AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: ConsumePasswordReset :one
DELETE FROM password_resets WHERE token_hash = @token_hash AND expires_at > @now
RETURNING user_id;

-- name: PrunePasswordResets :exec
DELETE FROM password_resets WHERE expires_at <= $1;
