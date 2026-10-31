package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	_ identityservice.Store       = (*Store)(nil)
	_ identityservice.GoogleStore = (*Store)(nil)
)

// millisecond is the identity contract's resolution, matching the SQLite store.
func millisecond(at time.Time) time.Time { return time.UnixMilli(at.UnixMilli()) }

// account builds an identity account from a row's columns.
func account(id, username string, admin bool, method string) identityservice.Account {
	return identityservice.Account{ID: id, Username: username, Admin: admin, Method: identityservice.Method(method)}
}

func foreignKeyViolation(err error) bool {
	var failure *pgconn.PgError
	return errors.As(err, &failure) && failure.Code == "23503"
}

func insertAccount(ctx context.Context, queries *pgdb.Queries, account identityservice.Account, passwordHash string, created time.Time) error {
	if err := queries.InsertAccountUser(ctx, pgdb.InsertAccountUserParams{ID: account.ID, Name: account.Username, CreatedAt: millisecond(created)}); err != nil {
		return err
	}
	count, err := queries.InsertRemoteAccount(ctx, pgdb.InsertRemoteAccountParams{UserID: account.ID, Username: account.Username, PasswordHash: passwordHash})
	if err != nil {
		return err
	}
	if count == 0 {
		return identityservice.ErrConflict
	}
	return nil
}

// CreateAccount commits a member and its credential together. An empty hash
// never verifies, so such an account has no password sign-in.
func (store *Store) CreateAccount(ctx context.Context, account identityservice.Account, passwordHash string, created time.Time, required identityservice.Settings) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := requireSettings(ctx, queries, required); err != nil {
			return err
		}
		return insertAccount(ctx, queries, account, passwordHash, created)
	})
}

func (store *Store) PendingAdminSetup(ctx context.Context, setupHash string, now time.Time) (bool, error) {
	return store.queries.PendingAdminSetup(ctx, pgdb.PendingAdminSetupParams{TokenHash: setupHash, Now: millisecond(now)})
}

func (store *Store) PutAdminSetup(ctx context.Context, tokenHash string, expires time.Time) (bool, error) {
	var stored bool
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.IdentityLock(ctx, adminLock); err != nil {
			return err
		}
		count, err := queries.PutAdminSetup(ctx, pgdb.PutAdminSetupParams{TokenHash: tokenHash, ExpiresAt: millisecond(expires)})
		stored = count == 1
		return err
	})
	return stored, err
}

func (store *Store) AdminExists(ctx context.Context) (bool, error) {
	return store.queries.AdminExists(ctx)
}

// CreateAdmin consumes the setup secret, so concurrent setups create one admin.
func (store *Store) CreateAdmin(ctx context.Context, account identityservice.Account, passwordHash, setupHash string, now time.Time) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := consumeAdminSetup(ctx, queries, setupHash, now); err != nil {
			return err
		}
		if err := insertAccount(ctx, queries, account, passwordHash, now); err != nil {
			return err
		}
		return queries.InsertAdmin(ctx, account.ID)
	})
}

// CreateGoogleAdmin is CreateAdmin for a new account with a Google identity.
func (store *Store) CreateGoogleAdmin(ctx context.Context, account identityservice.Account, identity google.Identity, setupHash string, now time.Time) error {
	now = millisecond(now)
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := consumeAdminSetup(ctx, queries, setupHash, now); err != nil {
			return err
		}
		if err := insertGoogleAccount(ctx, queries, account, identity, now); err != nil {
			return err
		}
		return queries.InsertAdmin(ctx, account.ID)
	})
}

// PromoteSetupAdmin consumes the setup secret for an existing enabled account.
func (store *Store) PromoteSetupAdmin(ctx context.Context, accountID, setupHash string, now time.Time) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := consumeAdminSetup(ctx, queries, setupHash, now); err != nil {
			return err
		}
		target, err := queries.LockTargetAccount(ctx, accountID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && target.Disabled {
			return identityservice.ErrNotFound
		}
		if err != nil {
			return err
		}
		return queries.InsertAdmin(ctx, accountID)
	})
}

// consumeAdminSetup takes the admin lock and the unexpired setup secret.
func consumeAdminSetup(ctx context.Context, queries *pgdb.Queries, setupHash string, now time.Time) error {
	if err := queries.IdentityLock(ctx, adminLock); err != nil {
		return err
	}
	count, err := queries.ConsumeAdminSetup(ctx, pgdb.ConsumeAdminSetupParams{TokenHash: setupHash, Now: millisecond(now)})
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrNotFound
	}
	return nil
}

func (store *Store) AccountSummaries(ctx context.Context) ([]identityservice.AccountSummary, error) {
	rows, err := store.queries.AccountSummaries(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]identityservice.AccountSummary, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, identityservice.AccountSummary{
			Account: account(row.UserID, row.Username, row.Admin, row.Method),
			Email:   row.Email, Disabled: row.Disabled, CreatedAt: millisecond(row.CreatedAt),
		})
	}
	return accounts, nil
}

