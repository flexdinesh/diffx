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
	// Muted follows still receive snapshots but no push notifications. Reads
	// report it; PutFollow leaves it unchanged.
	Muted bool
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
	// SetMuted changes whether a follow sends push notifications. ErrNotFound
	// when the account does not follow the repository.
	SetMuted(ctx context.Context, ownerID string, repositoryID int64, muted bool) error
	// RequestRefresh coalesces refresh intent per pull request by advancing its
	// generation. A nonempty retention run makes the next submission fresh.
	// Opened records, in the same transaction, that a webhook announced the
	// pull request, so its first snapshot notifies followers.
	RequestRefresh(ctx context.Context, ref PullRequestRef, retentionRun string, opened bool, now int64) error
	// PullRequestHead returns the last captured head SHA ("" before the first
	// snapshot) and whether a webhook announced the pull request.
	PullRequestHead(ctx context.Context, repositoryID int64, number int) (head string, opened bool, err error)
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
	// CompleteRefresh atomically admits every submission with its notification
	// intent, marks the leased generation refreshed and records the observed
	// state and, when nonempty, the captured head. A generation requested
	// during the lease stays pending. An admission for PublicOwnerID records
	// its job, the fetch time and head as the public snapshot.
	CompleteRefresh(ctx context.Context, lease Lease, state, head string, admissions []Admission, now int64) error

	// PublicPullRequest returns the public preview state by repository ID, and
	// PublicPullRequestNamed by case-insensitive full name. ErrNotFound when
	// the pull request has no refresh row.
	PublicPullRequest(ctx context.Context, repositoryID int64, number int, now int64) (PublicState, error)
	PublicPullRequestNamed(ctx context.Context, fullName string, number int, now int64) (PublicState, error)
	// RequestPublicRefresh advances the generation with public demand and
	// records the pull request read for it. It never replaces an existing
	// row's installation; a new row reads through ref's installation.
	RequestPublicRefresh(ctx context.Context, ref PullRequestRef, pull []byte, retentionRun string, now int64) error
	// RecordPublicCheck records a public read of the pull request that
	// needed no fetch.
	RecordPublicCheck(ctx context.Context, repositoryID int64, number int, pull []byte, now int64) error
	// RetryPublicRefresh makes a pending, unleased refresh available now and
	// clears its failure detail.
	RetryPublicRefresh(ctx context.Context, repositoryID int64, number int, now int64) error
}

// PublicState is a pull request's public preview: its demand, pending
// refresh and last public snapshot.
type PublicState struct {
	Ref PullRequestRef
	// Demanded reports a public request was ever made.
	Demanded bool
	// Pending refreshes wait for or hold a lease; failed attempts back off
	// with a detail.
	Pending bool
	Leased  bool
	Attempt int
	Detail  string
	// Pull is the GitHub pull request JSON last read for a public request.
	Pull []byte
	// Head, FetchedAt and JobState describe the last public snapshot's
	// ingestion; ContextID is set only once it succeeded and while it is
	// unexpired.
	Head      string
	FetchedAt int64
	JobState  string
	ContextID string
	// CheckedAt is when a public request last read GitHub.
	CheckedAt int64
}

var ErrLeaseLost = errors.New("fetch lease lost")

// Lease is one claimed refresh of a pull request at a generation. Public
// reports that the leased generation covers public demand.
type Lease struct {
	Ref          PullRequestRef
	Token        string
	Generation   int64
	RetentionRun string
	Attempt      int
	Public       bool
}

// Admission is one account's submission of a fetched snapshot. A non-nil
// Notification is push intent linked to the admitted ingestion job.
type Admission struct {
	OwnerID      string
	Request      ingestion.Request
	Notification *Notification
}

// Notification is the text of a pull request push notification. Delivery adds
// the link once the follower's snapshot is published.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
