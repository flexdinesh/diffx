package pgstore

import (
	"context"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

func (store *Store) AppSettings(ctx context.Context) (identityservice.Settings, error) {
	row, err := store.queries.AppSettings(ctx)
	return identityservice.Settings{Signup: row.SignupOpen, PasswordSignup: row.PasswordSignup,
		PublicPullRequests: row.PublicPullRequests, PublicRetentionDays: int(row.PublicRetentionDays)}, err
}

// PutAppSettings notifies every replica in the transaction that commits the change.
func (store *Store) PutAppSettings(ctx context.Context, settings identityservice.Settings, actorID string, at time.Time) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.PutAppSettings(ctx, pgdb.PutAppSettingsParams{SignupOpen: settings.Signup, PasswordSignup: settings.PasswordSignup,
			PublicPullRequests: settings.PublicPullRequests, PublicRetentionDays: int32(settings.PublicRetentionDays), UpdatedAt: millisecond(at), UpdatedBy: actorID}); err != nil {
			return err
		}
		return notifySettings(ctx, queries)
	})
}

// requireSettings refuses with ErrDisabled unless every setting required is
// on. It holds the settings row until the caller's transaction ends, so an
// account never commits after a change that closes its way in.
func requireSettings(ctx context.Context, queries *pgdb.Queries, required identityservice.Settings) error {
	if required == (identityservice.Settings{}) {
		return nil
	}
	row, err := queries.LockAppSettings(ctx)
	if err != nil {
		return err
	}
	if required.Signup && !row.SignupOpen || required.PasswordSignup && !row.PasswordSignup {
		return identityservice.ErrDisabled
	}
	return nil
}
