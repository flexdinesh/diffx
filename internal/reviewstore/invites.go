package reviewstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
)

// CreateInvite replaces the email's pending invites, refusing an email an
// account already uses, in one transaction.
func (store *Store) CreateInvite(ctx context.Context, invite identityservice.Invite, tokenHash string) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var linked bool
	if err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM google_identities WHERE email=?)`, invite.Email).Scan(&linked); err != nil {
		return err
	}
	if linked {
		return identityservice.ErrConflict
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE account_invites SET revoked_at=? WHERE email=? AND used_at IS NULL AND revoked_at IS NULL`, invite.CreatedAt.UnixMilli(), invite.Email); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO account_invites(id,token_hash,email,created_at,expires_at) VALUES(?,?,?,?,?)`,
		invite.ID, tokenHash, invite.Email, invite.CreatedAt.UnixMilli(), invite.ExpiresAt.UnixMilli()); err != nil {
		return err
	}
	return transaction.Commit()
}

const inviteColumns = `i.id,i.email,i.created_at,i.expires_at,i.used_at,COALESCE(a.username,''),i.revoked_at FROM account_invites i LEFT JOIN remote_accounts a ON a.user_id=i.used_by`

type rowScanner interface{ Scan(...any) error }

func scanInvite(row rowScanner) (identityservice.Invite, error) {
	var invite identityservice.Invite
	var created, expires int64
	var used, revoked sql.NullInt64
	if err := row.Scan(&invite.ID, &invite.Email, &created, &expires, &used, &invite.UsedBy, &revoked); err != nil {
		return invite, err
	}
	invite.CreatedAt, invite.ExpiresAt = time.UnixMilli(created), time.UnixMilli(expires)
	if used.Valid {
		at := time.UnixMilli(used.Int64)
		invite.UsedAt = &at
	}
	if revoked.Valid {
		at := time.UnixMilli(revoked.Int64)
		invite.RevokedAt = &at
	}
	return invite, nil
}

func (store *Store) Invites(ctx context.Context) ([]identityservice.Invite, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT `+inviteColumns+` ORDER BY i.created_at DESC,i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invites := make([]identityservice.Invite, 0)
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

func (store *Store) Invite(ctx context.Context, id string) (identityservice.Invite, error) {
	invite, err := scanInvite(store.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` WHERE i.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return invite, identityservice.ErrNotFound
	}
	return invite, err
}

func (store *Store) PendingInvite(ctx context.Context, tokenHash string, now time.Time) (identityservice.Invite, error) {
	invite, err := scanInvite(store.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` WHERE i.token_hash=? AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>?`, tokenHash, now.UnixMilli()))
	if errors.Is(err, sql.ErrNoRows) {
		return invite, identityservice.ErrNotFound
	}
	return invite, err
}

// RevokeInvite revokes a pending invite; used, revoked or unknown invites are ErrNotFound.
func (store *Store) RevokeInvite(ctx context.Context, id string, at time.Time) error {
	result, err := store.db.ExecContext(ctx, `UPDATE account_invites SET revoked_at=? WHERE id=? AND used_at IS NULL AND revoked_at IS NULL`, at.UnixMilli(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
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
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := insertAccount(ctx, transaction, account, "", now); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE account_invites SET used_at=?,used_by=? WHERE id=? AND used_at IS NULL AND revoked_at IS NULL AND expires_at>?
   AND NOT EXISTS(SELECT 1 FROM google_identities WHERE sub=?)`, now.UnixMilli(), account.ID, inviteID, now.UnixMilli(), identity.Subject)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrNotFound
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO google_identities(sub,user_id,email,updated_at) VALUES(?,?,?,?)`, identity.Subject, account.ID, identity.Email, now.UnixMilli()); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) GoogleAccount(ctx context.Context, identity google.Identity, now time.Time) (identityservice.Account, bool, error) {
	var account identityservice.Account
	var disabled bool
	err := store.db.QueryRowContext(ctx, `SELECT `+accountColumns+`,NOT `+enabled+` FROM google_identities g JOIN remote_accounts a ON a.user_id=g.user_id WHERE g.sub=?`, identity.Subject).
		Scan(&account.ID, &account.Username, &account.Admin, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return account, false, identityservice.ErrNotFound
	}
	if err != nil {
		return account, false, err
	}
	_, err = store.db.ExecContext(ctx, `UPDATE google_identities SET email=?,updated_at=? WHERE sub=? AND email<>?`, identity.Email, now.UnixMilli(), identity.Subject, identity.Email)
	return account, disabled, err
}

func (store *Store) PutGoogleFlow(ctx context.Context, stateHash string, flow identityservice.GoogleFlow) error {
	// Expired flows are removed when new ones start; consumed flows are deleted.
	if _, err := store.db.ExecContext(ctx, `DELETE FROM google_auth_states WHERE expires_at<=?`, time.Now().UnixMilli()); err != nil {
		return err
	}
	var invite any
	if flow.InviteID != "" {
		invite = flow.InviteID
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO google_auth_states(state_hash,binding_hash,nonce,pkce_verifier,invite_id,expires_at) VALUES(?,?,?,?,?,?)`,
		stateHash, flow.BindingHash, flow.Nonce, flow.Verifier, invite, flow.ExpiresAt.UnixMilli())
	return err
}

func (store *Store) TakeGoogleFlow(ctx context.Context, stateHash string, now time.Time) (identityservice.GoogleFlow, error) {
	var flow identityservice.GoogleFlow
	var invite sql.NullString
	var expires int64
	err := store.db.QueryRowContext(ctx, `DELETE FROM google_auth_states WHERE state_hash=? AND expires_at>? RETURNING binding_hash,nonce,pkce_verifier,invite_id,expires_at`, stateHash, now.UnixMilli()).
		Scan(&flow.BindingHash, &flow.Nonce, &flow.Verifier, &invite, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return flow, identityservice.ErrNotFound
	}
	flow.InviteID, flow.ExpiresAt = invite.String, time.UnixMilli(expires)
	return flow, err
}
