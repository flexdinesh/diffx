package reviewstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/flexdinesh/diffx/internal/githubsync"
)

// GitHub integration state arrived in schema 12; every table is additive.
func initializeGitHub(transaction *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS github_oauth_states (state_hash TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS github_connections (owner_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, user_id INTEGER NOT NULL, login TEXT NOT NULL, access_token BLOB NOT NULL, access_expires_at INTEGER NOT NULL, refresh_token BLOB NOT NULL, refresh_expires_at INTEGER NOT NULL, status TEXT NOT NULL, connected_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS github_connections_user ON github_connections(user_id)`,
		`CREATE TABLE IF NOT EXISTS github_follows (owner_id TEXT NOT NULL REFERENCES github_connections(owner_id) ON DELETE CASCADE, repository_id INTEGER NOT NULL, installation_id INTEGER NOT NULL, full_name TEXT NOT NULL, status TEXT NOT NULL, verified_at INTEGER NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY(owner_id, repository_id))`,
		`CREATE INDEX IF NOT EXISTS github_follows_repository ON github_follows(repository_id, status)`,
		`CREATE INDEX IF NOT EXISTS github_follows_installation ON github_follows(installation_id)`,
		`CREATE TABLE IF NOT EXISTS github_deliveries (id TEXT PRIMARY KEY, event TEXT NOT NULL, received_at INTEGER NOT NULL)`,
		// One row per pull request coalesces refresh intent: webhooks and
		// reconciliation advance generation; the refresh task records the last
		// generation it processed and the state it observed.
		`CREATE TABLE IF NOT EXISTS github_pull_requests (repository_id INTEGER NOT NULL, number INTEGER NOT NULL, installation_id INTEGER NOT NULL, full_name TEXT NOT NULL, generation INTEGER NOT NULL, refreshed_generation INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL DEFAULT 'open', retention_run TEXT NOT NULL DEFAULT '', requested_at INTEGER NOT NULL, PRIMARY KEY(repository_id, number))`,
		`CREATE INDEX IF NOT EXISTS github_pull_requests_pending ON github_pull_requests(requested_at) WHERE generation>refreshed_generation`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// GitHub adapts the store to the integration's storage contract.
func (store *Store) GitHub() *GitHubStore { return &GitHubStore{store: store} }

type GitHubStore struct{ store *Store }

func (s *GitHubStore) PutOAuthState(ctx context.Context, hash, ownerID string, expiresAt int64) error {
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO github_oauth_states(state_hash,owner_id,expires_at) VALUES(?,?,?)`, hash, ownerID, expiresAt)
	return err
}

func (s *GitHubStore) TakeOAuthState(ctx context.Context, hash string, now int64) (string, error) {
	var owner string
	err := s.store.db.QueryRowContext(ctx, `DELETE FROM github_oauth_states WHERE state_hash=? AND expires_at>? RETURNING owner_id`, hash, now).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", githubsync.ErrNotFound
	}
	return owner, err
}

func (s *GitHubStore) PutConnection(ctx context.Context, c githubsync.Connection) error {
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO github_connections(owner_id,user_id,login,access_token,access_expires_at,refresh_token,refresh_expires_at,status,connected_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(owner_id) DO UPDATE SET user_id=excluded.user_id,login=excluded.login,access_token=excluded.access_token,
		access_expires_at=excluded.access_expires_at,refresh_token=excluded.refresh_token,refresh_expires_at=excluded.refresh_expires_at,status=excluded.status,connected_at=excluded.connected_at`,
		c.OwnerID, c.UserID, c.Login, c.AccessToken, c.AccessExpiresAt, c.RefreshToken, c.RefreshExpiresAt, c.Status, c.ConnectedAt)
	return err
}

const connectionColumns = `owner_id,user_id,login,access_token,access_expires_at,refresh_token,refresh_expires_at,status,connected_at`

func scanConnection(row scanner) (githubsync.Connection, error) {
	var c githubsync.Connection
	err := row.Scan(&c.OwnerID, &c.UserID, &c.Login, &c.AccessToken, &c.AccessExpiresAt, &c.RefreshToken, &c.RefreshExpiresAt, &c.Status, &c.ConnectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, githubsync.ErrNotFound
	}
	return c, err
}

func (s *GitHubStore) Connection(ctx context.Context, ownerID string) (githubsync.Connection, error) {
	return scanConnection(s.store.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM github_connections WHERE owner_id=?`, ownerID))
}

