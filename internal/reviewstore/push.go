package reviewstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/webpush"
)

// Push notification state arrived in schema 15; every table is additive.
func initializePush(transaction *sql.Tx) error {
	statements := []string{
		// The server's VAPID identity; subscriptions are bound to its public key.
		`CREATE TABLE IF NOT EXISTS push_keys (singleton INTEGER PRIMARY KEY CHECK(singleton=1), public_key TEXT NOT NULL, private_key TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS push_subscriptions (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL, auth TEXT NOT NULL, label TEXT NOT NULL, created_at INTEGER NOT NULL, last_success_at INTEGER)`,
		`CREATE INDEX IF NOT EXISTS push_subscriptions_owner ON push_subscriptions(owner_id, created_at)`,
		// The outbox commits with the ingestion admission it announces; payload
		// holds the title and body, and delivery adds the link once published.
		`CREATE TABLE IF NOT EXISTS push_notifications (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			job_id TEXT NOT NULL REFERENCES ingestion_jobs(id) ON DELETE CASCADE, kind TEXT NOT NULL, payload TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('pending','sent','dropped')), attempt INTEGER NOT NULL DEFAULT 0, available_at INTEGER NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS push_notifications_due ON push_notifications(available_at) WHERE state='pending'`,
		`CREATE INDEX IF NOT EXISTS push_notifications_job ON push_notifications(job_id)`,
		// A device's final outcome per notification, so retries and restarts
		// never send again to a device that already accepted.
		`CREATE TABLE IF NOT EXISTS push_deliveries (notification_id TEXT NOT NULL REFERENCES push_notifications(id) ON DELETE CASCADE,
			subscription_id TEXT NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE, state TEXT NOT NULL CHECK(state IN ('accepted','refused')),
			PRIMARY KEY(notification_id, subscription_id))`,
		`CREATE INDEX IF NOT EXISTS push_deliveries_subscription ON push_deliveries(subscription_id)`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

const pullRequestNotification = "pull_request"

// insertNotification records push intent in the caller's admission transaction.
func insertNotification(transaction *sql.Tx, ownerID, jobID, kind string, notification githubsync.Notification, now int64) error {
	payload, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = transaction.Exec(`INSERT INTO push_notifications(id,owner_id,job_id,kind,payload,state,available_at,created_at) VALUES(?,?,?,?,?,'pending',?,?)`,
		id, ownerID, jobID, kind, string(payload), now, now)
	return err
}

// prunePush removes finished notifications; pending ones end within a day.
func prunePush(transaction *sql.Tx, now int64) error {
	_, err := transaction.Exec(`DELETE FROM push_notifications WHERE state<>'pending' AND created_at<=?`, now-2*24*60*60*1000)
	return err
}

// Push adapts the store to the push service's storage contract.
func (store *Store) Push() *PushStore { return &PushStore{store: store} }

type PushStore struct{ store *Store }

func (s *PushStore) PushKeys(ctx context.Context, generated webpush.Keys, now int64) (webpush.Keys, error) {
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO push_keys(singleton,public_key,private_key,created_at) VALUES(1,?,?,?) ON CONFLICT DO NOTHING`,
		generated.Public, generated.Private, now); err != nil {
		return webpush.Keys{}, err
	}
	var keys webpush.Keys
	err := s.store.db.QueryRowContext(ctx, `SELECT public_key,private_key FROM push_keys WHERE singleton=1`).Scan(&keys.Public, &keys.Private)
	return keys, err
}

const subscriptionColumns = `s.id,s.owner_id,s.endpoint,s.p256dh,s.auth,s.label,s.created_at,s.last_success_at`

func scanSubscription(row scanner) (pushservice.Subscription, error) {
	var subscription pushservice.Subscription
	var success sql.NullInt64
	err := row.Scan(&subscription.ID, &subscription.OwnerID, &subscription.Endpoint, &subscription.P256dh, &subscription.Auth, &subscription.Label, &subscription.CreatedAt, &success)
	if success.Valid {
		subscription.LastSuccessAt = &success.Int64
	}
	return subscription, err
}

func (s *PushStore) subscriptions(ctx context.Context, query string, arguments ...any) ([]pushservice.Subscription, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+subscriptionColumns+` FROM push_subscriptions s `+query+` ORDER BY s.created_at,s.id`, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]pushservice.Subscription, 0)
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, subscription)
	}
	return result, rows.Err()
}

func (s *PushStore) PutSubscription(ctx context.Context, subscription pushservice.Subscription) (pushservice.Subscription, error) {
	transaction, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return subscription, err
	}
	defer transaction.Rollback()
	existing, err := scanSubscription(transaction.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM push_subscriptions s WHERE s.endpoint=?`, subscription.Endpoint))
	switch {
	case err == nil && existing.OwnerID == subscription.OwnerID:
		existing.P256dh, existing.Auth, existing.Label = subscription.P256dh, subscription.Auth, subscription.Label
		if _, err := transaction.ExecContext(ctx, `UPDATE push_subscriptions SET p256dh=?,auth=?,label=? WHERE id=?`, existing.P256dh, existing.Auth, existing.Label, existing.ID); err != nil {
			return subscription, err
		}
		return existing, transaction.Commit()
	case err == nil:
		// The browser now belongs to another account: it starts as a new device.
		if _, err := transaction.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id=?`, existing.ID); err != nil {
			return subscription, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return subscription, err
	}
	if subscription.ID, err = newID(); err != nil {
		return subscription, err
	}
	subscription.LastSuccessAt = nil
	if _, err := transaction.ExecContext(ctx, `INSERT INTO push_subscriptions(id,owner_id,endpoint,p256dh,auth,label,created_at) VALUES(?,?,?,?,?,?,?)`,
		subscription.ID, subscription.OwnerID, subscription.Endpoint, subscription.P256dh, subscription.Auth, subscription.Label, subscription.CreatedAt); err != nil {
		return subscription, err
	}
	return subscription, transaction.Commit()
}

func (s *PushStore) Subscriptions(ctx context.Context, ownerID string) ([]pushservice.Subscription, error) {
	return s.subscriptions(ctx, `WHERE s.owner_id=?`, ownerID)
}

func (s *PushStore) DeleteSubscription(ctx context.Context, ownerID, id string) error {
	result, err := s.store.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE owner_id=? AND id=?`, ownerID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return pushservice.ErrNotFound
	}
	return err
}

