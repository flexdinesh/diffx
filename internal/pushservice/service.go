// Package pushservice owns Web Push for the remote server: when push is
// enabled, the server's VAPID identity, each account's device subscriptions,
// and delivery of notification intent recorded with ingestion admissions.
// Remote composition only. It holds no transport or SQL.
package pushservice

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flexdinesh/diffx/internal/webpush"
)

var (
	// ErrDisabled reports that push needs an HTTPS public URL.
	ErrDisabled = errors.New("Push notifications need an HTTPS public URL")
	// ErrNotFound reports a subscription the account does not have.
	ErrNotFound = errors.New("push subscription not found")
	// ErrInvalid reports a malformed subscription.
	ErrInvalid = errors.New("invalid push subscription")
	// ErrRejected reports a push service that refused or failed a message.
	ErrRejected = errors.New("push service did not accept the notification")
)

// RateLimitedError reports a push service throttling the server.
type RateLimitedError struct{ RetryAfter time.Duration }

func (err *RateLimitedError) Error() string { return "push service rate limit reached" }

// Subscription is one browser's push subscription for an account.
type Subscription struct {
	ID            string
	OwnerID       string
	Endpoint      string
	P256dh        string
	Auth          string
	Label         string
	CreatedAt     int64
	LastSuccessAt *int64
}

// Notification is durable notification intent linked to an ingestion job.
type Notification struct {
	ID      string
	OwnerID string
	Kind    string
	// Content holds the JSON title and body; delivery adds the link.
	Content   []byte
	Attempt   int
	CreatedAt int64
	// Available reports that the owning account exists and is enabled.
	Available bool
	// JobState is the linked ingestion job's state, empty when it is gone;
	// ContextID is its published context once succeeded.
	JobState  string
	ContextID string
}

// Claim is a pending notification leased to one delivery worker. Token fences
// the worker's transitions; a lease that expires is claimable again.
type Claim struct {
	Notification
	Token string
}

// ErrLeaseLost reports a claim another worker has since replaced.
var ErrLeaseLost = errors.New("push notification lease lost")

// Store persists push state atomically per operation.
type Store interface {
	// PushKeys stores generated unless keys exist, and returns the stored keys.
	PushKeys(ctx context.Context, generated webpush.Keys, now int64) (webpush.Keys, error)
	// PutSubscription registers the endpoint for the subscription's owner. The
	// same owner's endpoint keeps its ID and creation time; another account's
	// endpoint moves to this owner as a new device.
	PutSubscription(ctx context.Context, subscription Subscription) (Subscription, error)
	Subscriptions(ctx context.Context, ownerID string) ([]Subscription, error)
	// DeleteSubscription returns ErrNotFound unless the owner has the subscription.
	DeleteSubscription(ctx context.Context, ownerID, id string) error
	RecordSuccess(ctx context.Context, subscriptionID string, now int64) error

	// ClaimNotifications leases up to limit pending notifications available and
	// unleased at now until leaseUntil, oldest first. Concurrent claims never
	// return the same notification.
	ClaimNotifications(ctx context.Context, now, leaseUntil int64, limit int) ([]Claim, error)
	// Undelivered lists the owner's subscriptions without a recorded outcome
	// for the notification.
	Undelivered(ctx context.Context, notificationID string) ([]Subscription, error)

	// Claim transitions return ErrLeaseLost unless the claim's token still
	// holds the pending notification.

	// RecordDelivery records one device's first final outcome; accepted also
	// records the device's last success.
	RecordDelivery(ctx context.Context, claim Claim, subscriptionID string, accepted bool, now int64) error
	// RetryNotification releases the claim until availableAt.
	RetryNotification(ctx context.Context, claim Claim, attempt int, availableAt int64) error
	// FinishNotification ends the claimed notification as sent or dropped.
	FinishNotification(ctx context.Context, claim Claim, state string) error
	// StreamID resolves the owner's context to its review stream; ErrNotFound
	// when it was deleted or expired.
	StreamID(ctx context.Context, ownerID, contextID string, now int64) (string, error)
}

type Options struct {
	// PublicURL enables push when it is https, or http on a loopback host for
	// development. Its origin is the VAPID subject.
	PublicURL string
	Now       func() time.Time
}

type Service struct {
	store     Store
	client    *webpush.Client
	publicKey string
	now       func() time.Time
	// localHTTP admits plain-HTTP loopback endpoints, only for a server whose
	// own public URL is loopback, so production never posts to its own host.
	localHTTP bool
}

// New loads the server's VAPID keys, creating them on first use when push is
// enabled. Keys never rotate automatically: subscriptions are bound to them.
func New(ctx context.Context, store Store, options Options) (*Service, error) {
	service := &Service{store: store, now: options.Now}
	if service.now == nil {
		service.now = time.Now
	}
	subject, ok := enabledOrigin(options.PublicURL)
	if !ok {
		return service, nil
	}
	generated, err := webpush.GenerateKeys()
	if err != nil {
		return nil, err
	}
	keys, err := store.PushKeys(ctx, generated, service.millis())
	if err != nil {
		return nil, fmt.Errorf("load push keys: %w", err)
	}
	service.client, service.publicKey = webpush.New(keys, subject), keys.Public
	if parsed, err := url.Parse(subject); err == nil {
		service.localHTTP = loopback(parsed.Hostname())
	}
	return service, nil
}

