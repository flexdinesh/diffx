package githubsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
)

// HistoryEntry is one pull request's durable history: repository facts
// written whenever the integration reads the pull request from GitHub.
type HistoryEntry struct {
	RepositoryID int64
	FullName     string
	Number       int
	Title        string
	URL          string
	AuthorID     int64
	AuthorLogin  string
	Draft        bool
	State        string // open, closed or merged
	CreatedAt    time.Time
	MergedAt     *time.Time
	ClosedAt     *time.Time
	UpdatedAt    time.Time
}

const (
	// historyBackfill is how far back following or Resync records closed
	// pull requests, so the longest History range has data at once.
	historyBackfill = 30 * 24 * time.Hour
	// maxHistoryRange bounds one History read: the 30-day range plus a
	// daylight saving shift.
	maxHistoryRange = 31 * 24 * time.Hour
	// maxHistoryItems bounds one History read's pull requests.
	maxHistoryItems = 1000
)

var ErrInvalidRange = errors.New("history range is invalid")

// History is the account's GitHub user's pull requests opened or merged in a
// range, within its actively followed repositories.
type History struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	// Following reports any active follow.
	Following bool `json:"following"`
	// RetainedSince is the oldest instant history is kept for, in Unix ms.
	RetainedSince int64                `json:"retainedSince"`
	Since         int64                `json:"since"`
	Until         int64                `json:"until"`
	Opened        int                  `json:"opened"`
	Merged        int                  `json:"merged"`
	Repositories  []HistoryRepository  `json:"repositories"`
	PullRequests  []HistoryPullRequest `json:"pullRequests"`
	// Truncated reports more pull requests than one read returns; counts
	// cover only those returned.
	Truncated bool `json:"truncated"`
}

// HistoryRepository counts one repository's pull requests in the range.
type HistoryRepository struct {
	FullName string `json:"fullName"`
	Opened   int    `json:"opened"`
	Merged   int    `json:"merged"`
}

// HistoryPullRequest is one pull request in the range. Opened and Merged
// report which of its events fall in the range.
type HistoryPullRequest struct {
	RepositoryID int64  `json:"repositoryId"`
	FullName     string `json:"fullName"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Draft        bool   `json:"draft"`
	State        string `json:"state"`
	CreatedAt    int64  `json:"createdAt"`
	MergedAt     *int64 `json:"mergedAt"`
	ClosedAt     *int64 `json:"closedAt"`
	Opened       bool   `json:"opened"`
	Merged       bool   `json:"merged"`
}

// History reads the account's pull request history in [since, until), Unix
// ms. The range starts no earlier than retained history and may span at most
// 31 days; one ending before retained history is invalid.
func (s *Service) History(ctx context.Context, ownerID string, since, until int64) (History, error) {
	if since >= until || time.Duration(until-since)*time.Millisecond > maxHistoryRange {
		return History{}, ErrInvalidRange
	}
	days, err := s.store.HistoryRetentionDays(ctx)
	if err != nil {
		return History{}, err
	}
	retained := s.now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	if until <= retained {
		return History{}, ErrInvalidRange
	}
	since = max(since, retained)
	result := History{RetainedSince: retained, Since: since, Until: until, Repositories: []HistoryRepository{}, PullRequests: []HistoryPullRequest{}}
	connection, err := s.store.Connection(ctx, ownerID)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return History{}, err
	}
	result.Connected, result.Login = true, connection.Login
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return History{}, err
	}
	result.Following = hasActive(follows)
	entries, err := s.store.PullRequestHistory(ctx, ownerID, since, until, maxHistoryItems+1)
	if err != nil {
		return History{}, err
	}
	if len(entries) > maxHistoryItems {
		entries, result.Truncated = entries[:maxHistoryItems], true
	}
	repositories := map[string]*HistoryRepository{}
	within := func(at *time.Time) bool {
		return at != nil && at.UnixMilli() >= since && at.UnixMilli() < until
	}
	for _, entry := range entries {
		item := HistoryPullRequest{RepositoryID: entry.RepositoryID, FullName: entry.FullName, Number: entry.Number, Title: entry.Title,
			URL: entry.URL, Draft: entry.Draft, State: entry.State, CreatedAt: entry.CreatedAt.UnixMilli(),
			MergedAt: millis(entry.MergedAt), ClosedAt: millis(entry.ClosedAt),
			Opened: within(&entry.CreatedAt), Merged: within(entry.MergedAt)}
		result.PullRequests = append(result.PullRequests, item)
		repository := repositories[entry.FullName]
		if repository == nil {
			repository = &HistoryRepository{FullName: entry.FullName}
			repositories[entry.FullName] = repository
		}
		if item.Opened {
			result.Opened++
			repository.Opened++
		}
		if item.Merged {
			result.Merged++
			repository.Merged++
		}
	}
	for _, repository := range repositories {
		result.Repositories = append(result.Repositories, *repository)
	}
	slices.SortFunc(result.Repositories, func(a, b HistoryRepository) int {
		if a.Merged+a.Opened != b.Merged+b.Opened {
			return (b.Merged + b.Opened) - (a.Merged + a.Opened)
		}
		return strings.Compare(a.FullName, b.FullName)
	})
	return result, nil
}

func millis(at *time.Time) *int64 {
	if at == nil {
		return nil
	}
	value := at.UnixMilli()
	return &value
}

func historyEntry(ref PullRequestRef, pull github.PullRequest) HistoryEntry {
	return HistoryEntry{RepositoryID: ref.RepositoryID, FullName: ref.FullName, Number: pull.Number, Title: pull.Title, URL: pull.HTMLURL,
		AuthorID: pull.Author.ID, AuthorLogin: pull.Author.Login, Draft: pull.Draft, State: pullState(pull),
		CreatedAt: pull.CreatedAt, MergedAt: pull.MergedAt, ClosedAt: pull.ClosedAt, UpdatedAt: pull.UpdatedAt}
}

// recordHistory writes what a read of the repository's pull requests showed.
func (s *Service) recordHistory(ctx context.Context, follow Follow, pulls []github.PullRequest) error {
	for _, pull := range pulls {
		ref := PullRequestRef{RepositoryID: follow.RepositoryID, InstallationID: follow.InstallationID, FullName: follow.FullName, Number: pull.Number}
		if err := s.store.PutPullRequestHistory(ctx, historyEntry(ref, pull)); err != nil {
			return err
		}
	}
	return nil
}

// backfillHistory records the repository's pull requests closed recently, so
// History covers them without a fetch.
func (s *Service) backfillHistory(ctx context.Context, follow Follow) error {
	closed, err := s.github.ClosedPullRequests(ctx, follow.InstallationID, follow.FullName, s.now().Add(-historyBackfill))
	if err != nil && !notFound(err) {
		return fmt.Errorf("list closed pull requests of %s: %w", follow.FullName, err)
	}
	return s.recordHistory(ctx, follow, closed)
}
