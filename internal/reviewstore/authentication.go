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
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// CreateAccount commits the owner and its initial credential together. Bootstrap
// checks all remote accounts in this transaction, so changed env never creates
// another owner or replaces credentials on restart.
func (store *Store) CreateAccount(ctx context.Context, account identityservice.Account, passwordHash string, bootstrap bool, created time.Time) (identityservice.Account, error) {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return identityservice.Account{}, err
	}
	defer transaction.Rollback()
	if bootstrap {
		var existing identityservice.Account
		err := transaction.QueryRowContext(ctx, `SELECT user_id,username FROM remote_accounts ORDER BY username LIMIT 1`).Scan(&existing.ID, &existing.Username)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return identityservice.Account{}, err
		}
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO users(id,os_uid,name,created_at) VALUES(?,?,?,?)`, account.ID, "remote:"+account.ID, account.Username, created.UnixMilli()); err != nil {
		return identityservice.Account{}, err
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO remote_accounts(user_id,username,password_hash) VALUES(?,?,?) ON CONFLICT(username) DO NOTHING`, account.ID, account.Username, passwordHash)
	if err != nil {
		return identityservice.Account{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return identityservice.Account{}, err
	}
	if count == 0 {
		return identityservice.Account{}, identityservice.ErrConflict
	}
	return account, transaction.Commit()
}

func (store *Store) AccountPassword(ctx context.Context, username string) (identityservice.PasswordCredential, error) {
	var result identityservice.PasswordCredential
	err := store.db.QueryRowContext(ctx, `SELECT user_id,username,password_hash FROM remote_accounts WHERE username=?`, username).Scan(&result.Account.ID, &result.Account.Username, &result.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return result, identityservice.ErrNotFound
	}
	return result, err
}

func (store *Store) AccountPasswordByID(ctx context.Context, accountID string) (identityservice.PasswordCredential, error) {
	var result identityservice.PasswordCredential
	err := store.db.QueryRowContext(ctx, `SELECT user_id,username,password_hash FROM remote_accounts WHERE user_id=?`, accountID).Scan(&result.Account.ID, &result.Account.Username, &result.Hash)
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
	result, err := transaction.ExecContext(ctx, `INSERT INTO browser_sessions(secret_hash,user_id,csrf_token,expires_at) SELECT ?,user_id,?,? FROM remote_accounts WHERE user_id=? AND password_hash=?`, secretHash, csrf, expires.UnixMilli(), accountID, expected)
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
	err := store.db.QueryRowContext(ctx, `SELECT a.user_id,a.username,s.csrf_token,s.expires_at FROM browser_sessions s JOIN remote_accounts a ON a.user_id=s.user_id WHERE s.secret_hash=? AND s.expires_at>?`, secretHash, now.UnixMilli()).Scan(&session.Account.ID, &session.Account.Username, &session.CSRFToken, &expires)
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
	err = transaction.QueryRowContext(ctx, `SELECT a.user_id,a.username,t.purpose,t.expires_at FROM identity_tokens t JOIN remote_accounts a ON a.user_id=t.user_id WHERE t.secret_hash=? AND t.expires_at>?`, secretHash, now.UnixMilli()).Scan(&result.Account.ID, &result.Account.Username, &result.Purpose, &expires)
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
	rows, err := store.db.QueryContext(ctx, `SELECT user_id,username FROM remote_accounts ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]identityservice.Account, 0)
	for rows.Next() {
		var account identityservice.Account
		if err := rows.Scan(&account.ID, &account.Username); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}
