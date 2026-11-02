package authhttp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// Each client IP may make AttemptLimit authentication attempts per window,
// which starts at its first attempt.
const (
	AttemptLimit  = 20
	attemptWindow = time.Minute
	// concurrentAttempts bounds each process's simultaneous credential checks.
	concurrentAttempts = 4
)

// Attempts counts a client's attempts in its current window; every replica
// and the console share the counts.
type Attempts interface {
	CountAuthAttempt(ctx context.Context, client string, now time.Time, window time.Duration) (int, error)
}

// Admission bounds authentication attempts: a few at once per process and,
// with a client IP header, a fixed number per client IP each window. The
// listener's peer is an ingress, not the client, so the IP comes only from the
// header a trusted ingress sets; without that header attempts are not limited
// per client.
type Admission struct {
	attempts Attempts
	header   string
	slots    chan struct{}
}

func NewAdmission(attempts Attempts, header string) *Admission {
	return &Admission{attempts: attempts, header: header, slots: make(chan struct{}, concurrentAttempts)}
}

// Admit answers the refusal itself and returns false, or returns a release
// function for the admitted attempt.
func (admission *Admission) Admit(w http.ResponseWriter, r *http.Request) (func(), bool) {
	var client string
	if admission.header != "" {
		var ok bool
		if client, ok = clientIP(r, admission.header); !ok {
			Problem(w, 400, "Client IP header missing or invalid")
			return nil, false
		}
	}
	select {
	case admission.slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "1")
		Problem(w, 429, "Authentication busy")
		return nil, false
	}
	release := func() { <-admission.slots }
	if admission.header != "" {
		attempts, err := admission.attempts.CountAuthAttempt(r.Context(), client, time.Now(), attemptWindow)
		if err != nil {
			release()
			Problem(w, 500, "Server could not process request")
			return nil, false
		}
		if attempts > AttemptLimit {
			release()
			w.Header().Set("Retry-After", "60")
			Problem(w, 429, "Too many authentication attempts")
			return nil, false
		}
	}
	return release, true
}

// clientIP reads the single IP address in the header; lists, ports and zones
// are refused rather than guessed at.
func clientIP(r *http.Request, header string) (string, bool) {
	values := r.Header.Values(header)
	if len(values) != 1 {
		return "", false
	}
	address, err := netip.ParseAddr(values[0])
	if err != nil || address.Zone() != "" {
		return "", false
	}
	return address.Unmap().String(), true
}

// Public pull request fetches are limited per client IP each window, or
// server-wide without a client IP header. A window starts at its first fetch.
const (
	FetchLimit       = 5
	GlobalFetchLimit = 30
	fetchWindow      = time.Hour
	// globalClient counts every fetch when clients cannot be told apart.
	globalClient = "*"
)

// FetchAttempts counts a client's fetches in its current window and reports
// when it started; every replica shares the counts.
type FetchAttempts interface {
	CountFetchAttempt(ctx context.Context, client string, now time.Time, window time.Duration) (int, time.Time, error)
}

// FetchLimiter bounds fetches that anyone, signed in or not, can trigger.
type FetchLimiter struct {
	attempts FetchAttempts
	header   string
}

func NewFetchLimiter(attempts FetchAttempts, header string) *FetchLimiter {
	return &FetchLimiter{attempts: attempts, header: header}
}

// ErrClientIP reports a missing or invalid client IP header.
var ErrClientIP = errors.New("client IP header missing or invalid")

// RateLimited refuses a fetch until RetryAfter has passed.
type RateLimited struct{ RetryAfter time.Duration }

func (err *RateLimited) Error() string {
	return fmt.Sprintf("fetch limit reached; retry in %s", err.RetryAfter.Round(time.Second))
}

// Admit returns the request's fetch admission: each call counts one fetch
// and refuses with *RateLimited past the limit, or ErrClientIP.
func (limiter *FetchLimiter) Admit(r *http.Request) func(context.Context) error {
	return func(ctx context.Context) error {
		client, limit := globalClient, GlobalFetchLimit
		if limiter.header != "" {
			ip, ok := clientIP(r, limiter.header)
			if !ok {
				return ErrClientIP
			}
			client, limit = ip, FetchLimit
		}
		now := time.Now()
		fetches, start, err := limiter.attempts.CountFetchAttempt(ctx, client, now, fetchWindow)
		if err != nil {
			return err
		}
		if fetches > limit {
			return &RateLimited{RetryAfter: start.Add(fetchWindow).Sub(now)}
		}
		return nil
	}
}

// TooManyRequests answers 429 with Retry-After in whole seconds, at least one.
func TooManyRequests(w http.ResponseWriter, retryAfter time.Duration, detail string) {
	seconds := max(1, int64(math.Ceil(retryAfter.Seconds())))
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	Problem(w, http.StatusTooManyRequests, detail)
}
