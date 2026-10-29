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

-- name: DueNotifications :many
-- The linked job is read through its owner's ingestion record, so a
-- notification never resolves another account's publication.
SELECT n.id, n.owner_id, n.kind, n.payload, n.attempt, n.created_at,
  EXISTS(SELECT 1 FROM remote_accounts a WHERE a.user_id = n.owner_id
    AND NOT EXISTS(SELECT 1 FROM disabled_accounts d WHERE d.user_id = a.user_id))::boolean AS available,
  COALESCE(j.state, '')::text AS job_state, COALESCE(j.context_id, '')::text AS job_context_id
FROM push_notifications n
LEFT JOIN ingestion_jobs j ON j.id = n.job_id
  AND EXISTS(SELECT 1 FROM ingestion_records r WHERE r.sequence = j.sequence AND r.owner_id = n.owner_id)
WHERE n.state = 'pending' AND n.available_at <= @now
ORDER BY n.available_at, n.created_at, n.id
LIMIT @max_count;

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

-- name: RetryNotification :exec
UPDATE push_notifications SET attempt = $2, available_at = $3 WHERE id = $1 AND state = 'pending';

-- name: FinishNotification :exec
UPDATE push_notifications SET state = $2 WHERE id = $1 AND state = 'pending';

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