func (s *Service) millis() int64 { return s.now().UnixMilli() }

// enabledOrigin returns the public URL's origin when it can serve push.
func enabledOrigin(publicURL string) (string, bool) {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback(parsed.Hostname())) {
		return "", false
	}
	return parsed.Scheme + "://" + parsed.Host, true
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Settings is push availability and the account's devices.
type Settings struct {
	Enabled   bool
	PublicKey string
	Devices   []Subscription
}

func (s *Service) Settings(ctx context.Context, ownerID string) (Settings, error) {
	devices, err := s.store.Subscriptions(ctx, ownerID)
	if err != nil {
		return Settings{}, err
	}
	return Settings{Enabled: s.client != nil, PublicKey: s.publicKey, Devices: devices}, nil
}

// SubscriptionInput is a browser's PushSubscription and a device label.
type SubscriptionInput struct {
	Endpoint string
	P256dh   string
	Auth     string
	Label    string
}

const (
	maxEndpoint = 2048
	maxLabel    = 120
)

// Subscribe registers the browser for the account, replacing an earlier
// registration of the same endpoint, including another account's.
func (s *Service) Subscribe(ctx context.Context, ownerID string, input SubscriptionInput) (Subscription, error) {
	if s.client == nil {
		return Subscription{}, ErrDisabled
	}
	if err := validate(input, s.localHTTP); err != nil {
		return Subscription{}, err
	}
	return s.store.PutSubscription(ctx, Subscription{OwnerID: ownerID, Endpoint: input.Endpoint, P256dh: input.P256dh, Auth: input.Auth, Label: input.Label, CreatedAt: s.millis()})
}

func validate(input SubscriptionInput, localHTTP bool) error {
	if len(input.Endpoint) > maxEndpoint {
		return fmt.Errorf("%w: endpoint too long", ErrInvalid)
	}
	endpoint, err := url.Parse(input.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil {
		return fmt.Errorf("%w: endpoint must be an absolute URL", ErrInvalid)
	}
	// Plain HTTP reaches only loopback push services of a loopback server, for
	// tests and development.
	if endpoint.Scheme != "https" && (endpoint.Scheme != "http" || !localHTTP || !loopback(endpoint.Hostname())) {
		return fmt.Errorf("%w: endpoint must use HTTPS", ErrInvalid)
	}
	key, err := decodeKey(input.P256dh)
	if err != nil || len(key) != 65 {
		return fmt.Errorf("%w: p256dh must be a base64url uncompressed P-256 point", ErrInvalid)
	}
	if _, err := ecdh.P256().NewPublicKey(key); err != nil {
		return fmt.Errorf("%w: p256dh must be a base64url uncompressed P-256 point", ErrInvalid)
	}
	if auth, err := decodeKey(input.Auth); err != nil || len(auth) != 16 {
		return fmt.Errorf("%w: auth must be 16 base64url bytes", ErrInvalid)
	}
	if !utf8.ValidString(input.Label) || utf8.RuneCountInString(input.Label) > maxLabel {
		return fmt.Errorf("%w: label must be at most %d characters", ErrInvalid, maxLabel)
	}
	return nil
}

// decodeKey accepts base64url with or without padding, as browsers produce.
func decodeKey(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
}

// Unsubscribe removes one of the account's devices.
func (s *Service) Unsubscribe(ctx context.Context, ownerID, id string) error {
	return s.store.DeleteSubscription(ctx, ownerID, id)
}

// Test sends a test notification to one of the account's devices and waits
// for the push service. A device the push service reports gone is removed and
// reported as ErrNotFound.
func (s *Service) Test(ctx context.Context, ownerID, id string) error {
	if s.client == nil {
		return ErrDisabled
	}
	devices, err := s.store.Subscriptions(ctx, ownerID)
	if err != nil {
		return err
	}
	var device *Subscription
	for i := range devices {
		if devices[i].ID == id {
			device = &devices[i]
		}
	}
	if device == nil {
		return ErrNotFound
	}
	payload, err := encode(message{Kind: "test", Title: "diffx", Body: "Notifications are on for this device.", URL: "/settings", Tag: "diffx-test", Timestamp: s.millis()})
	if err != nil {
		return err
	}
	result := s.client.Send(ctx, device.target(), payload, webpush.Options{TTL: ttl, Urgency: webpush.UrgencyNormal})
	switch result.Outcome {
	case webpush.Accepted:
		return s.store.RecordSuccess(ctx, device.ID, s.millis())
	case webpush.Gone:
		if err := s.store.DeleteSubscription(ctx, ownerID, device.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return ErrNotFound
	case webpush.Retry:
		if result.Status == 429 {
			return &RateLimitedError{RetryAfter: result.RetryAfter}
		}
	}
	return fmt.Errorf("%w: %w", ErrRejected, result.Err)
}

func (subscription Subscription) target() webpush.Subscription {
	return webpush.Subscription{Endpoint: subscription.Endpoint, P256dh: subscription.P256dh, Auth: subscription.Auth}
}
