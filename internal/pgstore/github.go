package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

// GitHub adapts the store to the integration's storage contract.
func (store *Store) GitHub() *GitHubStore { return &GitHubStore{store: store} }

type GitHubStore struct{ store *Store }

var _ githubsync.Store = (*GitHubStore)(nil)

func (s *GitHubStore) PutOAuthState(ctx context.Context, hash, ownerID string, expiresAt int64) error {
	return s.store.queries.PutGitHubOAuthState(ctx, pgdb.PutGitHubOAuthStateParams{StateHash: hash, OwnerID: ownerID, ExpiresAt: time.UnixMilli(expiresAt)})
}

func (s *GitHubStore) TakeOAuthState(ctx context.Context, hash string, now int64) (string, error) {
	owner, err := s.store.queries.TakeGitHubOAuthState(ctx, pgdb.TakeGitHubOAuthStateParams{StateHash: hash, Now: time.UnixMilli(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", githubsync.ErrNotFound
	}
	return owner, err
}

func (s *GitHubStore) PutConnection(ctx context.Context, c githubsync.Connection) error {
	return s.store.queries.PutGitHubConnection(ctx, pgdb.PutGitHubConnectionParams{
		OwnerID: c.OwnerID, UserID: c.UserID, Login: c.Login,
		AccessToken: c.AccessToken, AccessExpiresAt: time.UnixMilli(c.AccessExpiresAt),
		RefreshToken: c.RefreshToken, RefreshExpiresAt: time.UnixMilli(c.RefreshExpiresAt),
		Status: c.Status, ConnectedAt: time.UnixMilli(c.ConnectedAt),
	})
}

func connectionFromRow(row pgdb.GithubConnection) githubsync.Connection {
	return githubsync.Connection{
		OwnerID: row.OwnerID, UserID: row.UserID, Login: row.Login,
		AccessToken: row.AccessToken, AccessExpiresAt: row.AccessExpiresAt.UnixMilli(),
		RefreshToken: row.RefreshToken, RefreshExpiresAt: row.RefreshExpiresAt.UnixMilli(),
		Status: row.Status, ConnectedAt: row.ConnectedAt.UnixMilli(),
	}
}

func (s *GitHubStore) Connection(ctx context.Context, ownerID string) (githubsync.Connection, error) {
	row, err := s.store.queries.GitHubConnection(ctx, ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return githubsync.Connection{}, githubsync.ErrNotFound
	}
	if err != nil {
		return githubsync.Connection{}, err
	}
	return connectionFromRow(row), nil
}

func (s *GitHubStore) Connections(ctx context.Context) ([]githubsync.Connection, error) {
	rows, err := s.store.queries.GitHubConnections(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]githubsync.Connection, 0, len(rows))
	for _, row := range rows {
		result = append(result, connectionFromRow(row))
	}
	return result, nil
}

// Follows cascade with their connection.
func (s *GitHubStore) DeleteConnection(ctx context.Context, ownerID string) error {
	return s.store.queries.DeleteGitHubConnection(ctx, ownerID)
}

func (s *GitHubStore) RevokeUser(ctx context.Context, userID int64) error {
	return s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.RevokeGitHubUserConnections(ctx, userID); err != nil {
			return err
		}
		return queries.LoseGitHubUserFollows(ctx, userID)
	})
}

func (s *GitHubStore) PutFollow(ctx context.Context, f githubsync.Follow) error {
	return s.store.queries.PutGitHubFollow(ctx, pgdb.PutGitHubFollowParams{
		OwnerID: f.OwnerID, RepositoryID: f.RepositoryID, InstallationID: f.InstallationID, FullName: f.FullName,
		Status: f.Status, VerifiedAt: time.UnixMilli(f.VerifiedAt), CreatedAt: time.UnixMilli(f.CreatedAt),
	})
}

func (s *GitHubStore) DeleteFollow(ctx context.Context, ownerID string, repositoryID int64) error {
	return s.store.queries.DeleteGitHubFollow(ctx, pgdb.DeleteGitHubFollowParams{OwnerID: ownerID, RepositoryID: repositoryID})
}

