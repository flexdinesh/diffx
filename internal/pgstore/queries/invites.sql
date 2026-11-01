-- name: InviteEmailLock :exec
-- Serializes invitation and acceptance per email; the two-key form keeps
-- these locks apart from single-key ones.
SELECT pg_advisory_xact_lock(@namespace::int, hashtext(@email::text));

-- name: EmailLinked :one
SELECT EXISTS (
  SELECT 1 FROM google_identities g WHERE g.email = @email::text
  UNION ALL
  SELECT 1 FROM remote_accounts a WHERE a.email = @email::text
);

-- name: RevokePendingInvites :exec
UPDATE account_invites SET revoked_at = @at::timestamptz WHERE email = @email AND used_at IS NULL AND revoked_at IS NULL;

-- name: InsertInvite :exec
INSERT INTO account_invites(id, token_hash, email, created_at, expires_at) VALUES ($1, $2, $3, $4, $5);

-- name: Invites :many
SELECT i.id, i.email, i.created_at, i.expires_at, i.used_at, COALESCE(a.email, '')::text AS used_by, i.revoked_at
FROM account_invites i LEFT JOIN remote_accounts a ON a.user_id = i.used_by
ORDER BY i.created_at DESC, i.id COLLATE "C";

-- name: Invite :one
SELECT i.id, i.email, i.created_at, i.expires_at, i.used_at, COALESCE(a.email, '')::text AS used_by, i.revoked_at
FROM account_invites i LEFT JOIN remote_accounts a ON a.user_id = i.used_by
WHERE i.id = $1;

-- name: PendingInvite :one
SELECT i.id, i.email, i.created_at, i.expires_at, i.used_at, COALESCE(a.email, '')::text AS used_by, i.revoked_at
FROM account_invites i LEFT JOIN remote_accounts a ON a.user_id = i.used_by
WHERE i.token_hash = @token_hash AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > @now;

-- name: RevokeInvite :execrows
UPDATE account_invites SET revoked_at = @at::timestamptz WHERE id = @id AND used_at IS NULL AND revoked_at IS NULL;

-- name: UseInvite :execrows
UPDATE account_invites SET used_at = @now::timestamptz, used_by = @user_id::text
WHERE id = @id AND used_at IS NULL AND revoked_at IS NULL AND expires_at > @now::timestamptz
  AND NOT EXISTS (SELECT 1 FROM google_identities WHERE sub = @sub::text);

-- name: UsePasswordInvite :execrows
UPDATE account_invites SET used_at = @now::timestamptz, used_by = @user_id::text
WHERE id = @id AND email = @email AND used_at IS NULL AND revoked_at IS NULL AND expires_at > @now::timestamptz;

-- name: InsertGoogleIdentity :execrows
INSERT INTO google_identities(sub, user_id, email, updated_at) VALUES ($1, $2, $3, $4)
ON CONFLICT (sub) DO NOTHING;

-- name: GoogleAccount :one
SELECT a.user_id,
  EXISTS (SELECT 1 FROM admin_account m WHERE m.user_id = a.user_id) AS admin,
  EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id) AS disabled
FROM google_identities g JOIN remote_accounts a ON a.user_id = g.user_id
WHERE g.sub = $1;

-- name: RecordGoogleEmail :exec
UPDATE google_identities SET email = @email, updated_at = @now WHERE sub = @sub AND email <> @email;

-- name: PruneGoogleFlows :exec
-- Skips flows being taken, so cleanup never waits on a callback.
DELETE FROM google_auth_states WHERE state_hash IN (
  SELECT e.state_hash FROM google_auth_states e WHERE e.expires_at <= $1 FOR UPDATE SKIP LOCKED
);

-- name: InsertGoogleFlow :exec
INSERT INTO google_auth_states(state_hash, binding_hash, nonce, pkce_verifier, invite_id, setup_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: TakeGoogleFlow :one
DELETE FROM google_auth_states WHERE state_hash = @state_hash AND expires_at > @now::timestamptz
RETURNING binding_hash, nonce, pkce_verifier, invite_id, setup_hash, expires_at;
