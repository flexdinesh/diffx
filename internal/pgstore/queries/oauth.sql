-- name: InsertOAuthClient :exec
INSERT INTO oauth_clients(client_id, client_name, redirect_uris, created_at) VALUES ($1, $2, $3, $4);

-- name: OAuthClient :one
SELECT client_id, client_name, redirect_uris FROM oauth_clients WHERE client_id = $1;

-- name: PruneOAuthRequests :exec
-- Skips rows other transactions hold, so cleanup never waits or deadlocks.
DELETE FROM oauth_requests WHERE id IN (
  SELECT e.id FROM oauth_requests e WHERE e.expires_at <= $1 FOR UPDATE SKIP LOCKED
);

-- name: InsertOAuthRequest :exec
INSERT INTO oauth_requests(id, client_id, client_name, verified, redirect_uri, state, code_challenge, scopes, resource, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: OAuthRequest :one
SELECT id, client_id, client_name, verified, redirect_uri, state, code_challenge, scopes, resource, expires_at
FROM oauth_requests WHERE id = @id AND expires_at > @now;

-- name: ConsumeOAuthRequest :one
DELETE FROM oauth_requests WHERE id = @id AND expires_at > @now
RETURNING id, client_id, client_name, verified, redirect_uri, state, code_challenge, scopes, resource, expires_at;

-- name: PruneOAuthCodes :exec
DELETE FROM oauth_codes WHERE code_hash IN (
  SELECT e.code_hash FROM oauth_codes e WHERE e.expires_at <= $1 FOR UPDATE SKIP LOCKED
);

-- name: InsertOAuthCode :execrows
-- Only an enabled account consents.
INSERT INTO oauth_codes(code_hash, user_id, client_id, client_name, verified, redirect_uri, code_challenge, scopes, resource, expires_at)
SELECT @code_hash::text, a.user_id, @client_id::text, @client_name::text, @verified::boolean, @redirect_uri::text, @code_challenge::text,
  @scopes::text[], @resource::text, @expires_at::timestamptz
FROM remote_accounts a
WHERE a.user_id = @user_id AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id);

-- name: OAuthCodeOwner :one
SELECT user_id FROM oauth_codes WHERE code_hash = $1;

-- name: OAuthRefreshOwner :one
SELECT g.user_id FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.secret_hash = $1 AND t.kind = 'refresh';

-- name: OAuthAccountEnabled :one
SELECT EXISTS (
  SELECT 1 FROM remote_accounts a WHERE a.user_id = $1 AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id)
);

-- name: LockOAuthCode :one
SELECT code_hash, user_id, client_id, client_name, verified, redirect_uri, code_challenge, scopes, resource, expires_at, family_id
FROM oauth_codes WHERE code_hash = $1 FOR UPDATE;

-- name: RedeemOAuthCode :exec
UPDATE oauth_codes SET family_id = @family_id WHERE code_hash = @code_hash;

-- name: UpsertOAuthGrant :one
-- A new consent replaces the client's name, verification and scopes.
INSERT INTO oauth_grants(id, user_id, client_id, client_name, verified, scopes, created_at, last_used_at)
VALUES (@id, @user_id, @client_id, @client_name, @verified, @scopes, @now, @now)
ON CONFLICT (user_id, client_id) DO UPDATE SET client_name = excluded.client_name, verified = excluded.verified, scopes = excluded.scopes,
  last_used_at = GREATEST(oauth_grants.last_used_at, excluded.last_used_at)
RETURNING id;

-- name: PruneOAuthTokens :exec
DELETE FROM oauth_tokens WHERE secret_hash IN (
  SELECT e.secret_hash FROM oauth_tokens e WHERE e.expires_at <= $1 FOR UPDATE SKIP LOCKED
);

-- name: InsertOAuthToken :exec
INSERT INTO oauth_tokens(secret_hash, grant_id, family_id, kind, scopes, resource, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: DeleteOAuthFamily :exec
DELETE FROM oauth_tokens WHERE family_id = $1;

-- name: LockOAuthRefresh :one
SELECT t.secret_hash, t.grant_id, t.family_id, t.scopes, t.resource, t.expires_at, t.rotated_at, g.client_id, g.user_id
FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
WHERE t.secret_hash = $1 AND t.kind = 'refresh'
FOR UPDATE OF t;

-- name: RotateOAuthRefresh :exec
UPDATE oauth_tokens SET rotated_at = @now WHERE secret_hash = @secret_hash;

-- name: TouchOAuthGrant :exec
UPDATE oauth_grants SET last_used_at = GREATEST(last_used_at, @now::timestamptz) WHERE id = @id;

-- name: ResolveOAuthToken :one
-- Records the grant's latest use; last_used_at never moves backwards.
UPDATE oauth_grants g SET last_used_at = GREATEST(g.last_used_at, @now::timestamptz)
FROM oauth_tokens t, remote_accounts a
WHERE t.secret_hash = @secret_hash AND t.kind = 'access' AND t.expires_at > @now::timestamptz AND g.id = t.grant_id
  AND a.user_id = g.user_id AND NOT EXISTS (SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id)
RETURNING a.user_id,
  COALESCE((SELECT gi.email FROM google_identities gi WHERE gi.user_id = a.user_id), a.email)::text AS email,
  g.client_id, g.client_name, g.verified, t.scopes, t.resource, t.expires_at;

-- name: RevokeOAuthToken :exec
DELETE FROM oauth_tokens WHERE family_id IN (
  SELECT t.family_id FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
  WHERE t.secret_hash = @secret_hash AND g.client_id = @client_id
);

-- name: OAuthGrants :many
SELECT id, client_id, client_name, verified, scopes, created_at, last_used_at FROM oauth_grants
WHERE user_id = $1 ORDER BY created_at DESC, id COLLATE "C";

-- name: DeleteOAuthGrant :execrows
DELETE FROM oauth_grants WHERE user_id = $1 AND id = $2;

-- name: DeleteAccountOAuthGrants :exec
DELETE FROM oauth_grants WHERE user_id = $1;

-- name: DeleteAccountOAuthCodes :exec
DELETE FROM oauth_codes WHERE user_id = $1;