func followFromRow(row pgdb.GitHubFollowsRow) githubsync.Follow {
	return githubsync.Follow{
		OwnerID: row.OwnerID, RepositoryID: row.RepositoryID, InstallationID: row.InstallationID, FullName: row.FullName,
		Status: row.Status, VerifiedAt: row.VerifiedAt.UnixMilli(), CreatedAt: row.CreatedAt.UnixMilli(), Muted: row.Muted,
	}
}

func (s *GitHubStore) Follows(ctx context.Context, ownerID string) ([]githubsync.Follow, error) {
	rows, err := s.store.queries.GitHubFollows(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	result := make([]githubsync.Follow, 0, len(rows))
	for _, row := range rows {
		result = append(result, followFromRow(row))
	}
	return result, nil
}

func (s *GitHubStore) RepositoryFollows(ctx context.Context, repositoryID int64) ([]githubsync.Follow, error) {
	rows, err := s.store.queries.GitHubRepositoryFollows(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	result := make([]githubsync.Follow, 0, len(rows))
	for _, row := range rows {
		// Both queries select the same columns.
		result = append(result, followFromRow(pgdb.GitHubFollowsRow(row)))
	}
	return result, nil
}

func (s *GitHubStore) SetMuted(ctx context.Context, ownerID string, repositoryID int64, muted bool) error {
	return s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if _, err := queries.LockGitHubFollow(ctx, pgdb.LockGitHubFollowParams{OwnerID: ownerID, RepositoryID: repositoryID}); errors.Is(err, pgx.ErrNoRows) {
			return githubsync.ErrNotFound
		} else if err != nil {
			return err
		}
		if muted {
			return queries.MuteGitHubFollow(ctx, pgdb.MuteGitHubFollowParams{OwnerID: ownerID, RepositoryID: repositoryID})
		}
		return queries.UnmuteGitHubFollow(ctx, pgdb.UnmuteGitHubFollowParams{OwnerID: ownerID, RepositoryID: repositoryID})
	})
}

func (s *GitHubStore) LoseInstallation(ctx context.Context, installationID int64, repositoryIDs []int64) error {
	if len(repositoryIDs) == 0 {
		return s.store.queries.LoseGitHubInstallation(ctx, installationID)
	}
	return s.store.queries.LoseGitHubInstallationRepositories(ctx, pgdb.LoseGitHubInstallationRepositoriesParams{InstallationID: installationID, RepositoryIds: repositoryIDs})
}

func (s *GitHubStore) SeenDelivery(ctx context.Context, id string) (bool, error) {
	return s.store.queries.SeenGitHubDelivery(ctx, id)
}

func (s *GitHubStore) RecordDelivery(ctx context.Context, id, event string, now int64) error {
	return s.store.queries.RecordGitHubDelivery(ctx, pgdb.RecordGitHubDeliveryParams{ID: id, Event: event, ReceivedAt: time.UnixMilli(now)})
}

func (s *GitHubStore) RequestRefresh(ctx context.Context, ref githubsync.PullRequestRef, retentionRun string, opened bool, now int64) error {
	return s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if err := queries.RequestGitHubRefresh(ctx, pgdb.RequestGitHubRefreshParams{
			RepositoryID: ref.RepositoryID, Number: int32(ref.Number), InstallationID: ref.InstallationID, FullName: ref.FullName,
			RetentionRun: retentionRun, RequestedAt: time.UnixMilli(now),
		}); err != nil {
			return err
		}
		if !opened {
			return nil
		}
		return queries.MarkGitHubPullRequestOpened(ctx, pgdb.MarkGitHubPullRequestOpenedParams{RepositoryID: ref.RepositoryID, Number: int32(ref.Number)})
	})
}

