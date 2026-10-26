package reviewstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
)

var _ identityservice.Store = (*Store)(nil)

func initializeAuthentication(transaction *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS remote_accounts (
   user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
   username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS browser_sessions (
   secret_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
   csrf_token TEXT NOT NULL, expires_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS browser_sessions_owner ON browser_sessions(user_id)`,
		`CREATE TABLE IF NOT EXISTS identity_tokens (
   id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
   name TEXT NOT NULL, purpose TEXT NOT NULL CHECK(purpose IN ('collector','api')),
   secret_hash TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL,
   last_used_at INTEGER, expires_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS identity_tokens_owner ON identity_tokens(user_id)`,
		// Schema 13 adds administration and Google sign-in; every table is additive.
		// The singleton keys allow at most one admin and one pending setup secret.
		`CREATE TABLE IF NOT EXISTS admin_account (
   singleton INTEGER PRIMARY KEY CHECK(singleton=1),
   user_id TEXT NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS admin_setup (
   singleton INTEGER PRIMARY KEY CHECK(singleton=1),
   token_hash TEXT NOT NULL, expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS disabled_accounts (
   user_id TEXT PRIMARY KEY REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
   disabled_at INTEGER NOT NULL)`,
		// Google's subject identifies the person; email is the latest one Google reported.
		`CREATE TABLE IF NOT EXISTS google_identities (
   sub TEXT PRIMARY KEY, user_id TEXT NOT NULL UNIQUE REFERENCES remote_accounts(user_id) ON DELETE CASCADE,
   email TEXT NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS google_identities_email ON google_identities(email)`,
		`CREATE TABLE IF NOT EXISTS account_invites (
   id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, email TEXT NOT NULL,
   created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL,
   used_at INTEGER, used_by TEXT REFERENCES remote_accounts(user_id) ON DELETE SET NULL, revoked_at INTEGER)`,
		`CREATE INDEX IF NOT EXISTS account_invites_email ON account_invites(email)`,
		`CREATE TABLE IF NOT EXISTS google_auth_states (
   state_hash TEXT PRIMARY KEY, binding_hash TEXT NOT NULL, nonce TEXT NOT NULL, pkce_verifier TEXT NOT NULL,
   invite_id TEXT REFERENCES account_invites(id) ON DELETE CASCADE, expires_at INTEGER NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// accountColumns selects an Account from remote_accounts aliased as a.
const accountColumns = `a.user_id,a.username,EXISTS(SELECT 1 FROM admin_account WHERE user_id=a.user_id)`

// enabled restricts remote_accounts aliased as a to accounts that may authenticate.
const enabled = `NOT EXISTS(SELECT 1 FROM disabled_accounts WHERE user_id=a.user_id)`

func insertAccount(ctx context.Context, transaction *sql.Tx, account identityservice.Account, passwordHash string, created time.Time) error {
	if _, err := transaction.ExecContext(ctx, `INSERT INTO users(id,os_uid,name,created_at) VALUES(?,?,?,?)`, account.ID, "remote:"+account.ID, account.Username, created.UnixMilli()); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO remote_accounts(user_id,username,password_hash) VALUES(?,?,?) ON CONFLICT(username) DO NOTHING`, account.ID, account.Username, passwordHash)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return identityservice.ErrConflict
	}
	return nil
}

// CreateAccount commits a non-admin owner and its credential together. An empty
// hash never verifies, so such an account has no password sign-in.
func (store *Store) CreateAccount(ctx context.Context, account identityservice.Account, passwordHash string, created time.Time) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := insertAccount(ctx, transaction, account, passwordHash, created); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) PutAdminSetup(ctx context.Context, tokenHash string, expires time.Time) (bool, error) {
	result, err := store.db.ExecContext(ctx, `INSERT INTO admin_setup(singleton,token_hash,expires_at) SELECT 1,?,? WHERE NOT EXISTS(SELECT 1 FROM admin_account)
   ON CONFLICT(singleton) DO UPDATE SET token_hash=excluded.token_hash,expires_at=excluded.expires_at`, tokenHash, expires.UnixMilli())
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (store *Store) AdminExists(ctx context.Context) (bool, error) {
	var exists bool
	err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_account)`).Scan(&exists)
	return exists, err
}

// CreateAdmin consumes the setup secret, so concurrent setups create one admin.
func (store *Store) CreateAdmin(ctx context.Context, account identityservice.Account, passwordHash, setupHash string, now time.Time) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `DELETE FROM admin_setup WHERE token_hash=? AND expires_at>? AND NOT EXISTS(SELECT 1 FROM admin_account)`, setupHash, now.UnixMilli())
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
	if err := insertAccount(ctx, transaction, account, passwordHash, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO admin_account(singleton,user_id) VALUES(1,?)`, account.ID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) AccountSummaries(ctx context.Context) ([]identityservice.AccountSummary, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT `+accountColumns+`,COALESCE(g.email,''),NOT `+enabled+`,u.created_at
   FROM remote_accounts a JOIN users u ON u.id=a.user_id LEFT JOIN google_identities g ON g.user_id=a.user_id ORDER BY a.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]identityservice.AccountSummary, 0)
	for rows.Next() {
		var account identityservice.AccountSummary
		var created int64
		if err := rows.Scan(&account.ID, &account.Username, &account.Admin, &account.Email, &account.Disabled, &created); err != nil {
			return nil, err
		}
		account.CreatedAt = time.UnixMilli(created)
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// targetAccount locks in the account an admin action applies to, refusing the admin.
func targetAccount(ctx context.Context, transaction *sql.Tx, accountID string) error {
	var admin bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_account WHERE user_id=a.user_id) FROM remote_accounts a WHERE a.user_id=?`, accountID).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) {
		return identityservice.ErrNotFound
	}
	if err != nil {
		return err
	}
	if admin {
		return identityservice.ErrForbidden
	}
	return nil
}

// SetAccountDisabled revokes every browser session and API token at the commit
// that disables the account; enabling restores sign-in, not those credentials.
func (store *Store) SetAccountDisabled(ctx context.Context, accountID string, disabled bool, at time.Time) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := targetAccount(ctx, transaction, accountID); err != nil {
		return err
	}
	statements := []string{`DELETE FROM disabled_accounts WHERE user_id=?`}
	if disabled {
		statements = []string{`INSERT INTO disabled_accounts(user_id,disabled_at) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, `DELETE FROM browser_sessions WHERE user_id=?`, `DELETE FROM identity_tokens WHERE user_id=?`}
	}
	for index, statement := range statements {
		arguments := []any{accountID}
		if disabled && index == 0 {
			arguments = append(arguments, at.UnixMilli())
		}
		if _, err := transaction.ExecContext(ctx, statement, arguments...); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

// DeleteAccount removes the owner row; foreign keys cascade to everything it
// owns. Queued jobs reference ingestion records without a cascade, so they go first.
func (store *Store) DeleteAccount(ctx context.Context, accountID string) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := targetAccount(ctx, transaction, accountID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM ingestion_jobs WHERE sequence IN (SELECT sequence FROM ingestion_records WHERE owner_id=?)`, accountID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM users WHERE id=?`, accountID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) AccountPassword(ctx context.Context, username string) (identityservice.PasswordCredential, error) {
	var result identityservice.PasswordCredential
	err := store.db.QueryRowContext(ctx, `SELECT `+accountColumns+`,a.password_hash FROM remote_accounts a WHERE a.username=? AND `+enabled, username).Scan(&result.Account.ID, &result.Account.Username, &result.Account.Admin, &result.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return result, identityservice.ErrNotFound
	}
	return result, err
}

func (store *Store) AccountPasswordByID(ctx context.Context, accountID string) (identityservice.PasswordCredential, error) {
	var result identityservice.PasswordCredential
	err := store.db.QueryRowContext(ctx, `SELECT `+accountColumns+`,a.password_hash FROM remote_accounts a WHERE a.user_id=? AND `+enabled, accountID).Scan(&result.Account.ID, &result.Account.Username, &result.Account.Admin, &result.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return result, identityservice.ErrNotFound
	}
	return result, err
}

// ReplacePassword fences authentication against password changes/reset, and
// invalidates every browser session at the same commit. API tokens are independent.
func (store *Store) ReplacePassword(ctx context.Context, accountID, expected, replacement string) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `UPDATE remote_accounts SET password_hash=? WHERE user_id=? AND password_hash=?`, replacement, accountID, expected)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrAuthentication
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM browser_sessions WHERE user_id=?`, accountID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) CreateSession(ctx context.Context, accountID, expected, secretHash, csrf string, expires time.Time) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	// Remove expired credentials during issuance; retained live sessions are never evicted.
	if _, err := transaction.ExecContext(ctx, `DELETE FROM browser_sessions WHERE expires_at<=?`, expires.Add(-identityservice.SessionLifetime).UnixMilli()); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO browser_sessions(secret_hash,user_id,csrf_token,expires_at) SELECT ?,a.user_id,?,? FROM remote_accounts a WHERE a.user_id=? AND a.password_hash=? AND `+enabled, secretHash, csrf, expires.UnixMilli(), accountID, expected)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return identityservice.ErrAuthentication
	}
	return transaction.Commit()
}

func (store *Store) ResolveSession(ctx context.Context, secretHash string, now time.Time) (identityservice.Session, error) {
	var session identityservice.Session
	var expires int64
	err := store.db.QueryRowContext(ctx, `SELECT `+accountColumns+`,s.csrf_token,s.expires_at FROM browser_sessions s JOIN remote_accounts a ON a.user_id=s.user_id WHERE s.secret_hash=? AND s.expires_at>? AND `+enabled, secretHash, now.UnixMilli()).Scan(&session.Account.ID, &session.Account.Username, &session.Account.Admin, &session.CSRFToken, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return session, identityservice.ErrAuthentication
	}
	session.ExpiresAt = time.UnixMilli(expires)
	return session, err
}

func (store *Store) DeleteSession(ctx context.Context, secretHash string) error {
	_, err := store.db.ExecContext(ctx, `DELETE FROM browser_sessions WHERE secret_hash=?`, secretHash)
	return err
}

func (store *Store) CreateIdentityToken(ctx context.Context, accountID string, token identityservice.Token, secretHash string) error {
	result, err := store.db.ExecContext(ctx, `INSERT INTO identity_tokens(id,user_id,name,purpose,secret_hash,created_at,expires_at) SELECT ?,user_id,?,?,?,?,? FROM remote_accounts WHERE user_id=?`, token.ID, token.Name, token.Purpose, secretHash, token.CreatedAt.UnixMilli(), token.ExpiresAt.UnixMilli(), accountID)
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

func (store *Store) IdentityTokens(ctx context.Context, accountID string) ([]identityservice.Token, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id,name,purpose,created_at,last_used_at,expires_at FROM identity_tokens WHERE user_id=? ORDER BY created_at DESC,id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]identityservice.Token, 0)
	for rows.Next() {
		var token identityservice.Token
		var created, expires int64
		var lastUsed sql.NullInt64
		if err := rows.Scan(&token.ID, &token.Name, &token.Purpose, &created, &lastUsed, &expires); err != nil {
			return nil, err
		}
		token.CreatedAt = time.UnixMilli(created)
		token.ExpiresAt = time.UnixMilli(expires)
		if lastUsed.Valid {
			last := time.UnixMilli(lastUsed.Int64)
			token.LastUsedAt = &last
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (store *Store) DeleteIdentityToken(ctx context.Context, accountID, tokenID string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM identity_tokens WHERE user_id=? AND id=?`, accountID, tokenID)
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

func (store *Store) ResolveIdentityToken(ctx context.Context, secretHash string, now time.Time) (identityservice.TokenIdentity, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return identityservice.TokenIdentity{}, err
	}
	defer transaction.Rollback()
	var result identityservice.TokenIdentity
	var expires int64
	err = transaction.QueryRowContext(ctx, `SELECT `+accountColumns+`,t.purpose,t.expires_at FROM identity_tokens t JOIN remote_accounts a ON a.user_id=t.user_id WHERE t.secret_hash=? AND t.expires_at>? AND `+enabled, secretHash, now.UnixMilli()).Scan(&result.Account.ID, &result.Account.Username, &result.Account.Admin, &result.Purpose, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return result, identityservice.ErrAuthentication
	}
	if err != nil {
		return result, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE identity_tokens SET last_used_at=CASE WHEN last_used_at IS NULL OR last_used_at<? THEN ? ELSE last_used_at END WHERE secret_hash=?`, now.UnixMilli(), now.UnixMilli(), secretHash); err != nil {
		return result, err
	}
	result.ExpiresAt = time.UnixMilli(expires)
	return result, transaction.Commit()
}

func (store *Store) IdentityAccounts(ctx context.Context) ([]identityservice.Account, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM remote_accounts a WHERE `+enabled+` ORDER BY a.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]identityservice.Account, 0)
	for rows.Next() {
		var account identityservice.Account
		if err := rows.Scan(&account.ID, &account.Username, &account.Admin); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}
