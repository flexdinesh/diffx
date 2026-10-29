package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/webpush"
	"github.com/jackc/pgx/v5"
)

// Push adapts the store to the push service's storage contract.
func (store *Store) Push() *PushStore { return &PushStore{store: store} }

type PushStore struct{ store *Store }

// PushKeys reads in a separate statement: its snapshot sees keys a concurrent
// caller committed while the insert waited on the conflict.
func (s *PushStore) PushKeys(ctx context.Context, generated webpush.Keys, now int64) (webpush.Keys, error) {
	if err := s.store.queries.InsertPushKeys(ctx, pgdb.InsertPushKeysParams{PublicKey: generated.Public, PrivateKey: generated.Private, CreatedAt: time.UnixMilli(now)}); err != nil {
		return webpush.Keys{}, err
	}
	keys, err := s.store.queries.PushKeys(ctx)
	if err != nil {
		return webpush.Keys{}, err
	}
	return webpush.Keys{Public: keys.PublicKey, Private: keys.PrivateKey}, nil
}

func subscriptionFromRow(row pgdb.PushSubscription) pushservice.Subscription {
	subscription := pushservice.Subscription{ID: row.ID, OwnerID: row.OwnerID, Endpoint: row.Endpoint, P256dh: row.P256dh, Auth: row.Auth, Label: row.Label, CreatedAt: row.CreatedAt.UnixMilli()}
	if row.LastSuccessAt != nil {
		success := row.LastSuccessAt.UnixMilli()
		subscription.LastSuccessAt = &success
	}
	return subscription
}

func subscriptionsFromRows(rows []pgdb.PushSubscription) []pushservice.Subscription {
	result := make([]pushservice.Subscription, 0, len(rows))
	for _, row := range rows {
		result = append(result, subscriptionFromRow(row))
	}
	return result
}

// endpointLockClass keys endpoint locks apart from other advisory locks; the
// two-key space never overlaps the single-key schema locks.
const endpointLockClass = 7_469_002

func (s *PushStore) PutSubscription(ctx context.Context, subscription pushservice.Subscription) (pushservice.Subscription, error) {
	result := subscription
	err := s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.LockEndpoint(ctx, pgdb.LockEndpointParams{LockClass: endpointLockClass, Endpoint: subscription.Endpoint}); err != nil {
			return err
		}
		row, err := queries.SubscriptionByEndpoint(ctx, subscription.Endpoint)
		switch {
		case err == nil && row.OwnerID == subscription.OwnerID:
			result = subscriptionFromRow(row)
			result.P256dh, result.Auth, result.Label = subscription.P256dh, subscription.Auth, subscription.Label
			return queries.UpdateSubscription(ctx, pgdb.UpdateSubscriptionParams{ID: row.ID, P256dh: result.P256dh, Auth: result.Auth, Label: result.Label})
		case err == nil:
			// The browser now belongs to another account: it starts as a new device.
			if err := queries.DeleteSubscriptionByID(ctx, row.ID); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if result.ID, err = newID(); err != nil {
			return err
		}
		result.LastSuccessAt = nil
		return queries.InsertSubscription(ctx, pgdb.InsertSubscriptionParams{ID: result.ID, OwnerID: result.OwnerID, Endpoint: result.Endpoint,
			P256dh: result.P256dh, Auth: result.Auth, Label: result.Label, CreatedAt: time.UnixMilli(result.CreatedAt)})
	})
	if err != nil {
		return subscription, err
	}
	return result, nil
}

func (s *PushStore) Subscriptions(ctx context.Context, ownerID string) ([]pushservice.Subscription, error) {
	rows, err := s.store.queries.Subscriptions(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return subscriptionsFromRows(rows), nil
}

func (s *PushStore) DeleteSubscription(ctx context.Context, ownerID, id string) error {
	count, err := s.store.queries.DeleteOwnedSubscription(ctx, pgdb.DeleteOwnedSubscriptionParams{OwnerID: ownerID, ID: id})
	if err == nil && count == 0 {
		return pushservice.ErrNotFound
	}
	return err
}

func (s *PushStore) RecordSuccess(ctx context.Context, subscriptionID string, now int64) error {
	return s.store.queries.RecordSubscriptionSuccess(ctx, pgdb.RecordSubscriptionSuccessParams{ID: subscriptionID, LastSuccessAt: time.UnixMilli(now)})
}

func (s *PushStore) DueNotifications(ctx context.Context, now int64, limit int) ([]pushservice.Notification, error) {
	rows, err := s.store.queries.DueNotifications(ctx, pgdb.DueNotificationsParams{Now: time.UnixMilli(now), MaxCount: int32(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]pushservice.Notification, 0, len(rows))
	for _, row := range rows {
		result = append(result, pushservice.Notification{ID: row.ID, OwnerID: row.OwnerID, Kind: row.Kind, Content: row.Payload, Attempt: int(row.Attempt),
			CreatedAt: row.CreatedAt.UnixMilli(), Available: row.Available, JobState: row.JobState, ContextID: row.JobContextID})
	}
	return result, nil
}

func (s *PushStore) Undelivered(ctx context.Context, notificationID string) ([]pushservice.Subscription, error) {
	rows, err := s.store.queries.Undelivered(ctx, notificationID)
	if err != nil {
		return nil, err
	}
	return subscriptionsFromRows(rows), nil
}

func (s *PushStore) RecordDelivery(ctx context.Context, notificationID, subscriptionID string, accepted bool, now int64) error {
	state := "refused"
	if accepted {
		state = "accepted"
	}
	return s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.InsertDelivery(ctx, pgdb.InsertDeliveryParams{NotificationID: notificationID, SubscriptionID: subscriptionID, State: state}); err != nil {
			return err
		}
		if !accepted {
			return nil
		}
		return queries.RecordSubscriptionSuccess(ctx, pgdb.RecordSubscriptionSuccessParams{ID: subscriptionID, LastSuccessAt: time.UnixMilli(now)})
	})
}

func (s *PushStore) RetryNotification(ctx context.Context, id string, attempt int, availableAt int64) error {
	return s.store.queries.RetryNotification(ctx, pgdb.RetryNotificationParams{ID: id, Attempt: int32(attempt), AvailableAt: time.UnixMilli(availableAt)})
}

func (s *PushStore) FinishNotification(ctx context.Context, id, state string) error {
	return s.store.queries.FinishNotification(ctx, pgdb.FinishNotificationParams{ID: id, State: state})
}

func (s *PushStore) StreamID(ctx context.Context, ownerID, contextID string, now int64) (string, error) {
	row, err := s.store.queries.ContextStream(ctx, pgdb.ContextStreamParams{OwnerID: ownerID, ID: contextID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pushservice.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if row.ExpiresAt != nil && row.ExpiresAt.UnixMilli() <= now {
		return "", pushservice.ErrNotFound
	}
	if row.HeadSourceID == nil || row.HeadRepositoryKey == nil || row.HeadCheckoutKey == nil || row.HeadBranch == nil || row.HeadComparisonPolicy == nil {
		return "", pushservice.ErrNotFound
	}
	return ingestion.StreamID(*row.HeadSourceID, *row.HeadRepositoryKey, *row.HeadCheckoutKey, *row.HeadBranch, *row.HeadComparisonPolicy), nil
}

var _ pushservice.Store = (*PushStore)(nil)
