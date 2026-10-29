package pgstore

import (
	"context"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
)

// CountAuthAttempt records one authentication attempt by client and returns the
// attempts in its current window, shared by every replica. A window starts at
// the client's first attempt and ends after window. Starting a window also
// prunes ended ones, so the table holds only recently active clients.
func (store *Store) CountAuthAttempt(ctx context.Context, client string, now time.Time, window time.Duration) (int, error) {
	expired := now.Add(-window)
	attempts, err := store.queries.CountAuthAttempt(ctx, pgdb.CountAuthAttemptParams{Client: client, Now: now, Expired: expired})
	if err != nil {
		return 0, err
	}
	if attempts == 1 {
		if err := store.queries.PruneAuthAttempts(ctx, expired); err != nil {
			return 0, err
		}
	}
	return int(attempts), nil
}
