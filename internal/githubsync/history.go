package githubsync

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"
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

// HistoryTotal counts a range's pull requests of one repository in one
// current state, and how many of them opened and merged in the range.
type HistoryTotal struct {
	FullName     string
	State        string
	Draft        bool
	PullRequests int
	Opened       int
	Merged       int
}

// HistoryPage is one page of a range's pull requests, Unix ms. Empty filters
// match every pull request; Limit is the page size.
type HistoryPage struct {
	Since, Until int64
	Repo         string
	Activity     string
	State        string
	After        *HistoryCursor
	Limit        int
}

// HistoryCursor is the last pull request of a page in its newest-event-first
// order. Event keeps storage's microsecond precision so ties stay exact.
type HistoryCursor struct {
	Event        time.Time
	RepositoryID int64
	Number       int
}

const (
	// historyBackfill is how far back following or Resync records closed
	// pull requests, so the longest History range has data at once.
	historyBackfill = 30 * 24 * time.Hour
	// maxHistoryRange bounds one History read: a leap year.
	maxHistoryRange = 366 * 24 * time.Hour
	// DefaultHistoryLimit is a History page's size when the reader names none.
	DefaultHistoryLimit = 200
	// maxHistoryLimit bounds a History page.
	maxHistoryLimit = 500
	// maxHistoryBins bounds a HistoryOverview's bins.
	maxHistoryBins = 120
	// maxOverviewItems bounds the pull requests one HistoryOverview reads.
	maxOverviewItems = 20_000
	// maxStillOpen bounds a HistoryOverview's listed open pull requests.
	maxStillOpen = 50
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
	RetainedSince int64 `json:"retainedSince"`
	Since         int64 `json:"since"`
	Until         int64 `json:"until"`
	// Opened, Merged, Repositories and States cover the whole range,
	// whatever the filters.
	Opened       int                 `json:"opened"`
	Merged       int                 `json:"merged"`
	Repositories []HistoryRepository `json:"repositories"`
	States       HistoryStates       `json:"states"`
	// PullRequests is one filtered page; NextCursor continues it, nil on the
	// last page.
	PullRequests []HistoryPullRequest `json:"pullRequests"`
	NextCursor   *string              `json:"nextCursor"`
}

// HistoryFilter selects one page of History. Empty strings match every pull
// request; Cursor is a previous page's NextCursor.
type HistoryFilter struct {
	Cursor string
	// Limit is the page size, 1 to 500.
	Limit int
	// Repo is a repository full name.
	Repo string
	// Activity is opened or merged in the range.
	Activity string
	// State is the current state: open (not a draft), draft (open), closed
	// (unmerged) or merged.
	State string
}

// HistoryRepository counts one repository's pull requests in the range.
type HistoryRepository struct {
	FullName string `json:"fullName"`
	Opened   int    `json:"opened"`
	Merged   int    `json:"merged"`
}

