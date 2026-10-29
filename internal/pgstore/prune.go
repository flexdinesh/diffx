package pgstore

import (
	"context"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

const (
	finishedNotificationRetention = 2 * 24 * time.Hour
	deliveryRetention             = 7 * 24 * time.Hour
)

// PruneExpired removes expired observations with every scope and commit diff,
// ends streams without observations, and drops finished integration records.
func (store *Store) PruneExpired(now time.Time) error {
	ctx := context.Background()
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.LockExpiredContexts(ctx, now); err != nil {
			return err
		}
		if err := queries.PruneExpiredObservationDiffs(ctx, &now); err != nil {
			return err
		}
		if err := queries.PruneExpiredDiffs(ctx, &now); err != nil {
			return err
		}
		// Ingestion holds the stream lock while adding an observation, so a
		// stream is ended only when no committed or pending observation remains.
		if err := queries.LockEndingStreams(ctx); err != nil {
			return err
		}
		if err := queries.EndStreams(ctx); err != nil {
			return err
		}
		if err := queries.PruneGitHubStates(ctx, now); err != nil {
			return err
		}
		if err := queries.PruneGitHubDeliveries(ctx, now.Add(-deliveryRetention)); err != nil {
			return err
		}
		return queries.PruneFinishedNotifications(ctx, now.Add(-finishedNotificationRetention))
	})
}