func (s *GitHubStore) Connections(ctx context.Context) ([]githubsync.Connection, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+connectionColumns+` FROM github_connections ORDER BY owner_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]githubsync.Connection, 0)
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Follows cascade with their connection.
func (s *GitHubStore) DeleteConnection(ctx context.Context, ownerID string) error {
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM github_connections WHERE owner_id=?`, ownerID)
	return err
}

func (s *GitHubStore) RevokeUser(ctx context.Context, userID int64) error {
	transaction, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `UPDATE github_connections SET status='revoked' WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE github_follows SET status='lost' WHERE owner_id IN (SELECT owner_id FROM github_connections WHERE user_id=?)`, userID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (s *GitHubStore) PutFollow(ctx context.Context, f githubsync.Follow) error {
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO github_follows(owner_id,repository_id,installation_id,full_name,status,verified_at,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(owner_id,repository_id) DO UPDATE SET installation_id=excluded.installation_id,full_name=excluded.full_name,status=excluded.status,verified_at=excluded.verified_at`,
		f.OwnerID, f.RepositoryID, f.InstallationID, f.FullName, f.Status, f.VerifiedAt, f.CreatedAt)
	return err
}

func (s *GitHubStore) DeleteFollow(ctx context.Context, ownerID string, repositoryID int64) error {
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM github_follows WHERE owner_id=? AND repository_id=?`, ownerID, repositoryID)
	return err
}

func (s *GitHubStore) follows(ctx context.Context, where string, argument any) ([]githubsync.Follow, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT owner_id,repository_id,installation_id,full_name,status,verified_at,created_at FROM github_follows WHERE `+where+` ORDER BY full_name,owner_id`, argument)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]githubsync.Follow, 0)
	for rows.Next() {
		var f githubsync.Follow
		if err := rows.Scan(&f.OwnerID, &f.RepositoryID, &f.InstallationID, &f.FullName, &f.Status, &f.VerifiedAt, &f.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (s *GitHubStore) Follows(ctx context.Context, ownerID string) ([]githubsync.Follow, error) {
	return s.follows(ctx, "owner_id=?", ownerID)
}

func (s *GitHubStore) RepositoryFollows(ctx context.Context, repositoryID int64) ([]githubsync.Follow, error) {
	return s.follows(ctx, "repository_id=?", repositoryID)
}

func (s *GitHubStore) LoseInstallation(ctx context.Context, installationID int64, repositoryIDs []int64) error {
	query := `UPDATE github_follows SET status='lost' WHERE installation_id=?`
	arguments := []any{installationID}
	if len(repositoryIDs) > 0 {
		query += ` AND repository_id IN (?` + strings.Repeat(",?", len(repositoryIDs)-1) + `)`
		for _, id := range repositoryIDs {
			arguments = append(arguments, id)
		}
	}
	_, err := s.store.db.ExecContext(ctx, query, arguments...)
	return err
}

func (s *GitHubStore) SeenDelivery(ctx context.Context, id string) (bool, error) {
	var found int
	err := s.store.db.QueryRowContext(ctx, `SELECT 1 FROM github_deliveries WHERE id=?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *GitHubStore) RecordDelivery(ctx context.Context, id, event string, now int64) error {
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO github_deliveries(id,event,received_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, id, event, now)
	return err
}

func (s *GitHubStore) RequestRefresh(ctx context.Context, ref githubsync.PullRequestRef, retentionRun string, now int64) error {
	_, err := s.store.db.ExecContext(ctx, `INSERT INTO github_pull_requests(repository_id,number,installation_id,full_name,generation,retention_run,requested_at) VALUES(?,?,?,?,1,?,?)
		ON CONFLICT(repository_id,number) DO UPDATE SET installation_id=excluded.installation_id,full_name=excluded.full_name,generation=generation+1,
		retention_run=CASE WHEN excluded.retention_run<>'' THEN excluded.retention_run ELSE retention_run END,requested_at=excluded.requested_at`,
		ref.RepositoryID, ref.Number, ref.InstallationID, ref.FullName, retentionRun, now)
	return err
}

func (s *GitHubStore) KnownOpen(ctx context.Context, repositoryID int64) ([]int, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT number FROM github_pull_requests WHERE repository_id=? AND state='open' ORDER BY number`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int, 0)
	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		result = append(result, number)
	}
	return result, rows.Err()
}

// pruneGitHub removes expired OAuth states and delivery identities older than
// GitHub's redelivery window with margin.
func pruneGitHub(transaction *sql.Tx, now int64) error {
	if _, err := transaction.Exec(`DELETE FROM github_oauth_states WHERE expires_at<=?`, now); err != nil {
		return err
	}
	_, err := transaction.Exec(`DELETE FROM github_deliveries WHERE received_at<=?`, now-7*24*60*60*1000)
	return err
}
