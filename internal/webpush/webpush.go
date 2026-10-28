// Package webpush is the Web Push wire adapter: VAPID key generation, RFC 8291
// payload encryption with RFC 8292 VAPID authorization, and classification of
// push service responses. It holds no storage or diffx policy.
package webpush

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	upstream "github.com/SherClockHolmes/webpush-go"
)

// Keys is a VAPID key pair: the base64url uncompressed P-256 public point
// browsers use as applicationServerKey, and the base64url private scalar.
type Keys struct {
	Public  string
	Private string
}

// GenerateKeys creates a VAPID key pair.
func GenerateKeys() (Keys, error) {
	private, public, err := upstream.GenerateVAPIDKeys()
	return Keys{Public: public, Private: private}, err
}

// Subscription is a browser PushSubscription: its endpoint and base64url keys.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Urgency values defined by RFC 8030.
const (
	UrgencyVeryLow = "very-low"
	UrgencyLow     = "low"
	UrgencyNormal  = "normal"
	UrgencyHigh    = "high"
)

// Options are the RFC 8030 message headers. Topic replaces an undelivered
// message with the same topic; it must be at most 32 base64url characters.
type Options struct {
	TTL     time.Duration
	Topic   string
	Urgency string
}

// Outcome classifies a push service response.
type Outcome int

const (
	// Accepted: the push service took the message (2xx).
	Accepted Outcome = iota
	// Gone: the subscription expired or was unsubscribed (404, 410).
	Gone
	// Retry: rate limited, a server failure or a network error (429, 5xx).
	Retry
	// Permanent: the push service refused this message (other 4xx, including 413).
	Permanent
)

func (outcome Outcome) String() string {
	switch outcome {
	case Accepted:
		return "accepted"
	case Gone:
		return "gone"
	case Retry:
		return "retry"
	default:
		return "permanent"
	}
}

// Result is a classified send. RetryAfter carries the push service's
// Retry-After when it gave one; Status is zero without a response.
type Result struct {
	Outcome    Outcome
	Status     int
	RetryAfter time.Duration
	Err        error
}

// maxResponseBytes bounds how much of a response body is read for diagnostics.
const maxResponseBytes = 4 << 10

type Client struct {
	keys    Keys
	subject string
	http    *http.Client
	now     func() time.Time
}

type Option func(*Client)

// WithHTTPClient replaces the default client, which times out after 30 seconds.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.http = client }
}

// New signs requests with keys. Subject identifies the application server to
// push services: an https URL or a mailto: address.
func New(keys Keys, subject string, options ...Option) *Client {
	client := &Client{keys: keys, subject: subject, http: &http.Client{Timeout: 30 * time.Second}, now: time.Now}
	for _, option := range options {
		option(client)
	}
	return client
}

// Send encrypts payload for the subscription and posts it to its endpoint.
func (c *Client) Send(ctx context.Context, subscription Subscription, payload []byte, options Options) Result {
	response, err := upstream.SendNotificationWithContext(ctx, payload, &upstream.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     upstream.Keys{P256dh: subscription.P256dh, Auth: subscription.Auth},
	}, &upstream.Options{
		HTTPClient:      c.http,
		Subscriber:      c.subject,
		Topic:           options.Topic,
		TTL:             int(options.TTL / time.Second),
		Urgency:         upstream.Urgency(options.Urgency),
		VAPIDPublicKey:  c.keys.Public,
		VAPIDPrivateKey: c.keys.Private,
	})
	if err != nil {
		// The HTTP client reports every transport failure as *url.Error; they
		// are retried. Malformed keys or an oversized payload never succeed.
		var transport *url.Error
		if errors.As(err, &transport) {
			return Result{Outcome: Retry, Err: err}
		}
		return Result{Outcome: Permanent, Err: err}
	}
	defer response.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	return classify(response, strings.TrimSpace(string(detail)), c.now())
}

func classify(response *http.Response, detail string, now time.Time) Result {
	status := response.StatusCode
	result := Result{Status: status}
	switch {
	case status >= 200 && status < 300:
		result.Outcome = Accepted
		return result
	case status == http.StatusNotFound || status == http.StatusGone:
		result.Outcome = Gone
	case status == http.StatusTooManyRequests || status >= 500:
		result.Outcome = Retry
		result.RetryAfter = retryAfter(response.Header.Get("Retry-After"), now)
	default:
		result.Outcome = Permanent
	}
	if len(detail) > 512 {
		detail = detail[:512]
	}
	result.Err = fmt.Errorf("push service answered HTTP %d: %s", status, detail)
	return result
}

// retryAfter parses delay-seconds or an HTTP date; invalid values are ignored.
func retryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(min(seconds, int64(365*24*time.Hour/time.Second))) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
