package githubsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/flexdinesh/diffx/internal/github"
)

// Event is what a snapshot announces to the pull request's followers.
type Event int

const (
	// EventNone announces nothing.
	EventNone Event = iota
	// EventOpened: opened, reopened or marked ready for review.
	EventOpened
	// EventNewCommits: the head changed while open.
	EventNewCommits
	EventMerged
	// EventClosed: closed without merging.
	EventClosed
)

// maxNotificationTitle bounds the pull request title shown in a notification.
const maxNotificationTitle = 120

// announcement classifies what a snapshot of pull announces relative to what
// refreshes last recorded, and phrases its notification. A first snapshot
// announces an open pull request only when a webhook announced it, so first
// follows, startup reconciliation and Resync import existing pull requests
// silently. Later snapshots announce, in priority order, a merge or close of
// an open pull request, a reopen, becoming ready for review, then a changed
// head while open. Metadata edits announce nothing.
func announcement(ref PullRequestRef, previous Observation, opened bool, pull github.PullRequest) (Event, *Notification) {
	state, author := pullState(pull), "PR by @"+pull.Author.Login
	var event Event
	var line string
	switch {
	case previous.Head == "":
		if !opened || state != "open" {
			return EventNone, nil
		}
		event, line = EventOpened, "New PR by @"+pull.Author.Login
	case previous.State == "open" && state == "merged":
		event, line = EventMerged, "Merged: "+author
		if pull.MergedBy != nil && pull.MergedBy.Login != "" {
			line = "Merged by @" + pull.MergedBy.Login
		}
	case previous.State == "open" && state == "closed":
		event, line = EventClosed, "Closed without merging: "+author
	case previous.State != "open" && state == "open":
		event, line = EventOpened, "Reopened: "+author
	case previous.State == "open" && previous.Draft && !pull.Draft:
		event, line = EventOpened, "Ready for review: "+author
	case state == "open" && previous.Head != pull.Head.SHA:
		event, line = EventNewCommits, "Updated by @"+pull.Author.Login
	default:
		return EventNone, nil
	}
	title := strings.TrimSpace(pull.Title)
	if runes := []rune(title); len(runes) > maxNotificationTitle {
		title = strings.TrimSpace(string(runes[:maxNotificationTitle-1])) + "…"
	}
	return event, &Notification{Title: ref.FullName + " #" + strconv.Itoa(ref.Number), Body: title + "\n" + line}
}

// Audience chooses whose pull requests an event notifies about.
type Audience struct {
	// ByMe covers pull requests authored by the account's connected GitHub user.
	ByMe     bool `json:"byMe"`
	ByOthers bool `json:"byOthers"`
}

// Preferences are an account's pull request notification filters, applied
// to every followed repository it has not muted. Every condition must hold.
type Preferences struct {
	Opened     Audience `json:"opened"`
	NewCommits Audience `json:"newCommits"`
	Merged     Audience `json:"merged"`
	Closed     Audience `json:"closed"`
	SkipDrafts bool     `json:"skipDrafts"`
	// OnlyAuthors, when nonempty, limits others' pull requests to these
	// GitHub logins; NeverAuthors excludes logins. Both ignore case.
	OnlyAuthors  []string `json:"onlyAuthors"`
	NeverAuthors []string `json:"neverAuthors"`
	// BaseBranches, when nonempty, limits pull requests to these base
	// branches, ignoring case; an entry ending in * matches a prefix.
	BaseBranches []string `json:"baseBranches"`
	// MutedTitleTerms excludes pull requests whose title contains any term,
	// ignoring case.
	MutedTitleTerms []string `json:"mutedTitleTerms"`
}

// DefaultPreferences notify of every opened pull request and nothing else.
func DefaultPreferences() Preferences {
	return Preferences{
		Opened:      Audience{ByMe: true, ByOthers: true},
		OnlyAuthors: []string{}, NeverAuthors: []string{}, BaseBranches: []string{}, MutedTitleTerms: []string{},
	}
}