func (s *PushStore) RecordSuccess(ctx context.Context, subscriptionID string, now int64) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE push_subscriptions SET last_success_at=? WHERE id=?`, now, subscriptionID)
	return err
}

func (s *PushStore) DueNotifications(ctx context.Context, now int64, limit int) ([]pushservice.Notification, error) {
	// The linked job is read through its owner's ingestion record, so a
	// notification never resolves another account's publication.
	rows, err := s.store.db.QueryContext(ctx, `SELECT n.id,n.owner_id,n.kind,n.payload,n.attempt,n.created_at,
		EXISTS(SELECT 1 FROM remote_accounts a WHERE a.user_id=n.owner_id AND `+enabled+`),
		COALESCE(j.state,''),COALESCE(j.context_id,'')
		FROM push_notifications n
		LEFT JOIN ingestion_jobs j ON j.id=n.job_id AND EXISTS(SELECT 1 FROM ingestion_records r WHERE r.sequence=j.sequence AND r.owner_id=n.owner_id)
		WHERE n.state='pending' AND n.available_at<=? ORDER BY n.available_at,n.created_at,n.id LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]pushservice.Notification, 0)
	for rows.Next() {
		var notification pushservice.Notification
		var payload string
		if err := rows.Scan(&notification.ID, &notification.OwnerID, &notification.Kind, &payload, &notification.Attempt, &notification.CreatedAt,
			&notification.Available, &notification.JobState, &notification.ContextID); err != nil {
			return nil, err
		}
		notification.Content = []byte(payload)
		result = append(result, notification)
	}
	return result, rows.Err()
}

func (s *PushStore) Undelivered(ctx context.Context, notificationID string) ([]pushservice.Subscription, error) {
	return s.subscriptions(ctx, `JOIN push_notifications n ON n.owner_id=s.owner_id WHERE n.id=?
		AND NOT EXISTS(SELECT 1 FROM push_deliveries d WHERE d.notification_id=n.id AND d.subscription_id=s.id)`, notificationID)
}

func (s *PushStore) RecordDelivery(ctx context.Context, notificationID, subscriptionID string, accepted bool, now int64) error {
	transaction, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	state := "refused"
	if accepted {
		state = "accepted"
	}
	// A device removed or moved to another account while sending has no outcome to keep.
	if _, err := transaction.ExecContext(ctx, `INSERT INTO push_deliveries(notification_id,subscription_id,state)
		SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM push_subscriptions WHERE id=?) ON CONFLICT DO NOTHING`, notificationID, subscriptionID, state, subscriptionID); err != nil {
		return err
	}
	if accepted {
		if _, err := transaction.ExecContext(ctx, `UPDATE push_subscriptions SET last_success_at=? WHERE id=?`, now, subscriptionID); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (s *PushStore) RetryNotification(ctx context.Context, id string, attempt int, availableAt int64) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE push_notifications SET attempt=?,available_at=? WHERE id=? AND state='pending'`, attempt, availableAt, id)
	return err
}

func (s *PushStore) FinishNotification(ctx context.Context, id, state string) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE push_notifications SET state=? WHERE id=? AND state='pending'`, state, id)
	return err
}

func (s *PushStore) StreamID(ctx context.Context, ownerID, contextID string, now int64) (string, error) {
	item, err := s.store.Context(ctx, ownerID, contextID, time.UnixMilli(now))
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrExpired) || err == nil && item.StreamID == nil {
		return "", pushservice.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return *item.StreamID, nil
}

var _ pushservice.Store = (*PushStore)(nil)
var _ githubsync.Store = (*GitHubStore)(nil)