// targetAccount locks the account an admin action applies to. When the action
// removes an enabled admin (removesAdmin), it refuses unless another enabled
// admin remains; the admin lock orders it with every other admin change.
func targetAccount(ctx context.Context, queries *pgdb.Queries, accountID string, removesAdmin bool) error {
	if err := queries.IdentityLock(ctx, adminLock); err != nil {
		return err
	}
	target, err := queries.LockTargetAccount(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !removesAdmin || !target.Admin || target.Disabled {
		return nil
	}
	others, err := queries.OtherEnabledAdmin(ctx, accountID)
	if err != nil {
		return err
	}
	if !others {
		return identityservice.ErrLastAdmin
	}
	return nil
}

// SetAccountDisabled revokes every browser session and API token at the commit
// that disables the account; enabling restores sign-in, not those credentials.
func (store *Store) SetAccountDisabled(ctx context.Context, accountID string, disabled bool, at time.Time) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := targetAccount(ctx, queries, accountID, disabled); err != nil {
			return err
		}
		if !disabled {
			return queries.EnableAccount(ctx, accountID)
		}
		if err := queries.DisableAccount(ctx, pgdb.DisableAccountParams{UserID: accountID, DisabledAt: millisecond(at)}); err != nil {
			return err
		}
		if err := queries.DeleteAccountSessions(ctx, accountID); err != nil {
			return err
		}
		if err := queries.DeleteAccountTokens(ctx, accountID); err != nil {
			return err
		}
		return notifyAccount(ctx, queries, accountID)
	})
}

// DeleteAccount removes the owner row; foreign keys cascade to everything it
// owns. Queued jobs reference ingestion records without a cascade, so they go
// first; the owner lock keeps new records out until the delete commits.
func (store *Store) DeleteAccount(ctx context.Context, accountID string) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := targetAccount(ctx, queries, accountID, true); err != nil {
			return err
		}
		if err := queries.DeleteAccountJobs(ctx, accountID); err != nil {
			return err
		}
		if err := queries.DeleteAccountUser(ctx, accountID); err != nil {
			return err
		}
		return notifyAccount(ctx, queries, accountID)
	})
}

// SetAdmin grants or removes the admin role; removing the last enabled admin
// is ErrLastAdmin.
func (store *Store) SetAdmin(ctx context.Context, accountID string, admin bool) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := targetAccount(ctx, queries, accountID, !admin); err != nil {
			return err
		}
		if admin {
			return queries.GrantAdmin(ctx, accountID)
		}
		return queries.RevokeAdmin(ctx, accountID)
	})
}

func (store *Store) AccountPassword(ctx context.Context, username string) (identityservice.PasswordCredential, error) {
	row, err := store.queries.AccountPassword(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.PasswordCredential{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.PasswordCredential{}, err
	}
	return identityservice.PasswordCredential{Account: account(row.UserID, row.Username, row.Admin, row.Method), Hash: row.PasswordHash}, nil
}

func (store *Store) AccountPasswordByID(ctx context.Context, accountID string) (identityservice.PasswordCredential, error) {
	row, err := store.queries.AccountPasswordByID(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.PasswordCredential{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.PasswordCredential{}, err
	}
	return identityservice.PasswordCredential{Account: account(row.UserID, row.Username, row.Admin, row.Method), Hash: row.PasswordHash}, nil
}

// ReplacePassword fences authentication against password changes/reset, and
// invalidates every browser session at the same commit. API tokens are independent.
func (store *Store) ReplacePassword(ctx context.Context, accountID, expected, replacement string) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		count, err := queries.ReplacePassword(ctx, pgdb.ReplacePasswordParams{Replacement: replacement, UserID: accountID, Expected: expected})
		if err != nil {
			return err
		}
		if count != 1 {
			return identityservice.ErrAuthentication
		}
		return queries.DeleteAccountSessions(ctx, accountID)
	})
}

// CreateSession issues only against the verified hash of an enabled account.
// The account lock precedes the check, so the check's snapshot includes any
// credential change that committed first, and later changes revoke the session.
func (store *Store) CreateSession(ctx context.Context, accountID, expected, secretHash, csrf string, expires time.Time) error {
	expires = millisecond(expires)
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.LockSessionAccount(ctx, accountID); err != nil {
			return err
		}
		// Remove expired credentials during issuance; retained live sessions are never evicted.
		if err := queries.PruneBrowserSessions(ctx, expires.Add(-identityservice.SessionLifetime)); err != nil {
			return err
		}
		count, err := queries.InsertSession(ctx, pgdb.InsertSessionParams{SecretHash: secretHash, CsrfToken: csrf, ExpiresAt: expires, UserID: accountID, Expected: expected})
		if err != nil {
			return err
		}
		if count != 1 {
			return identityservice.ErrAuthentication
		}
		return nil
	})
}

