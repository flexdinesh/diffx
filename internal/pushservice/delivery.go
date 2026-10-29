package pushservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/flexdinesh/diffx/internal/webpush"
)

const (
	// ttl is how long a push service keeps an undelivered message.
	ttl = 24 * time.Hour
	// lifetime bounds delivery attempts after the notification was recorded.
	lifetime = 24 * time.Hour
	// jobWait spaces checks of an ingestion job that has not finished.
	jobWait = 2 * time.Second
	// maxBackoff caps retry delays the push service did not dictate.
	maxBackoff = time.Hour
	// idlePoll spaces checks for due notifications.
	idlePoll = time.Second
	// batch bounds the notifications one pass claims, one at a time.
	batch = 32
	// lease bounds how long one worker holds a notification: devices are sent
	// to in turn, each within webpush's 30-second timeout, so it covers ten
	// unresponsive devices. A crashed worker's notification waits out its
	// lease; a worker outliving it loses the claim at its next transition, and
	// the reclaimer sends only to devices without a recorded outcome.
	lease = 5 * time.Minute
)

// message is the payload contract with the web app's service worker.
type message struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	Tag       string `json:"tag"`
	Timestamp int64  `json:"timestamp"`
}

func encode(value message) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	// Browsers parse JSON; keep "<", ">" and "&" readable for clients that do not.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// Topic collapses undelivered notifications of one stream at the push service.
func Topic(streamID string) string {
	digest := sha256.Sum256([]byte(streamID))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:32]
}

// Run delivers due notifications until ctx ends. Storage failures are logged
// and retried on the next poll, so they never stop the server.
func (s *Service) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := s.DeliverDue(ctx); err != nil && ctx.Err() == nil {
			log.Printf("push delivery: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(idlePoll):
		}
	}
	return nil
}

// DeliverDue attempts up to batch due notifications once, claiming each just
// before delivering it so a lease covers one notification's sends. A
// notification whose lease was lost belongs to another worker and is skipped.
func (s *Service) DeliverDue(ctx context.Context) error {
	var failures []error
	for range batch {
		now := s.now()
		claims, err := s.store.ClaimNotifications(ctx, now.UnixMilli(), now.Add(lease).UnixMilli(), 1)
		if err != nil || len(claims) == 0 {
			return errors.Join(append(failures, err)...)
		}
		err = s.deliver(ctx, claims[0])
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && !errors.Is(err, ErrLeaseLost) {
			failures = append(failures, fmt.Errorf("notification %s: %w", claims[0].ID, err))
		}
	}
	return errors.Join(failures...)
}

// deliver sends a notification once its follower's snapshot is published. A
// device records its outcome as soon as the push service answers, so a crash
// or retry never sends to a device that already accepted.
func (s *Service) deliver(ctx context.Context, notification Claim) error {
	now := s.now()
	drop := func() error { return s.store.FinishNotification(ctx, notification, "dropped") }
	switch {
	case s.client == nil, !notification.Available, now.UnixMilli()-notification.CreatedAt >= lifetime.Milliseconds():
		return drop()
	case notification.JobState == "succeeded" && notification.ContextID != "":
	case notification.JobState == "queued" || notification.JobState == "running" || notification.JobState == "retrying":
		return s.store.RetryNotification(ctx, notification, notification.Attempt, now.Add(jobWait).UnixMilli())
	default:
		// The admission failed or is gone: there is nothing to open.
		return drop()
	}
	stream, err := s.store.StreamID(ctx, notification.OwnerID, notification.ContextID, now.UnixMilli())
	if errors.Is(err, ErrNotFound) {
		return drop()
	}
	if err != nil {
		return err
	}
	var content struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(notification.Content, &content); err != nil {
		return errors.Join(err, drop())
	}
	payload, err := encode(message{Kind: notification.Kind, Title: content.Title, Body: content.Body, URL: "/streams/" + stream, Tag: stream, Timestamp: notification.CreatedAt})
	if err != nil {
		return err
	}
	devices, err := s.store.Undelivered(ctx, notification.ID)
	if err != nil {
		return err
	}
	options := webpush.Options{TTL: ttl, Topic: Topic(stream), Urgency: webpush.UrgencyNormal}
	retry, delay := false, time.Duration(0)
	for _, device := range devices {
		result := s.client.Send(ctx, device.target(), payload, options)
		if ctx.Err() != nil {
			// Shutdown leaves the notification pending; an interrupted send
			// is uncertain and repeats once the lease expires.
			return nil
		}
		switch result.Outcome {
		case webpush.Accepted:
			err = s.store.RecordDelivery(ctx, notification, device.ID, true, s.millis())
		case webpush.Gone:
			err = s.store.DeleteSubscription(ctx, notification.OwnerID, device.ID)
			if errors.Is(err, ErrNotFound) {
				err = nil
			}
		case webpush.Permanent:
			log.Printf("push delivery to device %s refused: %v", device.ID, result.Err)
			err = s.store.RecordDelivery(ctx, notification, device.ID, false, s.millis())
		case webpush.Retry:
			retry, delay = true, max(delay, result.RetryAfter)
		}
		if err != nil {
			return err
		}
	}
	if retry {
		delay = max(delay, backoff(notification.Attempt))
		return s.store.RetryNotification(ctx, notification, notification.Attempt+1, s.now().Add(delay).UnixMilli())
	}
	return s.store.FinishNotification(ctx, notification, "sent")
}

// backoff doubles from 30 seconds, capped at an hour.
func backoff(attempt int) time.Duration {
	return min(30*time.Second<<min(attempt, 7), maxBackoff)
}
