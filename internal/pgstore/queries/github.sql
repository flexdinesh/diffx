-- name: PutGitHubOAuthState :exec
INSERT INTO github_oauth_states(state_hash, owner_id, expires_at) VALUES ($1, $2, $3);

-- name: TakeGitHubOAuthState :one
DELETE FROM github_oauth_states WHERE state_hash = @state_hash AND expires_at > @now
RETURNING owner_id;

-- name: PutGitHubConnection :exec
INSERT INTO github_connections(owner_id, user_id, login, access_token, access_expires_at, refresh_token, refresh_expires_at, status, connected_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (owner_id) DO UPDATE SET user_id = excluded.user_id, login = excluded.login, access_token = excluded.access_token,
  access_expires_at = excluded.access_expires_at, refresh_token = excluded.refresh_token, refresh_expires_at = excluded.refresh_expires_at,
  status = excluded.status, connected_at = excluded.connected_at;

-- name: GitHubConnection :one
SELECT * FROM github_connections WHERE owner_id = $1;

-- name: GitHubConnections :many
SELECT * FROM github_connections ORDER BY owner_id;

-- name: DeleteGitHubConnection :exec
-- Follows and mutes cascade with their connection.
DELETE FROM github_connections WHERE owner_id = $1;

-- name: RevokeGitHubUserConnections :exec
UPDATE github_connections SET status = 'revoked' WHERE user_id = $1;

-- name: LoseGitHubUserFollows :exec
UPDATE github_follows SET status = 'lost'
WHERE owner_id IN (SELECT owner_id FROM github_connections WHERE user_id = $1);

-- name: PutGitHubFollow :exec
-- Mutes are separate rows, so a put leaves them unchanged.
INSERT INTO github_follows(owner_id, repository_id, installation_id, full_name, status, verified_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (owner_id, repository_id) DO UPDATE SET installation_id = excluded.installation_id, full_name = excluded.full_name,
  status = excluded.status, verified_at = excluded.verified_at;

-- name: DeleteGitHubFollow :exec
DELETE FROM github_follows WHERE owner_id = $1 AND repository_id = $2;

-- name: GitHubFollows :many
SELECT f.owner_id, f.repository_id, f.installation_id, f.full_name, f.status, f.verified_at, f.created_at, (m.owner_id IS NOT NULL)::boolean AS muted
FROM github_follows f LEFT JOIN github_follow_mutes m ON m.owner_id = f.owner_id AND m.repository_id = f.repository_id
WHERE f.owner_id = $1 ORDER BY f.full_name, f.owner_id;

-- name: GitHubRepositoryFollows :many
SELECT f.owner_id, f.repository_id, f.installation_id, f.full_name, f.status, f.verified_at, f.created_at, (m.owner_id IS NOT NULL)::boolean AS muted
FROM github_follows f LEFT JOIN github_follow_mutes m ON m.owner_id = f.owner_id AND m.repository_id = f.repository_id
WHERE f.repository_id = $1 ORDER BY f.full_name, f.owner_id;

-- name: LockGitHubFollow :one
-- Holds the follow against deletion until the mute change commits.
SELECT true FROM github_follows WHERE owner_id = $1 AND repository_id = $2 FOR KEY SHARE;

-- name: MuteGitHubFollow :exec
INSERT INTO github_follow_mutes(owner_id, repository_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: UnmuteGitHubFollow :exec
DELETE FROM github_follow_mutes WHERE owner_id = $1 AND repository_id = $2;

-- name: LoseGitHubInstallation :exec
UPDATE github_follows SET status = 'lost' WHERE installation_id = $1;

-- name: LoseGitHubInstallationRepositories :exec
UPDATE github_follows SET status = 'lost' WHERE installation_id = @installation_id AND repository_id = ANY(@repository_ids::bigint[]);

-- name: SeenGitHubDelivery :one
SELECT EXISTS(SELECT 1 FROM github_deliveries WHERE id = $1);

-- name: RecordGitHubDelivery :exec
INSERT INTO github_deliveries(id, event, received_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: RequestGitHubRefresh :exec
INSERT INTO github_pull_requests(repository_id, number, installation_id, full_name, generation, retention_run, requested_at)
VALUES (@repository_id, @number, @installation_id, @full_name, 1, @retention_run, @requested_at)
ON CONFLICT (repository_id, number) DO UPDATE SET installation_id = excluded.installation_id, full_name = excluded.full_name,
  generation = github_pull_requests.generation + 1,
  retention_run = CASE WHEN excluded.retention_run <> '' THEN excluded.retention_run ELSE github_pull_requests.retention_run END,
  requested_at = excluded.requested_at;

-- name: MarkGitHubPullRequestOpened :exec
INSERT INTO github_pr_heads(repository_id, number, opened) VALUES ($1, $2, true)
ON CONFLICT (repository_id, number) DO UPDATE SET opened = true;

-- name: GitHubPullRequestObservation :one
-- The state the last refresh observed, and the head and draft flag of the
-- last captured snapshot ('' and false before the first).
SELECT p.state, COALESCE(h.head_sha, '')::text AS head_sha, COALESCE(h.draft, false)::boolean AS draft,
  COALESCE(h.opened, false)::boolean AS opened
FROM github_pull_requests p LEFT JOIN github_pr_heads h ON h.repository_id = p.repository_id AND h.number = p.number
WHERE p.repository_id = $1 AND p.number = $2;

-- name: GitHubKnownOpen :many
SELECT number FROM github_pull_requests WHERE repository_id = $1 AND state = 'open' ORDER BY number;

-- name: ClaimGitHubRefresh :one
-- SKIP LOCKED lets replicas claim distinct pull requests concurrently; a row
-- another replica claimed meanwhile fails the recheck and is skipped.
UPDATE github_pull_requests p SET lease_token = @token, lease_until = @lease_until, leased_generation = p.generation, leased_run = p.retention_run, leased = ''
FROM (
  SELECT pending.repository_id, pending.number FROM github_pull_requests pending
  WHERE pending.generation > pending.refreshed_generation AND pending.lease_until <= @now AND pending.available_at <= @now
  ORDER BY pending.requested_at, pending.repository_id, pending.number LIMIT 1 FOR UPDATE SKIP LOCKED
) claimed
WHERE p.repository_id = claimed.repository_id AND p.number = claimed.number
RETURNING p.repository_id, p.installation_id, p.full_name, p.number, p.leased_generation, p.leased_run, p.attempt,
  (p.public_generation > p.refreshed_generation)::boolean AS public;

-- name: RecordGitHubLease :execrows
UPDATE github_pull_requests SET leased = @leased
WHERE repository_id = @repository_id AND number = @number AND lease_token = @token AND lease_until > @now;

-- name: GitHubLeased :one
SELECT repository_id, installation_id, full_name, number, leased_generation, leased_run, attempt, leased,
  (public_generation > refreshed_generation)::boolean AS public FROM github_pull_requests
WHERE repository_id = @repository_id AND number = @number AND lease_token = @token AND lease_until > @now AND leased <> '';

-- name: ReleaseGitHubRefresh :execrows
UPDATE github_pull_requests SET lease_token = '', lease_until = '-infinity', leased = '', attempt = attempt + 1, available_at = @retry_at, detail = @detail
WHERE repository_id = @repository_id AND number = @number AND lease_token = @token;

-- name: CompleteGitHubRefresh :execrows
UPDATE github_pull_requests SET refreshed_generation = GREATEST(refreshed_generation, leased_generation), state = @state,
  retention_run = CASE WHEN retention_run = leased_run THEN '' ELSE retention_run END,
  lease_token = '', lease_until = '-infinity', leased = '', attempt = 0, available_at = '-infinity', detail = ''
WHERE repository_id = @repository_id AND number = @number AND lease_token = @token AND lease_until > @now;

-- name: RecordGitHubPullRequestHead :exec
INSERT INTO github_pr_heads(repository_id, number, head_sha, draft) VALUES ($1, $2, $3, $4)
ON CONFLICT (repository_id, number) DO UPDATE SET head_sha = excluded.head_sha, draft = excluded.draft;

-- name: RequestPublicGitHubRefresh :exec
-- Public demand advances the generation like any refresh but never replaces
-- a follower's installation; a new row reads through the public installation.
INSERT INTO github_pull_requests(repository_id, number, installation_id, full_name, generation, retention_run, requested_at,
  public_generation, public_pull, public_checked_at)
VALUES (@repository_id, @number, @installation_id, @full_name, 1, @retention_run, @requested_at, 1, @public_pull, @requested_at)
ON CONFLICT (repository_id, number) DO UPDATE SET full_name = excluded.full_name,
  generation = github_pull_requests.generation + 1, retention_run = excluded.retention_run, requested_at = excluded.requested_at,
  public_generation = github_pull_requests.generation + 1, public_pull = excluded.public_pull, public_checked_at = excluded.public_checked_at;

-- name: RecordPublicGitHubCheck :exec
UPDATE github_pull_requests SET public_pull = @public_pull, public_checked_at = @now
WHERE repository_id = @repository_id AND number = @number;

-- name: RetryPublicGitHubRefresh :exec
-- Only a pending, unleased refresh waits for backoff.
UPDATE github_pull_requests SET available_at = @now, detail = ''
WHERE repository_id = @repository_id AND number = @number AND generation > refreshed_generation AND lease_until <= @now;

-- name: RecordPublicGitHubSnapshot :exec
UPDATE github_pull_requests SET public_job_id = @job_id, public_fetched_at = @now, public_head = @head
WHERE repository_id = @repository_id AND number = @number;

-- name: PublicGitHubPullRequest :one
-- pending is a pending refresh that covers public demand; context_id is set
-- only for a succeeded public job whose snapshot is unexpired.
SELECT p.repository_id, p.installation_id, p.full_name, p.number, p.public_generation,
  (p.public_generation > p.refreshed_generation)::boolean AS pending, (p.lease_until > @now::timestamptz)::boolean AS leased,
  p.attempt, p.detail, p.public_pull, p.public_head, p.public_checked_at, p.public_fetched_at,
  COALESCE(j.state, '')::text AS job_state,
  COALESCE((SELECT o.context_id FROM observations o JOIN contexts c ON c.id = o.context_id JOIN diffs d ON d.id = c.anchor_diff_id
    WHERE j.state = 'succeeded' AND o.owner_id = @owner_id::text AND o.context_id = j.context_id AND d.expires_at > @now::timestamptz), '')::text AS context_id
FROM github_pull_requests p LEFT JOIN ingestion_jobs j ON j.id = p.public_job_id
WHERE p.repository_id = @repository_id AND p.number = @number;

-- name: PublicGitHubPullRequestNamed :one
-- Names are case-insensitive; after renames the most recently requested row wins.
SELECT p.repository_id, p.installation_id, p.full_name, p.number, p.public_generation,
  (p.public_generation > p.refreshed_generation)::boolean AS pending, (p.lease_until > @now::timestamptz)::boolean AS leased,
  p.attempt, p.detail, p.public_pull, p.public_head, p.public_checked_at, p.public_fetched_at,
  COALESCE(j.state, '')::text AS job_state,
  COALESCE((SELECT o.context_id FROM observations o JOIN contexts c ON c.id = o.context_id JOIN diffs d ON d.id = c.anchor_diff_id
    WHERE j.state = 'succeeded' AND o.owner_id = @owner_id::text AND o.context_id = j.context_id AND d.expires_at > @now::timestamptz), '')::text AS context_id
FROM github_pull_requests p LEFT JOIN ingestion_jobs j ON j.id = p.public_job_id
WHERE lower(p.full_name) = lower(@full_name::text) AND p.number = @number
ORDER BY p.requested_at DESC LIMIT 1;

-- name: TryGitHubReconcileLock :one
-- Session-level: held across reconciliation's GitHub calls without a transaction.
SELECT pg_try_advisory_lock(@key::bigint);

-- name: GitHubReconcileUnlock :one
SELECT pg_advisory_unlock(@key::bigint);

-- name: PutGitHubPullRequestHistory :exec
-- GitHub's update time orders reads: an older read never replaces a newer row.
INSERT INTO github_pull_request_history(repository_id, number, full_name, title, html_url, author_id, author_login, draft, state,
  created_at, merged_at, closed_at, updated_at)
VALUES (@repository_id, @number, @full_name, @title, @html_url, @author_id, @author_login, @draft, @state,
  @created_at, sqlc.narg('merged_at'), sqlc.narg('closed_at'), @updated_at)
ON CONFLICT (repository_id, number) DO UPDATE SET full_name = excluded.full_name, title = excluded.title, html_url = excluded.html_url,
  author_id = excluded.author_id, author_login = excluded.author_login, draft = excluded.draft, state = excluded.state,
  created_at = excluded.created_at, merged_at = excluded.merged_at, closed_at = excluded.closed_at, updated_at = excluded.updated_at
WHERE github_pull_request_history.updated_at <= excluded.updated_at;

-- name: GitHubPullRequestHistory :many
-- The account's GitHub user's pull requests in its actively followed
-- repositories that opened or merged within [since, until), newest event first.
SELECT h.repository_id, h.number, h.full_name, h.title, h.html_url, h.draft, h.state, h.created_at, h.merged_at, h.closed_at
FROM github_pull_request_history h
JOIN github_connections c ON c.owner_id = @owner_id AND c.user_id = h.author_id
JOIN github_follows f ON f.owner_id = c.owner_id AND f.repository_id = h.repository_id AND f.status = 'active'
WHERE (h.created_at >= @since AND h.created_at < @until) OR (h.merged_at >= @since AND h.merged_at < @until)
ORDER BY GREATEST(CASE WHEN h.created_at < @until THEN h.created_at END, CASE WHEN h.merged_at < @until THEN h.merged_at END) DESC,
  h.repository_id, h.number
LIMIT @row_limit;

-- name: GitHubNotificationPreferences :one
SELECT * FROM github_notification_preferences WHERE owner_id = $1;

-- name: PutGitHubNotificationPreferences :exec
INSERT INTO github_notification_preferences(owner_id, opened_by_me, opened_by_others, new_commits_by_me, new_commits_by_others,
  merged_by_me, merged_by_others, closed_by_me, closed_by_others, skip_drafts, only_authors, never_authors, base_branches,
  muted_title_terms, updated_at)
VALUES (@owner_id, @opened_by_me, @opened_by_others, @new_commits_by_me, @new_commits_by_others,
  @merged_by_me, @merged_by_others, @closed_by_me, @closed_by_others, @skip_drafts, @only_authors, @never_authors, @base_branches,
  @muted_title_terms, @updated_at)
ON CONFLICT (owner_id) DO UPDATE SET opened_by_me = excluded.opened_by_me, opened_by_others = excluded.opened_by_others,
  new_commits_by_me = excluded.new_commits_by_me, new_commits_by_others = excluded.new_commits_by_others,
  merged_by_me = excluded.merged_by_me, merged_by_others = excluded.merged_by_others,
  closed_by_me = excluded.closed_by_me, closed_by_others = excluded.closed_by_others, skip_drafts = excluded.skip_drafts,
  only_authors = excluded.only_authors, never_authors = excluded.never_authors, base_branches = excluded.base_branches,
  muted_title_terms = excluded.muted_title_terms, updated_at = excluded.updated_at;

-- name: GitHubNotificationRecipients :many
-- Each listed connected account's GitHub user and saved preferences. Saved
-- is false for accounts that never saved any; their other columns are blank.
SELECT c.owner_id, c.user_id, (p.owner_id IS NOT NULL)::boolean AS saved,
  COALESCE(p.opened_by_me, false)::boolean AS opened_by_me, COALESCE(p.opened_by_others, false)::boolean AS opened_by_others,
  COALESCE(p.new_commits_by_me, false)::boolean AS new_commits_by_me, COALESCE(p.new_commits_by_others, false)::boolean AS new_commits_by_others,
  COALESCE(p.merged_by_me, false)::boolean AS merged_by_me, COALESCE(p.merged_by_others, false)::boolean AS merged_by_others,
  COALESCE(p.closed_by_me, false)::boolean AS closed_by_me, COALESCE(p.closed_by_others, false)::boolean AS closed_by_others,
  COALESCE(p.skip_drafts, false)::boolean AS skip_drafts,
  COALESCE(p.only_authors, '{}')::text[] AS only_authors, COALESCE(p.never_authors, '{}')::text[] AS never_authors,
  COALESCE(p.base_branches, '{}')::text[] AS base_branches, COALESCE(p.muted_title_terms, '{}')::text[] AS muted_title_terms
FROM github_connections c LEFT JOIN github_notification_preferences p ON p.owner_id = c.owner_id
WHERE c.owner_id = ANY(@owner_ids::text[]);

-- name: HistoryRetentionDays :one
SELECT history_retention_days FROM app_settings;
