package authhttp

import (
	"context"
	"net/http"
	"net/netip"
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
		if client, ok = admission.client(r); !ok {
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

// client reads the single IP address in the header; lists, ports and zones are
// refused rather than guessed at.
func (admission *Admission) client(r *http.Request) (string, bool) {
	values := r.Header.Values(admission.header)
	if len(values) != 1 {
		return "", false
	}
	address, err := netip.ParseAddr(values[0])
	if err != nil || address.Zone() != "" {
		return "", false
	}
	return address.Unmap().String(), true
}