// HistoryStates counts the range's pull requests by current state.
type HistoryStates struct {
	Open   int `json:"open"`
	Draft  int `json:"draft"`
	Closed int `json:"closed"`
	Merged int `json:"merged"`
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

// HistoryOverview aggregates the account's History over bins.
type HistoryOverview struct {
	Connected     bool   `json:"connected"`
	Login         string `json:"login,omitempty"`
	Following     bool   `json:"following"`
	RetainedSince int64  `json:"retainedSince"`
	Since         int64  `json:"since"`
	Until         int64  `json:"until"`
	Opened        int    `json:"opened"`
	Merged        int    `json:"merged"`
	// OpenNow counts pull requests opened in the range and still open.
	OpenNow int `json:"openNow"`
	// MedianToMerge is the median ms from opening to merging of pull requests
	// merged in the range, nil when none.
	MedianToMerge *int64                      `json:"medianToMerge"`
	Bins          []HistoryBin                `json:"bins"`
	Repositories  []HistoryOverviewRepository `json:"repositories"`
	// StillOpen lists, oldest first and at most 50, OpenNow's pull requests.
	StillOpen []HistoryOpenPullRequest `json:"stillOpen"`
	// Truncated reports more pull requests than one overview reads;
	// aggregates cover only the newest.
	Truncated bool `json:"truncated"`
}

// HistoryBin counts the pull requests opened and merged in [Since, the next
// edge).
type HistoryBin struct {
	Since  int64 `json:"since"`
	Opened int   `json:"opened"`
	Merged int   `json:"merged"`
}

// HistoryOverviewRepository aggregates one repository; Merges counts its
// merges per bin.
type HistoryOverviewRepository struct {
	FullName      string `json:"fullName"`
	Opened        int    `json:"opened"`
	Merged        int    `json:"merged"`
	OpenNow       int    `json:"openNow"`
	MedianToMerge *int64 `json:"medianToMerge"`
	Merges        []int  `json:"merges"`
}

// HistoryOpenPullRequest is a pull request opened in the range and still open.
type HistoryOpenPullRequest struct {
	RepositoryID int64  `json:"repositoryId"`
	FullName     string `json:"fullName"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Draft        bool   `json:"draft"`
	CreatedAt    int64  `json:"createdAt"`
}

// historyScope is what one history read covers: retained history, the range
// start clamped to it, and the reader's connection and follows.
type historyScope struct {
	retained, since      int64
	connected, following bool
	login                string
}

// historyScope refuses a range ending before retained history. Without a
// connection it reports connected false.
func (s *Service) historyScope(ctx context.Context, ownerID string, since, until int64) (historyScope, error) {
	days, err := s.store.HistoryRetentionDays(ctx)
	if err != nil {
		return historyScope{}, err
	}
	retained := s.now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	if until <= retained {
		return historyScope{}, ErrInvalidRange
	}
	scope := historyScope{retained: retained, since: max(since, retained)}
	connection, err := s.store.Connection(ctx, ownerID)
	if errors.Is(err, ErrNotFound) {
		return scope, nil
	}
	if err != nil {
		return historyScope{}, err
	}
	follows, err := s.store.Follows(ctx, ownerID)
	if err != nil {
		return historyScope{}, err
	}
	scope.connected, scope.login, scope.following = true, connection.Login, hasActive(follows)
	return scope, nil
}

// invalidSpan reports a range that is empty, reversed or longer than
// maxHistoryRange, including one whose length overflows.
func invalidSpan(since, until int64) bool {
	span := until - since
	return since >= until || span <= 0 || span > maxHistoryRange.Milliseconds()
}

// History reads one page of the account's pull request history in [since,
// until), Unix ms, with totals over the whole range. The range starts no
// earlier than retained history and may span at most 366 days; one ending
// before retained history is invalid, as are filters outside HistoryFilter's.
func (s *Service) History(ctx context.Context, ownerID string, since, until int64, filter HistoryFilter) (History, error) {
	if invalidSpan(since, until) || filter.Limit < 1 || filter.Limit > maxHistoryLimit ||
		!slices.Contains([]string{"", "opened", "merged"}, filter.Activity) ||
		!slices.Contains([]string{"", "open", "draft", "closed", "merged"}, filter.State) {
		return History{}, ErrInvalidRange
	}
	var after *HistoryCursor
	if filter.Cursor != "" {
		cursor, err := decodeHistoryCursor(filter.Cursor)
		// A page's events fall in its range.
		if err != nil || cursor.Event.UnixMilli() < since || cursor.Event.UnixMilli() >= until {
			return History{}, ErrInvalidRange
		}
		after = &cursor
	}
	scope, err := s.historyScope(ctx, ownerID, since, until)
	if err != nil {
		return History{}, err
	}
	since = scope.since
	result := History{Connected: scope.connected, Login: scope.login, Following: scope.following, RetainedSince: scope.retained,
		Since: since, Until: until, Repositories: []HistoryRepository{}, PullRequests: []HistoryPullRequest{}}
	if !scope.connected {
		return result, nil
	}
	totals, err := s.store.PullRequestHistoryTotals(ctx, ownerID, since, until)
	if err != nil {
		return History{}, err
	}
	repositories := map[string]*HistoryRepository{}
	for _, total := range totals {
		repository := repositories[total.FullName]
		if repository == nil {
			repository = &HistoryRepository{FullName: total.FullName}
			repositories[total.FullName] = repository
		}
		repository.Opened += total.Opened
		repository.Merged += total.Merged
		result.Opened += total.Opened
		result.Merged += total.Merged
		switch {
		case total.State == "merged":
			result.States.Merged += total.PullRequests
		case total.State == "closed":
			result.States.Closed += total.PullRequests
		case total.Draft:
			result.States.Draft += total.PullRequests
		default:
			result.States.Open += total.PullRequests
		}
	}
	for _, repository := range repositories {
		result.Repositories = append(result.Repositories, *repository)
	}
	slices.SortFunc(result.Repositories, func(a, b HistoryRepository) int {
		return mostActive(a.FullName, a.Opened+a.Merged, b.FullName, b.Opened+b.Merged)
	})
	entries, next, err := s.store.PullRequestHistoryPage(ctx, ownerID, HistoryPage{Since: since, Until: until, Repo: filter.Repo,
		Activity: filter.Activity, State: filter.State, After: after, Limit: filter.Limit})
	if err != nil {
		return History{}, err
	}
	for _, entry := range entries {
		result.PullRequests = append(result.PullRequests, HistoryPullRequest{RepositoryID: entry.RepositoryID, FullName: entry.FullName,
			Number: entry.Number, Title: entry.Title, URL: entry.URL, Draft: entry.Draft, State: entry.State,
			CreatedAt: entry.CreatedAt.UnixMilli(), MergedAt: millis(entry.MergedAt), ClosedAt: millis(entry.ClosedAt),
			Opened: within(&entry.CreatedAt, since, until), Merged: within(entry.MergedAt, since, until)})
	}
	if next != nil {
		cursor := encodeHistoryCursor(*next)
		result.NextCursor = &cursor
	}
	return result, nil
}

// HistoryOverview aggregates the account's pull request history over the
// bins [edges[i], edges[i+1]), Unix ms. Edges are 2 to 121 strictly
// increasing instants spanning at most 366 days; counting starts no earlier
// than retained history, and edges ending before it are invalid.
func (s *Service) HistoryOverview(ctx context.Context, ownerID string, edges []int64) (HistoryOverview, error) {
	if len(edges) < 2 || len(edges) > maxHistoryBins+1 {
		return HistoryOverview{}, ErrInvalidRange
	}
	for i := 1; i < len(edges); i++ {
		if edges[i] <= edges[i-1] {
			return HistoryOverview{}, ErrInvalidRange
		}
	}
	until := edges[len(edges)-1]
	if invalidSpan(edges[0], until) {
		return HistoryOverview{}, ErrInvalidRange
	}
	scope, err := s.historyScope(ctx, ownerID, edges[0], until)
	if err != nil {
		return HistoryOverview{}, err
	}
	since := scope.since
	result := HistoryOverview{Connected: scope.connected, Login: scope.login, Following: scope.following, RetainedSince: scope.retained,
		Since: since, Until: until, Bins: make([]HistoryBin, len(edges)-1), Repositories: []HistoryOverviewRepository{},
		StillOpen: []HistoryOpenPullRequest{}}
	for i := range result.Bins {
		result.Bins[i].Since = edges[i]
	}
	if !scope.connected {
		return result, nil
	}
	entries, err := s.store.PullRequestHistory(ctx, ownerID, since, until, maxOverviewItems+1)
	if err != nil {
		return HistoryOverview{}, err
	}
	if len(entries) > maxOverviewItems {
		entries, result.Truncated = entries[:maxOverviewItems], true
	}
	// bin is the index of the bin holding an instant within [since, until).
	bin := func(at int64) int {
		return sort.Search(len(edges), func(i int) bool { return edges[i] > at }) - 1
	}
	type repositoryAggregate struct {
		HistoryOverviewRepository
		toMerge []int64
	}
	repositories := map[string]*repositoryAggregate{}
	toMerge := []int64{}
	stillOpen := []HistoryEntry{}
	for _, entry := range entries {
		repository := repositories[entry.FullName]
		if repository == nil {
			repository = &repositoryAggregate{HistoryOverviewRepository: HistoryOverviewRepository{FullName: entry.FullName, Merges: make([]int, len(result.Bins))}}
			repositories[entry.FullName] = repository
		}
		if within(&entry.CreatedAt, since, until) {
			result.Opened++
			repository.Opened++
			result.Bins[bin(entry.CreatedAt.UnixMilli())].Opened++
			if entry.State == "open" {
				result.OpenNow++
				repository.OpenNow++
				stillOpen = append(stillOpen, entry)
			}
		}
		if within(entry.MergedAt, since, until) {
			result.Merged++
			repository.Merged++
			index := bin(entry.MergedAt.UnixMilli())
			result.Bins[index].Merged++
			repository.Merges[index]++
			duration := max(entry.MergedAt.UnixMilli()-entry.CreatedAt.UnixMilli(), 0)
			toMerge = append(toMerge, duration)
			repository.toMerge = append(repository.toMerge, duration)
		}
	}
	result.MedianToMerge = median(toMerge)
	for _, repository := range repositories {
		repository.MedianToMerge = median(repository.toMerge)
		result.Repositories = append(result.Repositories, repository.HistoryOverviewRepository)
	}
	slices.SortFunc(result.Repositories, func(a, b HistoryOverviewRepository) int {
		return mostActive(a.FullName, a.Opened+a.Merged, b.FullName, b.Opened+b.Merged)
	})
	slices.SortFunc(stillOpen, func(a, b HistoryEntry) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.RepositoryID, b.RepositoryID), cmp.Compare(a.Number, b.Number))
	})
	for _, entry := range stillOpen[:min(len(stillOpen), maxStillOpen)] {
		result.StillOpen = append(result.StillOpen, HistoryOpenPullRequest{RepositoryID: entry.RepositoryID, FullName: entry.FullName,
			Number: entry.Number, Title: entry.Title, URL: entry.URL, Draft: entry.Draft, CreatedAt: entry.CreatedAt.UnixMilli()})
	}
	return result, nil
}

// mostActive orders repositories by opened plus merged, descending, then name.
func mostActive(aName string, aCount int, bName string, bCount int) int {
	return cmp.Or(cmp.Compare(bCount, aCount), strings.Compare(aName, bName))
}

// within reports an instant in [since, until), Unix ms.
func within(at *time.Time, since, until int64) bool {
	return at != nil && at.UnixMilli() >= since && at.UnixMilli() < until
}

// median is the middle value, or the mean of the two middle values rounded
// down; nil without values.
func median(values []int64) *int64 {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	middle := values[len(values)/2]
	if len(values)%2 == 0 {
		lower := values[len(values)/2-1]
		middle = lower + (middle-lower)/2
	}
	return &middle
}

// historyCursorSize is a cursor's bytes: event Unix µs, repository id and
// number, big-endian.
const historyCursorSize = 8 + 8 + 4

func encodeHistoryCursor(cursor HistoryCursor) string {
	data := make([]byte, 0, historyCursorSize)
	data = binary.BigEndian.AppendUint64(data, uint64(cursor.Event.UnixMicro()))
	data = binary.BigEndian.AppendUint64(data, uint64(cursor.RepositoryID))
	data = binary.BigEndian.AppendUint32(data, uint32(cursor.Number))
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeHistoryCursor(value string) (HistoryCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) != historyCursorSize {
		return HistoryCursor{}, ErrInvalidRange
	}
	return HistoryCursor{Event: time.UnixMicro(int64(binary.BigEndian.Uint64(data))), RepositoryID: int64(binary.BigEndian.Uint64(data[8:])),
		Number: int(int32(binary.BigEndian.Uint32(data[16:])))}, nil
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