func (store *Store) ResolveSession(ctx context.Context, secretHash string, now time.Time) (identityservice.Session, error) {
	row, err := store.queries.ResolveSession(ctx, pgdb.ResolveSessionParams{SecretHash: secretHash, Now: millisecond(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.Session{}, identityservice.ErrAuthentication
	}
	if err != nil {
		return identityservice.Session{}, err
	}
	return identityservice.Session{Account: account(row.UserID, row.Username, row.Admin, row.Method), CSRFToken: row.CsrfToken, ExpiresAt: millisecond(row.ExpiresAt)}, nil
}

func (store *Store) DeleteSession(ctx context.Context, secretHash string) error {
	return store.queries.DeleteSession(ctx, secretHash)
}

func (store *Store) CreateIdentityToken(ctx context.Context, accountID string, token identityservice.Token, secretHash string) error {
	count, err := store.queries.InsertIdentityToken(ctx, pgdb.InsertIdentityTokenParams{
		ID: token.ID, Name: token.Name, Purpose: string(token.Purpose), SecretHash: secretHash,
		CreatedAt: millisecond(token.CreatedAt), ExpiresAt: millisecond(token.ExpiresAt), UserID: accountID,
	})
	// The account was deleted after the insert read it.
	if foreignKeyViolation(err) {
		return identityservice.ErrNotFound
	}
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrNotFound
	}
	return nil
}

func (store *Store) IdentityTokens(ctx context.Context, accountID string) ([]identityservice.Token, error) {
	rows, err := store.queries.IdentityTokens(ctx, accountID)
	if err != nil {
		return nil, err
	}
	tokens := make([]identityservice.Token, 0, len(rows))
	for _, row := range rows {
		token := identityservice.Token{ID: row.ID, Name: row.Name, Purpose: identityservice.Purpose(row.Purpose), CreatedAt: millisecond(row.CreatedAt), ExpiresAt: millisecond(row.ExpiresAt)}
		if row.LastUsedAt != nil {
			last := millisecond(*row.LastUsedAt)
			token.LastUsedAt = &last
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func (store *Store) DeleteIdentityToken(ctx context.Context, accountID, tokenID string) error {
	count, err := store.queries.DeleteIdentityToken(ctx, pgdb.DeleteIdentityTokenParams{UserID: accountID, ID: tokenID})
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrNotFound
	}
	return nil
}

// ResolveIdentityToken authenticates and records the use in one statement.
func (store *Store) ResolveIdentityToken(ctx context.Context, secretHash string, now time.Time) (identityservice.TokenIdentity, error) {
	row, err := store.queries.ResolveIdentityToken(ctx, pgdb.ResolveIdentityTokenParams{Now: millisecond(now), SecretHash: secretHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.TokenIdentity{}, identityservice.ErrAuthentication
	}
	if err != nil {
		return identityservice.TokenIdentity{}, err
	}
	return identityservice.TokenIdentity{Account: account(row.UserID, row.Username, row.Admin, row.Method), Purpose: identityservice.Purpose(row.Purpose), ExpiresAt: millisecond(row.ExpiresAt)}, nil
}

func (store *Store) IdentityAccounts(ctx context.Context) ([]identityservice.Account, error) {
	rows, err := store.queries.IdentityAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]identityservice.Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, account(row.UserID, row.Username, row.Admin, row.Method))
	}
	return accounts, nil
}

// PutPasswordReset replaces the account's pending reset.
func (store *Store) PutPasswordReset(ctx context.Context, accountID, tokenHash string, expires time.Time) error {
	err := store.queries.PutPasswordReset(ctx, pgdb.PutPasswordResetParams{UserID: accountID, TokenHash: tokenHash, ExpiresAt: millisecond(expires)})
	// The account was deleted after the caller read it.
	if foreignKeyViolation(err) {
		return identityservice.ErrNotFound
	}
	return err
}

func (store *Store) PasswordReset(ctx context.Context, tokenHash string, now time.Time) (identityservice.PasswordCredential, error) {
	row, err := store.queries.PasswordReset(ctx, pgdb.PasswordResetParams{TokenHash: tokenHash, Now: millisecond(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityservice.PasswordCredential{}, identityservice.ErrNotFound
	}
	if err != nil {
		return identityservice.PasswordCredential{}, err
	}
	return identityservice.PasswordCredential{Account: account(row.UserID, row.Username, row.Admin, row.Method), Hash: row.PasswordHash}, nil
}

// CompletePasswordReset consumes the reset, replaces the password fenced on
// the verified hash and revokes browser sessions at one commit.
func (store *Store) CompletePasswordReset(ctx context.Context, tokenHash, expected, replacement string, now time.Time) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		accountID, err := queries.ConsumePasswordReset(ctx, pgdb.ConsumePasswordResetParams{TokenHash: tokenHash, Now: millisecond(now)})
		if errors.Is(err, pgx.ErrNoRows) {
			return identityservice.ErrNotFound
		}
		if err != nil {
			return err
		}
		count, err := queries.ReplacePassword(ctx, pgdb.ReplacePasswordParams{Replacement: replacement, UserID: accountID, Expected: expected})
		if err != nil {
			return err
		}
		// The password changed after the reset was read.
		if count != 1 {
			return identityservice.ErrNotFound
		}
		return queries.DeleteAccountSessions(ctx, accountID)
	})
}
