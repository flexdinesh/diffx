-- name: InsertPushKeys :exec
INSERT INTO push_keys(public_key, private_key, created_at) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: PushKeys :one
SELECT public_key, private_key FROM push_keys;

-- name: LockEndpoint :exec
-- Serializes registrations of one endpoint, including its first insert.
SELECT pg_advisory_xact_lock(@lock_class::integer, hashtext(@endpoint::text));

-- name: SubscriptionByEndpoint :one
SELECT id, owner_id, endpoint, p256dh, auth, label, created_at, last_success_at
FROM push_subscriptions WHERE endpoint = $1;

-- name: UpdateSubscription :exec
UPDATE push_subscriptions SET p256dh = $2, auth = $3, label = $4 WHERE id = $1;

-- name: DeleteSubscriptionByID :exec
DELETE FROM push_subscriptions WHERE id = $1;

-- name: InsertSubscription :exec
INSERT INTO push_subscriptions(id, owner_id, endpoint, p256dh, auth, label, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: Subscriptions :many
SELECT id, owner_id, endpoint, p256dh, auth, label, created_at, last_success_at
FROM push_subscriptions WHERE owner_id = $1
ORDER BY created_at, id;

-- name: DeleteOwnedSubscription :execrows
DELETE FROM push_subscriptions WHERE owner_id = $1 AND id = $2;

-- name: RecordSubscriptionSuccess :exec
UPDATE push_subscriptions SET last_success_at = @last_success_at::timestamptz WHERE id = @id;

-- name: ClaimNotifications :many
-- SKIP LOCKED lets replicas claim distinct notifications concurrently. The
-- linked job is read through its owner's ingestion record, so a notification
-- never resolves another account's publication.
WITH due AS (
  SELECT d.id FROM push_notifications d
  WHERE d.state = 'pending' AND d.available_at <= @now AND d.lease_until <= @now
  ORDER BY d.available_at, d.created_at, d.id
  LIMIT @max_count
  FOR UPDATE SKIP LOCKED
), claimed AS (
  UPDATE push_notifications n SET lease_token = @lease_token, lease_until = @lease_until
  FROM due WHERE n.id = due.id
  RETURNING n.id, n.owner_id, n.job_id, n.kind, n.payload, n.attempt, n.available_at, n.created_at
)
SELECT c.id, c.owner_id, c.kind, c.payload, c.attempt, c.created_at,
  EXISTS(SELECT 1 FROM remote_accounts a WHERE a.user_id = c.owner_id
    AND NOT EXISTS(SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id))::boolean AS available,
  COALESCE(j.state, '')::text AS job_state, COALESCE(j.context_id, '')::text AS job_context_id
FROM claimed c
LEFT JOIN ingestion_jobs j ON j.id = c.job_id
  AND EXISTS(SELECT 1 FROM ingestion_records r WHERE r.sequence = j.sequence AND r.owner_id = c.owner_id)
ORDER BY c.available_at, c.created_at, c.id;

-- name: Undelivered :many
SELECT s.id, s.owner_id, s.endpoint, s.p256dh, s.auth, s.label, s.created_at, s.last_success_at
FROM push_subscriptions s JOIN push_notifications n ON n.owner_id = s.owner_id
WHERE n.id = $1
  AND NOT EXISTS(SELECT 1 FROM push_deliveries d WHERE d.notification_id = n.id AND d.subscription_id = s.id)
ORDER BY s.created_at, s.id;

-- name: InsertDelivery :exec
-- A device removed or moved to another account while sending has no outcome to keep.
INSERT INTO push_deliveries(notification_id, subscription_id, state)
SELECT @notification_id::text, @subscription_id::text, @state::text
WHERE EXISTS(SELECT 1 FROM push_subscriptions WHERE id = @subscription_id::text)
ON CONFLICT DO NOTHING;

-- name: HoldNotificationLease :one
-- Holds the claim until the caller's transaction commits, so no replica
-- reclaims the notification meanwhile.
SELECT true FROM push_notifications WHERE id = @id AND lease_token = @lease_token AND state = 'pending'
FOR NO KEY UPDATE;

-- name: RetryNotification :execrows
UPDATE push_notifications SET attempt = @attempt, available_at = @available_at, lease_token = '', lease_until = '-infinity'
WHERE id = @id AND lease_token = @lease_token AND state = 'pending';

-- name: FinishNotification :execrows
UPDATE push_notifications SET state = @state, lease_token = '', lease_until = '-infinity'
WHERE id = @id AND lease_token = @lease_token AND state = 'pending';

-- name: ContextStream :one
-- The context's expiry and its current stream head, matched as the catalog
-- matches observations to heads.
SELECT d.expires_at, head.source_id AS head_source_id, head.repository_key AS head_repository_key,
  head.checkout_key AS head_checkout_key, head.branch AS head_branch, head.comparison_policy AS head_comparison_policy
FROM contexts c
LEFT JOIN diffs d ON d.id = c.anchor_diff_id
LEFT JOIN observations o ON o.context_id = c.id
LEFT JOIN observation_stream_heads head ON head.owner_id = o.owner_id AND head.source_id = o.source_id
  AND head.repository_key = o.metadata->>'repositoryKey' AND head.checkout_key = o.metadata->>'checkoutKey'
  AND head.comparison_policy = o.comparison_policy
  AND head.branch = COALESCE(NULLIF(o.metadata->>'branchId', ''), o.metadata->>'branch')
WHERE c.owner_id = $1 AND c.id = $2;
