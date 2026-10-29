-- +goose Up
-- Web Push. The server's VAPID identity; subscriptions are bound to its public key.
CREATE TABLE push_keys (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  public_key text NOT NULL,
  private_key text NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE TABLE push_subscriptions (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint text NOT NULL UNIQUE,
  p256dh text NOT NULL,
  auth text NOT NULL,
  label text NOT NULL,
  created_at timestamptz NOT NULL,
  last_success_at timestamptz
);
CREATE INDEX push_subscriptions_owner ON push_subscriptions(owner_id, created_at);

-- The outbox commits with the ingestion admission it announces; payload holds
-- the title and body, and delivery adds the link once published.
CREATE TABLE push_notifications (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id text NOT NULL REFERENCES ingestion_jobs(id) ON DELETE CASCADE,
  kind text NOT NULL,
  payload bytea NOT NULL,
  state text NOT NULL CHECK (state IN ('pending', 'sent', 'dropped')),
  attempt integer NOT NULL DEFAULT 0,
  available_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE INDEX push_notifications_due ON push_notifications(available_at) WHERE state = 'pending';
CREATE INDEX push_notifications_job ON push_notifications(job_id);

-- A device's final outcome per notification, so retries and restarts never
-- send again to a device that already accepted.
CREATE TABLE push_deliveries (
  notification_id text NOT NULL REFERENCES push_notifications(id) ON DELETE CASCADE,
  subscription_id text NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
  state text NOT NULL CHECK (state IN ('accepted', 'refused')),
  PRIMARY KEY(notification_id, subscription_id)
);
CREATE INDEX push_deliveries_subscription ON push_deliveries(subscription_id);
