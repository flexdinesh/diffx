package githubsync

import (
	"context"
	"errors"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

// ErrNotFound reports an absent connection, follow or OAuth state.
var ErrNotFound = errors.New("not found")

// Connection is one account's GitHub user authorization. Tokens are sealed;
// storage never sees plaintext.
type Connection struct {
	OwnerID          string
	UserID           int64
	Login            string
	AccessToken      []byte
	AccessExpiresAt  int64
	RefreshToken     []byte
	RefreshExpiresAt int64
	Status           string // active, revoked or expired
	ConnectedAt      int64
}

// Follow is an account's subscription to a repository's pull requests. Lost
// follows stay listed but receive nothing until access is verified again.
type Follow struct {
	OwnerID        string
	RepositoryID   int64
	InstallationID int64
	FullName       string
	Status         string // active or lost
	VerifiedAt     int64
	CreatedAt      int64
}

// PullRequestRef addresses one pull request through the installation that can read it.
type PullRequestRef struct {
	RepositoryID   int64
	InstallationID int64
	FullName       string
	Number         int
}

// Store persists integration state atomically per operation.
type Store interface {
	PutOAuthState(ctx context.Context, hash, ownerID string, expiresAt int64) error
	// TakeOAuthState consumes an unexpired state exactly once.
	TakeOAuthState(ctx context.Context, hash string, now int64) (string, error)
	PutConnection(ctx context.Context, connection Connection) error
	Connection(ctx context.Context, ownerID string) (Connection, error)
	Connections(ctx context.Context) ([]Connection, error)
	// DeleteConnection removes the authorization and every follow of the account.
	DeleteConnection(ctx context.Context, ownerID string) error
	// RevokeUser marks every connection of the GitHub user revoked and their follows lost.
	RevokeUser(ctx context.Context, userID int64) error
	PutFollow(ctx context.Context, follow Follow) error
	DeleteFollow(ctx context.Context, ownerID string, repositoryID int64) error
	Follows(ctx context.Context, ownerID string) ([]Follow, error)
	RepositoryFollows(ctx context.Context, repositoryID int64) ([]Follow, error)
	// LoseInstallation marks follows lost for the installation, or only its listed repositories.
	LoseInstallation(ctx context.Context, installationID int64, repositoryIDs []int64) error
	// SeenDelivery and RecordDelivery deduplicate webhook deliveries by GUID.
	SeenDelivery(ctx context.Context, id string) (bool, error)
	RecordDelivery(ctx context.Context, id, event string, now int64) error
	// RequestRefresh coalesces refresh intent per pull request by advancing its
	// generation. A nonempty retention run makes the next submission fresh.
	RequestRefresh(ctx context.Context, ref PullRequestRef, retentionRun string, now int64) error
	// KnownOpen lists pull request numbers last observed open.
	KnownOpen(ctx context.Context, repositoryID int64) ([]int, error)

	// ClaimRefresh leases the oldest pending, available pull request until
	// leaseUntil, fencing earlier leases. ErrNotFound means none is pending.
	ClaimRefresh(ctx context.Context, token string, now, leaseUntil int64) (Lease, error)
	// RecordLease stores what the lease will publish. ErrLeaseLost when fenced.
	RecordLease(ctx context.Context, lease Lease, leased []byte, now int64) error
	// Leased returns the recorded lease for a current token. ErrLeaseLost when fenced or expired.
	Leased(ctx context.Context, repositoryID int64, number int, token string, now int64) (Lease, []byte, error)
	// ReleaseRefresh ends a failed lease; the pull request stays pending until retryAt.
	ReleaseRefresh(ctx context.Context, lease Lease, retryAt int64, detail string) error
	// CompleteRefresh atomically admits every submission, marks the leased
	// generation refreshed and records the observed state. A generation
	// requested during the lease stays pending.
	CompleteRefresh(ctx context.Context, lease Lease, state string, admissions []Admission, now int64) error
}

var ErrLeaseLost = errors.New("fetch lease lost")

// Lease is one claimed refresh of a pull request at a generation.
type Lease struct {
	Ref          PullRequestRef
	Token        string
	Generation   int64
	RetentionRun string
	Attempt      int
}

// Admission is one account's submission of a fetched snapshot.
type Admission struct {
	OwnerID string
	Request ingestion.Request
}
