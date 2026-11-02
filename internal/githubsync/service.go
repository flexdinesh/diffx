// Package githubsync owns the GitHub integration's application rules: which
// accounts may follow which repositories, webhook deduplication, coalesced
// refresh intent and reconciliation. It holds no transport or SQL.
package githubsync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
)

// GitHub is the adapter subset this service needs.
type GitHub interface {
	AppSlug(ctx context.Context) (string, error)
	VerifyWebhook(body []byte, signature256 string) bool
	AuthorizeURL(state, redirectURI string) string
	Exchange(ctx context.Context, code, redirectURI string) (github.UserToken, error)
	Refresh(ctx context.Context, refreshToken string) (github.UserToken, error)
	User(ctx context.Context, accessToken string) (github.Account, error)
	UserRepositories(ctx context.Context, accessToken string) ([]github.Repository, error)
	OpenPullRequests(ctx context.Context, installationID int64, fullName string) ([]github.PullRequest, error)
	PullRequest(ctx context.Context, installationID int64, fullName string, number int) (github.PullRequest, error)
	InstallationToken(ctx context.Context, installationID int64, repositoryIDs []int64, permissions map[string]string) (github.Token, error)
	Repository(ctx context.Context, installationID int64, fullName string) (github.Repository, error)
}

var (
	ErrForbidden    = errors.New("GitHub repository is not accessible to this account")
	ErrNotConnected = errors.New("GitHub is not connected")
	ErrReconnect    = errors.New("GitHub authorization expired or was revoked; connect again")
	ErrSignature    = errors.New("invalid webhook signature")
)

// verificationAge bounds how long a follow's verified access is trusted before
// fan-out rechecks it; it matches GitHub's user access token lifetime.
const verificationAge = 8 * time.Hour

const stateLifetime = 10 * time.Minute

type Options struct {
	PublicURL string
	TokenKey  []byte
	// PublicInstallationID reads public repositories for previews; zero
	// makes public previews unavailable.
	PublicInstallationID int64
	Now                  func() time.Time
}

type Service struct {
	store    Store
	github   GitHub
	sealer   sealer
	callback string
	public   int64
	now      func() time.Time
	// Refresh tokens rotate on use, so each account refreshes serially.
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
	slugMu  sync.Mutex
	slug    string
}

func New(store Store, client GitHub, options Options) (*Service, error) {
	sealer, err := newSealer(options.TokenKey)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, github: client, sealer: sealer, callback: strings.TrimSuffix(options.PublicURL, "/") + "/api/v2/github/callback", public: options.PublicInstallationID, now: now, locks: make(map[string]*sync.Mutex)}, nil
}

func (s *Service) millis() int64 { return s.now().UnixMilli() }

