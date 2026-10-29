package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

// CreateInvite replaces the email's pending invites, refusing an email an
// account already uses, in one transaction. The email lock orders it with
// other invitations and acceptances of the email.
func (store *Store) CreateInvite(ctx context.Context, invite identityservice.Invite, tokenHash string) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.InviteEmailLock(ctx, pgdb.InviteEmailLockParams{Namespace: inviteEmailLocks, Email: invite.Email}); err != nil {
			return err
		}
		linked, err := queries.EmailLinked(ctx, invite.Email)
		if err != nil {
			return err
		}
		if linked {
			return identityservice.ErrConflict
		}
		if err := queries.RevokePendingInvites(ctx, pgdb.RevokePendingInvitesParams{At: millisecond(invite.CreatedAt), Email: invite.Email}); err != nil {
			return err
		}
		return queries.InsertInvite(ctx, pgdb.InsertInviteParams{ID: invite.ID, TokenHash: tokenHash, Email: invite.Email, CreatedAt: millisecond(invite.CreatedAt), ExpiresAt: millisecond(invite.ExpiresAt)})
	})
}

func optionalMillisecond(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	value := millisecond(*at)
	return &value
}

func inviteFromRow(row pgdb.InviteRow) identityservice.Invite {
	return identityservice.Invite{
		ID: row.ID, Email: row.Email, CreatedAt: millisecond(row.CreatedAt), ExpiresAt: millisecond(row.ExpiresAt),
		UsedAt: optionalMillisecond(row.UsedAt), UsedBy: row.UsedBy, RevokedAt: optionalMillisecond(row.RevokedAt),
	}
}

func (store *Store) Invites(ctx context.Context) ([]identityservice.Invite, error) {
	rows, err := store.queries.Invites(ctx)
	if err != nil {
		return nil, err
	}
	invites := make([]identityservice.Invite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, inviteFromRow(pgdb.InviteRow(row)))
	}
	return invites, nil
}

func (store *Store) Invite(ctx context.Context, id string) (identityservice.Invite, error) {
	row, err := store.queries.Invite(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.Invite{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.Invite{}, err
	}
	return inviteFromRow(row), nil
}

func (store *Store) PendingInvite(ctx context.Context, tokenHash string, now time.Time) (identityservice.Invite, error) {
	row, err := store.queries.PendingInvite(ctx, pgdb.PendingInviteParams{TokenHash: tokenHash, Now: millisecond(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.Invite{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.Invite{}, err
	}
	return inviteFromRow(pgdb.InviteRow(row)), nil
}

// RevokeInvite revokes a pending invite; used, revoked or unknown invites are ErrNotFound.
func (store *Store) RevokeInvite(ctx context.Context, id string, at time.Time) error {
	count, err := store.queries.RevokeInvite(ctx, pgdb.RevokeInviteParams{At: millisecond(at), ID: id})
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrNotFound
	}
	return nil
}

// AcceptInvite uses the pending invite in the account's transaction, so two
// acceptances create one account.
func (store *Store) AcceptInvite(ctx context.Context, inviteID string, account identityservice.Account, identity google.Identity, now time.Time) error {
	now = millisecond(now)
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.InviteEmailLock(ctx, pgdb.InviteEmailLockParams{Namespace: inviteEmailLocks, Email: identity.Email}); err != nil {
			return err
		}
		if err := insertAccount(ctx, queries, account, "", now); err != nil {
			return err
		}
		count, err := queries.UseInvite(ctx, pgdb.UseInviteParams{Now: now, UserID: account.ID, ID: inviteID, Sub: identity.Subject})
		if err != nil {
			return err
		}
		if count != 1 {
			return identityservice.ErrNotFound
		}
		// A concurrent acceptance linked the subject first.
		count, err = queries.InsertGoogleIdentity(ctx, pgdb.InsertGoogleIdentityParams{Sub: identity.Subject, UserID: account.ID, Email: identity.Email, UpdatedAt: now})
		if err != nil {
			return err
		}
		if count != 1 {
			return identityservice.ErrNotFound
		}
		return nil
	})
}

func (store *Store) GoogleAccount(ctx context.Context, identity google.Identity, now time.Time) (identityservice.Account, bool, error) {
	row, err := store.queries.GoogleAccount(ctx, identity.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.Account{}, false, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.Account{}, false, err
	}
	account := identityservice.Account{ID: row.UserID, Username: row.Username, Admin: row.Admin}
	err = store.queries.RecordGoogleEmail(ctx, pgdb.RecordGoogleEmailParams{Email: identity.Email, Now: millisecond(now), Sub: identity.Subject})
	return account, row.Disabled, err
}

func (store *Store) GoogleEmail(ctx context.Context, accountID string) (string, error) {
	email, err := store.queries.GoogleEmail(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return email, err
}

func (store *Store) PutGoogleFlow(ctx context.Context, stateHash string, flow identityservice.GoogleFlow) error {
	// Expired flows are removed when new ones start; consumed flows are deleted.
	if err := store.queries.PruneGoogleFlows(ctx, millisecond(time.Now())); err != nil {
		return err
	}
	var invite *string
	if flow.InviteID != "" {
		invite = &flow.InviteID
	}
	return store.queries.InsertGoogleFlow(ctx, pgdb.InsertGoogleFlowParams{
		StateHash: stateHash, BindingHash: flow.BindingHash, Nonce: flow.Nonce, PkceVerifier: flow.Verifier,
		InviteID: invite, ExpiresAt: millisecond(flow.ExpiresAt),
	})
}

// TakeGoogleFlow deletes the flow as it reads it, so one callback consumes it.
func (store *Store) TakeGoogleFlow(ctx context.Context, stateHash string, now time.Time) (identityservice.GoogleFlow, error) {
	row, err := store.queries.TakeGoogleFlow(ctx, pgdb.TakeGoogleFlowParams{StateHash: stateHash, Now: millisecond(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.GoogleFlow{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.GoogleFlow{}, err
	}
	flow := identityservice.GoogleFlow{BindingHash: row.BindingHash, Nonce: row.Nonce, Verifier: row.PkceVerifier, ExpiresAt: millisecond(row.ExpiresAt)}
	if row.InviteID != nil {
		flow.InviteID = *row.InviteID
	}
	return flow, nil
}