func (s *GitHubStore) PullRequestHead(ctx context.Context, repositoryID int64, number int) (string, bool, error) {
	row, err := s.store.queries.GitHubPullRequestHead(ctx, pgdb.GitHubPullRequestHeadParams{RepositoryID: repositoryID, Number: int32(number)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return row.HeadSha, row.Opened, err
}

func (s *GitHubStore) KnownOpen(ctx context.Context, repositoryID int64) ([]int, error) {
	numbers, err := s.store.queries.GitHubKnownOpen(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	result := make([]int, 0, len(numbers))
	for _, number := range numbers {
		result = append(result, int(number))
	}
	return result, nil
}

func (s *GitHubStore) ClaimRefresh(ctx context.Context, token string, now, leaseUntil int64) (githubsync.Lease, error) {
	row, err := s.store.queries.ClaimGitHubRefresh(ctx, pgdb.ClaimGitHubRefreshParams{Token: token, LeaseUntil: time.UnixMilli(leaseUntil), Now: time.UnixMilli(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return githubsync.Lease{}, githubsync.ErrNotFound
	}
	if err != nil {
		return githubsync.Lease{}, err
	}
	return githubsync.Lease{
		Ref:   githubsync.PullRequestRef{RepositoryID: row.RepositoryID, InstallationID: row.InstallationID, FullName: row.FullName, Number: int(row.Number)},
		Token: token, Generation: row.LeasedGeneration, RetentionRun: row.LeasedRun, Attempt: int(row.Attempt),
	}, nil
}

func (s *GitHubStore) RecordLease(ctx context.Context, lease githubsync.Lease, leased []byte, now int64) error {
	// pgx sends nil as NULL; nothing recorded is empty, as before a record.
	if leased == nil {
		leased = []byte{}
	}
	return fenced(s.store.queries.RecordGitHubLease(ctx, pgdb.RecordGitHubLeaseParams{
		Leased: leased, RepositoryID: lease.Ref.RepositoryID, Number: int32(lease.Ref.Number), Token: lease.Token, Now: time.UnixMilli(now),
	}))
}

func (s *GitHubStore) Leased(ctx context.Context, repositoryID int64, number int, token string, now int64) (githubsync.Lease, []byte, error) {
	row, err := s.store.queries.GitHubLeased(ctx, pgdb.GitHubLeasedParams{RepositoryID: repositoryID, Number: int32(number), Token: token, Now: time.UnixMilli(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return githubsync.Lease{Token: token}, nil, githubsync.ErrLeaseLost
	}
	if err != nil {
		return githubsync.Lease{Token: token}, nil, err
	}
	return githubsync.Lease{
		Ref:   githubsync.PullRequestRef{RepositoryID: row.RepositoryID, InstallationID: row.InstallationID, FullName: row.FullName, Number: int(row.Number)},
		Token: token, Generation: row.LeasedGeneration, RetentionRun: row.LeasedRun, Attempt: int(row.Attempt),
	}, row.Leased, nil
}

func (s *GitHubStore) ReleaseRefresh(ctx context.Context, lease githubsync.Lease, retryAt int64, detail string) error {
	return fenced(s.store.queries.ReleaseGitHubRefresh(ctx, pgdb.ReleaseGitHubRefreshParams{
		RetryAt: time.UnixMilli(retryAt), Detail: detail, RepositoryID: lease.Ref.RepositoryID, Number: int32(lease.Ref.Number), Token: lease.Token,
	}))
}

func (s *GitHubStore) CompleteRefresh(ctx context.Context, lease githubsync.Lease, state, head string, admissions []githubsync.Admission, now int64) error {
	at := time.UnixMilli(now)
	return s.store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		// Fence first, so a lost lease admits nothing.
		if err := fenced(queries.CompleteGitHubRefresh(ctx, pgdb.CompleteGitHubRefreshParams{
			State: state, RepositoryID: lease.Ref.RepositoryID, Number: int32(lease.Ref.Number), Token: lease.Token, Now: at,
		})); err != nil {
			return err
		}
		if head != "" {
			if err := queries.RecordGitHubPullRequestHead(ctx, pgdb.RecordGitHubPullRequestHeadParams{RepositoryID: lease.Ref.RepositoryID, Number: int32(lease.Ref.Number), HeadSha: head}); err != nil {
				return err
			}
		}
		for _, admission := range admissions {
			job, err := acceptJob(ctx, queries, admission.OwnerID, admission.Request)
			if err != nil {
				return err
			}
			if admission.Notification != nil {
				if err := insertNotification(ctx, queries, admission.OwnerID, job.ID, pullRequestNotification, *admission.Notification, at); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// fenced reports ErrLeaseLost unless the update matched the lease's row.
func fenced(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows != 1 {
		return githubsync.ErrLeaseLost
	}
	return nil
}
