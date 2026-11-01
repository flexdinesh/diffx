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
		if err := store.queries.PruneAuthAttempts(ctx, pgdb.PruneAuthAttemptsParams{Expired: expired, FetchPrefix: fetchAttemptPrefix}); err != nil {
			return 0, err
		}
	}
	return int(attempts), nil
}

// fetchAttemptPrefix keeps fetch counters apart from authentication
// counters in the shared table; each prunes only its own windows.
const fetchAttemptPrefix = "public-fetch:"

// CountFetchAttempt records one public pull request fetch by client and
// returns the fetches in its current window and when that window started,
// with CountAuthAttempt's fixed-window semantics.
func (store *Store) CountFetchAttempt(ctx context.Context, client string, now time.Time, window time.Duration) (int, time.Time, error) {
	expired := now.Add(-window)
	row, err := store.queries.CountFetchAttempt(ctx, pgdb.CountFetchAttemptParams{Client: fetchAttemptPrefix + client, Now: now, Expired: expired})
	if err != nil {
		return 0, time.Time{}, err
	}
	if row.Attempts == 1 {
		if err := store.queries.PruneFetchAttempts(ctx, pgdb.PruneFetchAttemptsParams{Expired: expired, FetchPrefix: fetchAttemptPrefix}); err != nil {
			return 0, time.Time{}, err
		}
	}
	return int(row.Attempts), row.WindowStart, nil
}
