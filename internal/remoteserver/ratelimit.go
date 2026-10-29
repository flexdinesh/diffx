package remoteserver

import (
	"context"
	"net/http"
	"net/netip"
	"time"
)

// Each client IP may make authAttemptLimit authentication attempts per window,
// which starts at its first attempt.
const (
	authAttemptLimit  = 20
	authAttemptWindow = time.Minute
)

// authAttempts counts a client's attempts in its current window; every replica
// shares the counts.
type authAttempts interface {
	CountAuthAttempt(ctx context.Context, client string, now time.Time, window time.Duration) (int, error)
}

// authLimiter bounds authentication attempts per client IP. The listener's peer
// is an ingress, not the client, so the IP comes only from the header a trusted
// ingress sets; without that header the server does not limit attempts.
type authLimiter struct {
	attempts authAttempts
	header   string
}

// newLimiter returns nil, disabling limiting, when no header names the client.
func newLimiter(attempts authAttempts, header string) *authLimiter {
	if header == "" {
		return nil
	}
	return &authLimiter{attempts: attempts, header: header}
}

// client reads the single IP address in the header; lists, ports and zones are
// refused rather than guessed at.
func (limiter *authLimiter) client(r *http.Request) (string, bool) {
	values := r.Header.Values(limiter.header)
	if len(values) != 1 {
		return "", false
	}
	address, err := netip.ParseAddr(values[0])
	if err != nil || address.Zone() != "" {
		return "", false
	}
	return address.Unmap().String(), true
}

func (limiter *authLimiter) allow(ctx context.Context, client string) (bool, error) {
	attempts, err := limiter.attempts.CountAuthAttempt(ctx, client, time.Now(), authAttemptWindow)
	return attempts <= authAttemptLimit, err
}
