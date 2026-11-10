package githubsync_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
)

const day = 24 * time.Hour

// authored is a pull request by the GitHub account id, opened at created and,
// when merged is nonzero, merged then.
func authored(number int, author int64, created, merged time.Time) github.PullRequest {
	pull := github.PullRequest{Number: number, State: "open", Title: "Change", HTMLURL: "https://github.com/octo/app/pull/" + strconv.Itoa(number),
		Author: github.Account{ID: author, Login: "user"}, CreatedAt: created, UpdatedAt: created}
	if !merged.IsZero() {
		pull.State, pull.Merged, pull.MergedAt, pull.ClosedAt, pull.UpdatedAt = "closed", true, &merged, &merged, merged
	}
	return pull
}

func (f *fixture) history(t *testing.T, owner string, since, until time.Time) githubsync.History {
	t.Helper()
	history, err := f.service.History(t.Context(), owner, since.UnixMilli(), until.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func numbers(history githubsync.History) []int {
	result := []int{}
	for _, pull := range history.PullRequests {
		result = append(result, pull.Number)
	}
	return result
}

func TestHistoryBackfillsAndCountsOnlyTheReadersPullRequests(t *testing.T) {
	f := newFixture(t)
	now := *f.clock
	f.github.open = map[string][]github.PullRequest{"octo/app": {authored(1, 100, now.Add(-2*day), time.Time{})}}
	f.github.closed = map[string][]github.PullRequest{"octo/app": {
		authored(2, 100, now.Add(-9*day), now.Add(-3*day)),   // opened before the week, merged in it
		authored(3, 100, now.Add(-5*day), now.Add(-4*day)),   // opened and merged in the week
		authored(4, 200, now.Add(-2*day), now.Add(-1*day)),   // bob's
		authored(5, 100, now.Add(-40*day), now.Add(-35*day)), // older than the backfill
	}}
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.github.closedSince) != 1 || !f.github.closedSince[0].Equal(now.Add(-30*day)) {
		t.Fatalf("backfill cutoff %v, want 30 days", f.github.closedSince)
	}
	week := f.history(t, f.alice, now.Add(-7*day), now)
	if !week.Connected || week.Login != "alice-gh" || !week.Following {
		t.Fatalf("history identity %+v", week)
	}
	if got := numbers(week); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("pull requests %v, want alice's newest event first", got)
	}
	if week.Opened != 2 || week.Merged != 2 {
		t.Fatalf("opened %d merged %d, want 2 and 2", week.Opened, week.Merged)
	}
	if len(week.Repositories) != 1 || week.Repositories[0] != (githubsync.HistoryRepository{FullName: "octo/app", Opened: 2, Merged: 2}) {
		t.Fatalf("repositories %+v", week.Repositories)
	}
	if merged := week.PullRequests[1]; merged.Opened || !merged.Merged || merged.MergedAt == nil {
		t.Fatalf("pull request opened before the range %+v", merged)
	}
	// Bob follows the same repository but sees only his own pull request.
	f.connect(t, f.bob, "bob")
	if err := f.service.Follow(t.Context(), f.bob, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	if got := numbers(f.history(t, f.bob, now.Add(-7*day), now)); len(got) != 1 || got[0] != 4 {
		t.Fatalf("bob's history %v", got)
	}
	// Unfollowing hides the repository's history.
	if err := f.service.Unfollow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	if hidden := f.history(t, f.alice, now.Add(-7*day), now); hidden.Following || len(hidden.PullRequests) != 0 {
		t.Fatalf("unfollowed history %+v", hidden)
	}
}

func TestHistoryLeaseRecordsMergeAndIgnoresOlderReads(t *testing.T) {
	f := newFixture(t)
	now := *f.clock
	open := authored(1, 100, now.Add(-2*day), time.Time{})
	f.github.open = map[string][]github.PullRequest{"octo/app": {open}}
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	merged := pullRequest(headOne)
	merged.Author, merged.CreatedAt = github.Account{ID: 100, Login: "alice-gh"}, open.CreatedAt
	mergedAt := now.Add(-time.Hour)
	merged.State, merged.Merged, merged.MergedAt, merged.ClosedAt, merged.UpdatedAt = "closed", true, &mergedAt, &mergedAt, mergedAt
	f.github.pulls = map[int]github.PullRequest{1: merged}
	f.lease(t)
	history := f.history(t, f.alice, now.Add(-day), now)
	if len(history.PullRequests) != 1 || history.PullRequests[0].State != "merged" || history.Merged != 1 || history.Opened != 0 {
		t.Fatalf("lease did not record the merge: %+v", history)
	}
	// A later read of the older open state never replaces the merge.
	if err := f.store.GitHub().PutPullRequestHistory(t.Context(), githubsync.HistoryEntry{RepositoryID: appRepo.ID, FullName: "octo/app", Number: 1,
		AuthorID: 100, State: "open", CreatedAt: open.CreatedAt, UpdatedAt: open.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if again := f.history(t, f.alice, now.Add(-day), now); len(again.PullRequests) != 1 || again.PullRequests[0].State != "merged" {
		t.Fatalf("older read replaced the merge: %+v", again)
	}
}

func TestHistoryRangeAndRetention(t *testing.T) {
	f := newFixture(t)
	now := *f.clock
	unconnected := f.history(t, f.alice, now.Add(-day), now)
	if unconnected.Connected || unconnected.Following || unconnected.PullRequests == nil || unconnected.Repositories == nil {
		t.Fatalf("unconnected history %+v", unconnected)
	}
	retained := now.Add(-30 * day).UnixMilli()
	if unconnected.RetainedSince != retained {
		t.Fatalf("retained since %d, want the 30-day default %d", unconnected.RetainedSince, retained)
	}
	for name, window := range map[string][2]time.Time{
		"empty":           {now, now},
		"reversed":        {now, now.Add(-day)},
		"longer than 31d": {now.Add(-32 * day), now},
		"before retained": {now.Add(-40 * day), now.Add(-31 * day)},
	} {
		if _, err := f.service.History(t.Context(), f.alice, window[0].UnixMilli(), window[1].UnixMilli()); !errors.Is(err, githubsync.ErrInvalidRange) {
			t.Errorf("%s range: %v", name, err)
		}
	}
	partial := f.history(t, f.alice, now.Add(-31*day), now.Add(-24*day))
	if partial.Since != retained {
		t.Fatalf("partly retained range starts %d, want %d", partial.Since, retained)
	}
}

func TestHistoryPrunesPastRetention(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	old, recent := now.Add(-31*day), now.Add(-29*day)
	for number, at := range map[int]time.Time{1: old, 2: recent} {
		if err := f.store.GitHub().PutPullRequestHistory(t.Context(), githubsync.HistoryEntry{RepositoryID: appRepo.ID, FullName: "octo/app", Number: number,
			AuthorID: 100, State: "merged", CreatedAt: at.Add(-day), MergedAt: &at, ClosedAt: &at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.PruneExpired(now); err != nil {
		t.Fatal(err)
	}
	f.connect(t, f.alice, "alice")
	f.github.open = nil
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	kept, err := f.store.GitHub().PullRequestHistory(t.Context(), f.alice, now.Add(-60*day).UnixMilli(), now.UnixMilli(), 10)
	if err != nil || len(kept) != 1 || kept[0].Number != 2 {
		t.Fatalf("history after prune %+v %v, want only the retained pull request", kept, err)
	}
}