// Recipient is a follower's notification filter: its connected GitHub user
// and its preferences.
type Recipient struct {
	UserID      int64
	Preferences Preferences
}

// Notifies reports whether event on pull notifies an account whose connected
// GitHub user is viewerUserID (0: unknown, so no pull request is its own).
func (p Preferences) Notifies(event Event, pull github.PullRequest, viewerUserID int64) bool {
	var audience Audience
	switch event {
	case EventOpened:
		audience = p.Opened
	case EventNewCommits:
		audience = p.NewCommits
	case EventMerged:
		audience = p.Merged
	case EventClosed:
		audience = p.Closed
	}
	mine := viewerUserID != 0 && pull.Author.ID == viewerUserID
	if mine && !audience.ByMe || !mine && !audience.ByOthers {
		return false
	}
	if p.SkipDrafts && pull.Draft {
		return false
	}
	login := pull.Author.Login
	equalsLogin := func(entry string) bool { return strings.EqualFold(entry, login) }
	if !mine && len(p.OnlyAuthors) > 0 && !slices.ContainsFunc(p.OnlyAuthors, equalsLogin) {
		return false
	}
	if slices.ContainsFunc(p.NeverAuthors, equalsLogin) {
		return false
	}
	base := strings.ToLower(pull.Base.Ref)
	if len(p.BaseBranches) > 0 && !slices.ContainsFunc(p.BaseBranches, func(entry string) bool {
		entry = strings.ToLower(entry)
		if prefix, ok := strings.CutSuffix(entry, "*"); ok {
			return strings.HasPrefix(base, prefix)
		}
		return base == entry
	}) {
		return false
	}
	title := strings.ToLower(pull.Title)
	return !slices.ContainsFunc(p.MutedTitleTerms, func(term string) bool { return strings.Contains(title, strings.ToLower(term)) })
}

const (
	maxFilterEntries     = 20
	maxFilterEntryLength = 100
)

// ErrInvalidPreferences reports a filter list over its limits.
var ErrInvalidPreferences = errors.New("notification preferences are invalid")

// NotificationPreferences returns the account's preferences, or the defaults
// until it saves any.
func (s *Service) NotificationPreferences(ctx context.Context, ownerID string) (Preferences, error) {
	return s.store.NotificationPreferences(ctx, ownerID)
}

// SetNotificationPreferences replaces the account's preferences and returns
// what was stored. Filter entries are trimmed, blanks dropped and duplicates
// removed ignoring case, keeping the first spelling; a list then over 20
// entries, or an entry over 100 characters, is ErrInvalidPreferences.
func (s *Service) SetNotificationPreferences(ctx context.Context, ownerID string, preferences Preferences) (Preferences, error) {
	for _, list := range []struct {
		name    string
		entries *[]string
	}{
		{"onlyAuthors", &preferences.OnlyAuthors}, {"neverAuthors", &preferences.NeverAuthors},
		{"baseBranches", &preferences.BaseBranches}, {"mutedTitleTerms", &preferences.MutedTitleTerms},
	} {
		normalized, err := normalizeFilter(list.name, *list.entries)
		if err != nil {
			return Preferences{}, err
		}
		*list.entries = normalized
	}
	if err := s.store.PutNotificationPreferences(ctx, ownerID, preferences, s.millis()); err != nil {
		return Preferences{}, err
	}
	return preferences, nil
}

func normalizeFilter(name string, entries []string) ([]string, error) {
	normalized := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" || slices.ContainsFunc(normalized, func(kept string) bool { return strings.EqualFold(kept, entry) }) {
			continue
		}
		if utf8.RuneCountInString(entry) > maxFilterEntryLength {
			return nil, fmt.Errorf("%w: %s entries must be at most %d characters", ErrInvalidPreferences, name, maxFilterEntryLength)
		}
		normalized = append(normalized, entry)
	}
	if len(normalized) > maxFilterEntries {
		return nil, fmt.Errorf("%w: %s must have at most %d entries", ErrInvalidPreferences, name, maxFilterEntries)
	}
	return normalized, nil
}