func (s *Service) lock(ownerID string) func() {
	s.locksMu.Lock()
	mu, ok := s.locks[ownerID]
	if !ok {
		mu = &sync.Mutex{}
		s.locks[ownerID] = mu
	}
	s.locksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func stateHash(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Connect starts authorization bound to the account; only that account's
// session can complete it.
func (s *Service) Connect(ctx context.Context, ownerID string) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.store.PutOAuthState(ctx, stateHash(state), ownerID, s.now().Add(stateLifetime).UnixMilli()); err != nil {
		return "", err
	}
	return s.github.AuthorizeURL(state, s.callback), nil
}

// Complete exchanges the callback code. A state issued to another account is refused.
func (s *Service) Complete(ctx context.Context, ownerID, state, code string) error {
	issued, err := s.store.TakeOAuthState(ctx, stateHash(state), s.millis())
	if errors.Is(err, ErrNotFound) || err == nil && issued != ownerID {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	token, err := s.github.Exchange(ctx, code, s.callback)
	if err != nil {
		return err
	}
	user, err := s.github.User(ctx, token.AccessToken)
	if err != nil {
		return err
	}
	defer s.lock(ownerID)()
	return s.saveTokens(ctx, Connection{OwnerID: ownerID, UserID: user.ID, Login: user.Login, Status: "active", ConnectedAt: s.millis()}, token)
}

func (s *Service) saveTokens(ctx context.Context, connection Connection, token github.UserToken) error {
	var err error
	if connection.AccessToken, err = s.sealer.seal(connection.OwnerID, "access", token.AccessToken); err != nil {
		return err
	}
	if connection.RefreshToken, err = s.sealer.seal(connection.OwnerID, "refresh", token.RefreshToken); err != nil {
		return err
	}
	connection.AccessExpiresAt, connection.RefreshExpiresAt = token.ExpiresAt.UnixMilli(), token.RefreshExpiresAt.UnixMilli()
	if token.ExpiresAt.IsZero() {
		connection.AccessExpiresAt = 0
	}
	if token.RefreshExpiresAt.IsZero() {
		connection.RefreshExpiresAt = 0
	}
	return s.store.PutConnection(ctx, connection)
}

// accessToken returns a usable user token, refreshing it a minute before expiry.
// A rejected refresh expires the connection and loses its follows.
func (s *Service) accessToken(ctx context.Context, ownerID string) (string, error) {
	connection, err := s.store.Connection(ctx, ownerID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", err
	}
	if connection.Status != "active" {
		return "", ErrReconnect
	}
	if connection.AccessExpiresAt == 0 || connection.AccessExpiresAt > s.now().Add(time.Minute).UnixMilli() {
		return s.sealer.open(ownerID, "access", connection.AccessToken)
	}
	refresh, err := s.sealer.open(ownerID, "refresh", connection.RefreshToken)
	if err != nil {
		return "", err
	}
	token, err := s.github.Refresh(ctx, refresh)
	if errors.Is(err, github.ErrUnauthorized) {
		connection.Status = "expired"
		if err := s.store.PutConnection(ctx, connection); err != nil {
			return "", err
		}
		return "", ErrReconnect
	}
	if err != nil {
		return "", err
	}
	if err := s.saveTokens(ctx, connection, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// Status reports the account's connection and the app installation page.
type Status struct {
	Connected  bool   `json:"connected"`
	Login      string `json:"login,omitempty"`
	State      string `json:"state,omitempty"`
	InstallURL string `json:"installUrl"`
}

func (s *Service) Status(ctx context.Context, ownerID string) (Status, error) {
	install, err := s.installURL(ctx)
	if err != nil {
		return Status{}, err
	}
	connection, err := s.store.Connection(ctx, ownerID)
	if errors.Is(err, ErrNotFound) {
		return Status{InstallURL: install}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Connected: true, Login: connection.Login, State: connection.Status, InstallURL: install}, nil
}

func (s *Service) installURL(ctx context.Context) (string, error) {
	s.slugMu.Lock()
	defer s.slugMu.Unlock()
	if s.slug == "" {
		slug, err := s.github.AppSlug(ctx)
		if err != nil {
			return "", err
		}
		s.slug = slug
	}
	return "https://github.com/apps/" + s.slug + "/installations/new", nil
}

func (s *Service) Disconnect(ctx context.Context, ownerID string) error {
	defer s.lock(ownerID)()
	return s.store.DeleteConnection(ctx, ownerID)
}

// Repository is a repository the account can follow or already follows.
type Repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"fullName"`
	Private  bool   `json:"private"`
	Followed bool   `json:"followed"`
	// Lost marks a followed repository the account can no longer read.
	Lost bool `json:"lost"`
	// Notify reports whether new pull request diffs send push notifications:
	// followed and not muted.
	Notify bool `json:"notify"`
}

// Repositories lists what both the GitHub user and the app can read, plus
// followed repositories that lost access.
func (s *Service) Repositories(ctx context.Context, ownerID string) ([]Repository, error) {
	accessible, err := s.verify(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	followed := make(map[int64]Follow, len(follows))
	for _, follow := range follows {
		followed[follow.RepositoryID] = follow
	}
	result := make([]Repository, 0, len(accessible)+len(follows))
	seen := make(map[int64]bool, len(accessible))
	for _, repository := range accessible {
		seen[repository.ID] = true
		follow, ok := followed[repository.ID]
		result = append(result, Repository{ID: repository.ID, FullName: repository.FullName, Private: repository.Private, Followed: ok, Notify: ok && !follow.Muted})
	}
	for _, follow := range follows {
		if !seen[follow.RepositoryID] {
			result = append(result, Repository{ID: follow.RepositoryID, FullName: follow.FullName, Followed: true, Lost: true, Notify: !follow.Muted})
		}
	}
	slices.SortFunc(result, func(a, b Repository) int { return strings.Compare(a.FullName, b.FullName) })
	return result, nil
}

// verify lists the account's accessible repositories and updates every follow's
// access accordingly. It is the only path that marks a follow active.
func (s *Service) verify(ctx context.Context, ownerID string) ([]github.Repository, error) {
	defer s.lock(ownerID)()
	token, err := s.accessToken(ctx, ownerID)
	if errors.Is(err, ErrReconnect) {
		return nil, errors.Join(err, s.loseFollows(ctx, ownerID))
	}
	if err != nil {
		return nil, err
	}
	accessible, err := s.github.UserRepositories(ctx, token)
	if errors.Is(err, github.ErrUnauthorized) {
		connection, lookup := s.store.Connection(ctx, ownerID)
		if lookup != nil {
			return nil, lookup
		}
		connection.Status = "expired"
		if err := s.store.PutConnection(ctx, connection); err != nil {
			return nil, err
		}
		return nil, errors.Join(ErrReconnect, s.loseFollows(ctx, ownerID))
	}
	if err != nil {
		return nil, err
	}
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]github.Repository, len(accessible))
	for _, repository := range accessible {
		byID[repository.ID] = repository
	}
	now := s.millis()
	for _, follow := range follows {
		if repository, ok := byID[follow.RepositoryID]; ok {
			follow.Status, follow.VerifiedAt, follow.InstallationID, follow.FullName = "active", now, repository.InstallationID, repository.FullName
		} else {
			follow.Status = "lost"
		}
		if err := s.store.PutFollow(ctx, follow); err != nil {
			return nil, err
		}
	}
	return accessible, nil
}

func (s *Service) loseFollows(ctx context.Context, ownerID string) error {
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return err
	}
	for _, follow := range follows {
		follow.Status = "lost"
		if err := s.store.PutFollow(ctx, follow); err != nil {
			return err
		}
	}
	return nil
}

// Follow subscribes the account after verifying access, then requests a
// refresh of every open pull request so existing work appears immediately.
func (s *Service) Follow(ctx context.Context, ownerID string, repositoryID int64) error {
	accessible, err := s.verify(ctx, ownerID)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(accessible, func(r github.Repository) bool { return r.ID == repositoryID })
	if index < 0 {
		return ErrForbidden
	}
	repository := accessible[index]
	now := s.millis()
	if err := s.store.PutFollow(ctx, Follow{OwnerID: ownerID, RepositoryID: repository.ID, InstallationID: repository.InstallationID, FullName: repository.FullName, Status: "active", VerifiedAt: now, CreatedAt: now}); err != nil {
		return err
	}
	return s.reconcileRepository(ctx, Follow{RepositoryID: repository.ID, InstallationID: repository.InstallationID, FullName: repository.FullName}, "")
}

func (s *Service) Unfollow(ctx context.Context, ownerID string, repositoryID int64) error {
	return s.store.DeleteFollow(ctx, ownerID, repositoryID)
}

// SetNotify changes whether a followed repository's new pull request diffs
// send push notifications to the account. ErrNotFound when not followed.
func (s *Service) SetNotify(ctx context.Context, ownerID string, repositoryID int64, notify bool) error {
	return s.store.SetMuted(ctx, ownerID, repositoryID, !notify)
}

// Resync rechecks the account's access and refreshes every followed
// repository's pull requests. Its run identity makes each resulting submission
// fresh, extending retention of pull requests idle since their last change.
func (s *Service) Resync(ctx context.Context, ownerID string) error {
	if _, err := s.verify(ctx, ownerID); err != nil {
		return err
	}
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return err
	}
	run, err := randomToken()
	if err != nil {
		return err
	}
	var failures []error
	for _, follow := range follows {
		if follow.Status == "active" {
			failures = append(failures, s.reconcileRepository(ctx, follow, run))
		}
	}
	return errors.Join(failures...)
}

// Reconcile recovers deliveries GitHub never retries: it rechecks every
// connected account's access, then requests refreshes for every open or
// previously open pull request of each actively followed repository.
func (s *Service) Reconcile(ctx context.Context) error {
	connections, err := s.store.Connections(ctx)
	if err != nil {
		return err
	}
	var failures []error
	repositories := make(map[int64]Follow)
	for _, connection := range connections {
		if _, err := s.verify(ctx, connection.OwnerID); err != nil && !errors.Is(err, ErrReconnect) {
			failures = append(failures, fmt.Errorf("verify GitHub access for account %s: %w", connection.OwnerID, err))
			continue
		}
		follows, err := s.store.Follows(ctx, connection.OwnerID)
		if err != nil {
			return err
		}
		for _, follow := range follows {
			if follow.Status == "active" {
				repositories[follow.RepositoryID] = follow
			}
		}
	}
	for _, follow := range repositories {
		failures = append(failures, s.reconcileRepository(ctx, follow, ""))
	}
	return errors.Join(failures...)
}

// reconcileRepository requests refreshes for listed open pull requests and for
// known-open ones missing from the list, which closed without a delivery.
func (s *Service) reconcileRepository(ctx context.Context, follow Follow, run string) error {
	open, err := s.github.OpenPullRequests(ctx, follow.InstallationID, follow.FullName)
	if err != nil {
		return fmt.Errorf("list open pull requests of %s: %w", follow.FullName, err)
	}
	known, err := s.store.KnownOpen(ctx, follow.RepositoryID)
	if err != nil {
		return err
	}
	numbers := make([]int, 0, len(open)+len(known))
	for _, pull := range open {
		numbers = append(numbers, pull.Number)
	}
	for _, number := range known {
		if !slices.Contains(numbers, number) {
			numbers = append(numbers, number)
		}
	}
	now := s.millis()
	for _, number := range numbers {
		ref := PullRequestRef{RepositoryID: follow.RepositoryID, InstallationID: follow.InstallationID, FullName: follow.FullName, Number: number}
		if err := s.store.RequestRefresh(ctx, ref, run, false, now); err != nil {
			return err
		}
	}
	return nil
}

// pullRequestActions change a pull request's diff, metadata or state.
var pullRequestActions = []string{"opened", "reopened", "synchronize", "edited", "closed", "ready_for_review", "converted_to_draft"}

// announcingActions present a pull request for review. Only they make its
// first snapshot notify followers; first follows and reconciliation import
// existing pull requests silently.
var announcingActions = []string{"opened", "reopened", "ready_for_review"}

// Webhook verifies and applies one delivery. Duplicate delivery IDs are
// acknowledged without effect; a delivery is recorded only after it applies,
// so a failed one can be redelivered. Every effect is idempotent.
func (s *Service) Webhook(ctx context.Context, event, delivery, signature string, body []byte) error {
	if !s.github.VerifyWebhook(body, signature) {
		return ErrSignature
	}
	if delivery == "" || len(delivery) > 128 {
		return errors.New("webhook delivery ID required")
	}
	seen, err := s.store.SeenDelivery(ctx, delivery)
	if err != nil || seen {
		return err
	}
	parsed, err := github.ParseEvent(event, body)
	if err != nil {
		return err
	}
	if err := s.apply(ctx, parsed); err != nil {
		return err
	}
	return s.store.RecordDelivery(ctx, delivery, event, s.millis())
}

func (s *Service) apply(ctx context.Context, event any) error {
	switch event := event.(type) {
	case github.PullRequestEvent:
		if !slices.Contains(pullRequestActions, event.Action) {
			return nil
		}
		follows, err := s.store.RepositoryFollows(ctx, event.Repository.ID)
		if err != nil || !hasActive(follows) {
			return err
		}
		ref := PullRequestRef{RepositoryID: event.Repository.ID, InstallationID: event.InstallationID, FullName: event.Repository.FullName, Number: event.Number}
		return s.store.RequestRefresh(ctx, ref, "", slices.Contains(announcingActions, event.Action), s.millis())
	case github.InstallationEvent:
		if event.Action == "deleted" || event.Action == "suspend" {
			return s.store.LoseInstallation(ctx, event.InstallationID, nil)
		}
	case github.InstallationRepositoriesEvent:
		if len(event.Removed) > 0 {
			ids := make([]int64, 0, len(event.Removed))
			for _, repository := range event.Removed {
				ids = append(ids, repository.ID)
			}
			return s.store.LoseInstallation(ctx, event.InstallationID, ids)
		}
	case github.AuthorizationEvent:
		if event.Action == "revoked" {
			return s.store.RevokeUser(ctx, event.UserID)
		}
	}
	return nil
}

// Followers returns accounts whose access to the repository is verified within
// verificationAge, rechecking stale follows first. Accounts that lost access
// are excluded; their follows stay lost until verified again.
func (s *Service) Followers(ctx context.Context, repositoryID int64) ([]string, error) {
	follows, err := s.followers(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	owners := make([]string, 0, len(follows))
	for _, follow := range follows {
		owners = append(owners, follow.OwnerID)
	}
	return owners, nil
}

func (s *Service) followers(ctx context.Context, repositoryID int64) ([]Follow, error) {
	follows, err := s.store.RepositoryFollows(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	stale := s.now().Add(-verificationAge).UnixMilli()
	admitted := make([]Follow, 0, len(follows))
	for _, follow := range follows {
		if follow.Status != "active" {
			continue
		}
		if follow.VerifiedAt <= stale {
			accessible, err := s.verify(ctx, follow.OwnerID)
			if errors.Is(err, ErrReconnect) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !slices.ContainsFunc(accessible, func(r github.Repository) bool { return r.ID == repositoryID }) {
				continue
			}
		}
		admitted = append(admitted, follow)
	}
	return admitted, nil
}
