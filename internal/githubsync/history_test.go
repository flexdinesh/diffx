package githubsync_test

import (
	"errors"
	"slices"
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
	history, err := f.service.History(t.Context(), owner, since.UnixMilli(), until.UnixMilli(), githubsync.HistoryFilter{Limit: githubsync.DefaultHistoryLimit})
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
		"empty":            {now, now},
		"reversed":         {now, now.Add(-day)},
		"longer than 366d": {now.Add(-367 * day), now},
		"before retained":  {now.Add(-40 * day), now.Add(-31 * day)},
	} {
		if _, err := f.service.History(t.Context(), f.alice, window[0].UnixMilli(), window[1].UnixMilli(), githubsync.HistoryFilter{Limit: 1}); !errors.Is(err, githubsync.ErrInvalidRange) {
			t.Errorf("%s range: %v", name, err)
		}
	}
	if _, err := f.service.History(t.Context(), f.alice, -1<<63, 1<<62, githubsync.HistoryFilter{Limit: 1}); !errors.Is(err, githubsync.ErrInvalidRange) {
		t.Errorf("overflowing range: %v", err)
	}
	partial := f.history(t, f.alice, now.Add(-31*day), now.Add(-24*day))
	if partial.Since != retained {
		t.Fatalf("partly retained range starts %d, want %d", partial.Since, retained)
	}
	// A year's range is accepted and starts at retained history.
	if year := f.history(t, f.alice, now.Add(-366*day), now); year.Since != retained {
		t.Fatalf("year range starts %d, want %d", year.Since, retained)
	}
	f.retain(t, 365)
	if year := f.history(t, f.alice, now.Add(-366*day), now); year.Since != now.Add(-365*day).UnixMilli() {
		t.Fatalf("year range with a year's retention starts %d", year.Since)
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

var unfollowedRepo = github.Repository{ID: 12, FullName: "octo/unfollowed", InstallationID: 5}

// record writes a pull request into history: authored by the GitHub user,
// in its current state, merged at merged when nonzero.
func (f *fixture) record(t *testing.T, repo github.Repository, number int, author int64, state string, draft bool, created, merged time.Time) {
	t.Helper()
	entry := githubsync.HistoryEntry{RepositoryID: repo.ID, FullName: repo.FullName, Number: number, Title: "Change " + strconv.Itoa(number),
		URL: "https://github.com/" + repo.FullName + "/pull/" + strconv.Itoa(number), AuthorID: author, State: state, Draft: draft,
		CreatedAt: created, UpdatedAt: created}
	if !merged.IsZero() {
		entry.MergedAt, entry.ClosedAt, entry.UpdatedAt = &merged, &merged, merged
	}
	if err := f.store.GitHub().PutPullRequestHistory(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
}

// followAll connects alice and follows her repositories, recording none of
// their pull requests.
func (f *fixture) followAll(t *testing.T) {
	t.Helper()
	f.github.open = nil
	f.connect(t, f.alice, "alice")
	for _, repo := range []github.Repository{appRepo, otherRepo} {
		if err := f.service.Follow(t.Context(), f.alice, repo.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fixture) retain(t *testing.T, days int) {
	t.Helper()
	settings, err := f.store.AppSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings.HistoryRetentionDays = days
	if err := f.store.PutAppSettings(t.Context(), settings, "", *f.clock); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) page(t *testing.T, since, until time.Time, filter githubsync.HistoryFilter) githubsync.History {
	t.Helper()
	if filter.Limit == 0 {
		filter.Limit = githubsync.DefaultHistoryLimit
	}
	history, err := f.service.History(t.Context(), f.alice, since.UnixMilli(), until.UnixMilli(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

// pages follows cursors to the end, returning each page's numbers.
func (f *fixture) pages(t *testing.T, since, until time.Time, filter githubsync.HistoryFilter) [][]int {
	t.Helper()
	result := [][]int{}
	for {
		page := f.page(t, since, until, filter)
		result = append(result, numbers(page))
		if page.NextCursor == nil {
			return result
		}
		if len(result) > 20 {
			t.Fatal("cursor never ended")
		}
		filter.Cursor = *page.NextCursor
	}
}

func (f *fixture) overview(t *testing.T, owner string, edges ...time.Time) githubsync.HistoryOverview {
	t.Helper()
	millis := []int64{}
	for _, edge := range edges {
		millis = append(millis, edge.UnixMilli())
	}
	overview, err := f.service.HistoryOverview(t.Context(), owner, millis)
	if err != nil {
		t.Fatal(err)
	}
	return overview
}

func TestHistoryPagesFilteredPullRequestsWithWholeRangeTotals(t *testing.T) {
	f := newFixture(t)
	now := (*f.clock).Truncate(time.Millisecond)
	f.followAll(t)
	tie := now.Add(-3 * day)
	f.record(t, appRepo, 1, 100, "open", false, now.Add(-day), time.Time{})
	f.record(t, appRepo, 2, 100, "open", true, now.Add(-2*day), time.Time{})
	f.record(t, appRepo, 3, 100, "merged", false, now.Add(-10*day), tie)                    // opened before the week
	f.record(t, appRepo, 4, 100, "closed", false, tie, time.Time{})                         // closed unmerged
	f.record(t, otherRepo, 5, 100, "merged", false, tie, now.Add(-time.Hour))               // opened and merged
	f.record(t, otherRepo, 6, 100, "open", false, tie, time.Time{})                         // ties #3 and #4
	f.record(t, appRepo, 7, 100, "open", false, tie.Add(500*time.Microsecond), time.Time{}) // within #3's millisecond
	f.record(t, appRepo, 8, 200, "open", false, now.Add(-day), time.Time{})                 // bob's
	f.record(t, appRepo, 9, 100, "merged", false, now.Add(-20*day), now.Add(-15*day))       // before the week
	f.record(t, unfollowedRepo, 10, 100, "open", false, now.Add(-day), time.Time{})
	week := now.Add(-7 * day)
	all := f.page(t, week, now, githubsync.HistoryFilter{})
	if got, want := numbers(all), []int{5, 1, 2, 7, 3, 4, 6}; !slices.Equal(got, want) || all.NextCursor != nil {
		t.Fatalf("pull requests %v next %v, want %v on one page", got, all.NextCursor, want)
	}
	if all.Opened != 6 || all.Merged != 2 {
		t.Fatalf("opened %d merged %d, want 6 and 2", all.Opened, all.Merged)
	}
	if want := (githubsync.HistoryStates{Open: 3, Draft: 1, Closed: 1, Merged: 2}); all.States != want {
		t.Fatalf("states %+v, want %+v", all.States, want)
	}
	if want := []githubsync.HistoryRepository{{FullName: "octo/app", Opened: 4, Merged: 1}, {FullName: "octo/secret", Opened: 2, Merged: 1}}; !slices.Equal(all.Repositories, want) {
		t.Fatalf("repositories %+v", all.Repositories)
	}
	// Every page size continues without duplicates or gaps, ties included.
	for limit := 1; limit <= 7; limit++ {
		pages := f.pages(t, week, now, githubsync.HistoryFilter{Limit: limit})
		joined := slices.Concat(pages...)
		if !slices.Equal(joined, numbers(all)) || len(pages[0]) != limit || len(pages) != (7+limit-1)/limit {
			t.Fatalf("limit %d pages %v", limit, pages)
		}
	}
	for _, tc := range []struct {
		filter githubsync.HistoryFilter
		want   []int
	}{
		{githubsync.HistoryFilter{Repo: "octo/secret"}, []int{5, 6}},
		{githubsync.HistoryFilter{Activity: "opened"}, []int{5, 1, 2, 7, 4, 6}},
		{githubsync.HistoryFilter{Activity: "merged"}, []int{5, 3}},
		{githubsync.HistoryFilter{State: "open"}, []int{1, 7, 6}},
		{githubsync.HistoryFilter{State: "draft"}, []int{2}},
		{githubsync.HistoryFilter{State: "closed"}, []int{4}},
		{githubsync.HistoryFilter{State: "merged"}, []int{5, 3}},
		{githubsync.HistoryFilter{Repo: "octo/app", State: "open"}, []int{1, 7}},
		{githubsync.HistoryFilter{Repo: "octo/app", Activity: "merged", State: "merged"}, []int{3}},
		{githubsync.HistoryFilter{Repo: "octo/missing"}, []int{}},
	} {
		page := f.page(t, week, now, tc.filter)
		if got := numbers(page); !slices.Equal(got, tc.want) {
			t.Errorf("filter %+v: %v, want %v", tc.filter, got, tc.want)
		}
		if page.Opened != all.Opened || page.Merged != all.Merged || page.States != all.States || !slices.Equal(page.Repositories, all.Repositories) {
			t.Errorf("filter %+v changed totals: %+v", tc.filter, page)
		}
		tc.filter.Limit = 1
		if got := slices.Concat(f.pages(t, week, now, tc.filter)...); !slices.Equal(got, tc.want) {
			t.Errorf("filter %+v paged: %v, want %v", tc.filter, got, tc.want)
		}
	}
	first := f.page(t, week, now, githubsync.HistoryFilter{Limit: 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no cursor")
	}
	for name, filter := range map[string]githubsync.HistoryFilter{
		"zero limit":       {Limit: 0},
		"limit over 500":   {Limit: 501},
		"unknown activity": {Limit: 1, Activity: "closed"},
		"unknown state":    {Limit: 1, State: "draft-ish"},
		"malformed cursor": {Limit: 1, Cursor: "!!"},
		"short cursor":     {Limit: 1, Cursor: "AAAA"},
		"padded cursor":    {Limit: 1, Cursor: *first.NextCursor + "="},
	} {
		if _, err := f.service.History(t.Context(), f.alice, week.UnixMilli(), now.UnixMilli(), filter); !errors.Is(err, githubsync.ErrInvalidRange) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A cursor names an event in its range.
	if _, err := f.service.History(t.Context(), f.alice, week.UnixMilli(), now.Add(-2*time.Hour).UnixMilli(), githubsync.HistoryFilter{Limit: 1, Cursor: *first.NextCursor}); !errors.Is(err, githubsync.ErrInvalidRange) {
		t.Errorf("cursor outside the range: %v", err)
	}
}

func TestHistoryOverviewBinsAndAggregates(t *testing.T) {
	f := newFixture(t)
	now := (*f.clock).Truncate(time.Millisecond)
	f.followAll(t)
	// Local days across daylight saving changes: 23 and 25 hours.
	e0 := now.Add(-72 * time.Hour)
	e1, e2, e3 := e0.Add(23*time.Hour), e0.Add(48*time.Hour), now
	f.record(t, appRepo, 1, 100, "merged", false, e0.Add(time.Hour), e0.Add(5*time.Hour))
	f.record(t, appRepo, 2, 100, "merged", false, e1.Add(-time.Millisecond), e1) // either side of an edge
	f.record(t, appRepo, 3, 100, "open", false, e1.Add(24*time.Hour), time.Time{})
	f.record(t, appRepo, 4, 100, "merged", false, e0.Add(-48*time.Hour), e2.Add(time.Hour)) // opened before the range
	f.record(t, otherRepo, 5, 100, "open", true, e2.Add(2*time.Hour), time.Time{})
	f.record(t, otherRepo, 6, 100, "merged", false, e0.Add(3*time.Hour), e2.Add(3*time.Hour))
	f.record(t, appRepo, 7, 100, "closed", false, e0.Add(2*time.Hour), time.Time{})
	f.record(t, appRepo, 8, 200, "open", false, e0.Add(time.Hour), time.Time{}) // bob's
	f.record(t, unfollowedRepo, 9, 100, "open", false, e0.Add(time.Hour), time.Time{})
	f.record(t, appRepo, 10, 100, "open", false, e3, time.Time{}) // at the exclusive end
	overview := f.overview(t, f.alice, e0, e1, e2, e3)
	if !overview.Connected || !overview.Following || overview.Login != "alice-gh" || overview.Since != e0.UnixMilli() || overview.Until != e3.UnixMilli() || overview.Truncated {
		t.Fatalf("overview identity %+v", overview)
	}
	if want := []githubsync.HistoryBin{{Since: e0.UnixMilli(), Opened: 4, Merged: 1}, {Since: e1.UnixMilli(), Opened: 1, Merged: 1}, {Since: e2.UnixMilli(), Opened: 1, Merged: 2}}; !slices.Equal(overview.Bins, want) {
		t.Fatalf("bins %+v, want %+v", overview.Bins, want)
	}
	if overview.Opened != 6 || overview.Merged != 4 || overview.OpenNow != 2 {
		t.Fatalf("opened %d merged %d open now %d, want 6, 4 and 2", overview.Opened, overview.Merged, overview.OpenNow)
	}
	// Merges took 1ms, 4h, 48h and 97h: the mean of the middle two.
	if overview.MedianToMerge == nil || *overview.MedianToMerge != (26*time.Hour).Milliseconds() {
		t.Fatalf("median to merge %v, want 26h", overview.MedianToMerge)
	}
	if len(overview.Repositories) != 2 {
		t.Fatalf("repositories %+v", overview.Repositories)
	}
	app, secret := overview.Repositories[0], overview.Repositories[1]
	if app.FullName != "octo/app" || app.Opened != 4 || app.Merged != 3 || app.OpenNow != 1 || !slices.Equal(app.Merges, []int{1, 1, 1}) ||
		app.MedianToMerge == nil || *app.MedianToMerge != (4*time.Hour).Milliseconds() {
		t.Fatalf("app repository %+v", app)
	}
	if secret.FullName != "octo/secret" || secret.Opened != 2 || secret.Merged != 1 || secret.OpenNow != 1 || !slices.Equal(secret.Merges, []int{0, 0, 1}) ||
		secret.MedianToMerge == nil || *secret.MedianToMerge != (48*time.Hour).Milliseconds() {
		t.Fatalf("secret repository %+v", secret)
	}
	if len(overview.StillOpen) != 2 || overview.StillOpen[0].Number != 3 || overview.StillOpen[1].Number != 5 || !overview.StillOpen[1].Draft ||
		overview.StillOpen[1] != (githubsync.HistoryOpenPullRequest{RepositoryID: otherRepo.ID, FullName: "octo/secret", Number: 5, Title: "Change 5",
			URL: "https://github.com/octo/secret/pull/5", Draft: true, CreatedAt: e2.Add(2 * time.Hour).UnixMilli()}) {
		t.Fatalf("still open %+v", overview.StillOpen)
	}
	// Without merges the median is unknown.
	quiet := f.overview(t, f.alice, e1.Add(23*time.Hour), e2)
	if quiet.Opened != 1 || quiet.Merged != 0 || quiet.MedianToMerge != nil || len(quiet.Repositories) != 1 || quiet.Repositories[0].MedianToMerge != nil {
		t.Fatalf("overview without merges %+v", quiet)
	}
	// Two merges, 1ms and 4h: their mean, rounded down.
	two := f.overview(t, f.alice, e0, e2)
	if two.Merged != 2 || two.MedianToMerge == nil || *two.MedianToMerge != (4*time.Hour).Milliseconds()/2 {
		t.Fatalf("two merges' median %v", two.MedianToMerge)
	}
	// An odd count's median is the middle merge.
	three := f.overview(t, f.alice, e0, e2.Add(2*time.Hour))
	if three.Merged != 3 || three.MedianToMerge == nil || *three.MedianToMerge != (4*time.Hour).Milliseconds() {
		t.Fatalf("three merges' median %v", three.MedianToMerge)
	}
	// Bob is not connected: bins are zero and still cover every edge.
	unconnected := f.overview(t, f.bob, e0, e1, e2, e3)
	if unconnected.Connected || len(unconnected.Bins) != 3 || unconnected.Bins[2] != (githubsync.HistoryBin{Since: e2.UnixMilli()}) ||
		unconnected.Repositories == nil || unconnected.StillOpen == nil || unconnected.MedianToMerge != nil || unconnected.Opened != 0 {
		t.Fatalf("unconnected overview %+v", unconnected)
	}
}

func TestHistoryOverviewListsOldestFiftyStillOpen(t *testing.T) {
	f := newFixture(t)
	now := (*f.clock).Truncate(time.Millisecond)
	f.followAll(t)
	start := now.Add(-2 * day)
	for number := 52; number >= 1; number-- {
		f.record(t, appRepo, number, 100, "open", number%2 == 0, start.Add(time.Duration(number)*time.Minute), time.Time{})
	}
	overview := f.overview(t, f.alice, start, now)
	if overview.OpenNow != 52 || len(overview.StillOpen) != 50 {
		t.Fatalf("open now %d still open %d, want 52 and 50", overview.OpenNow, len(overview.StillOpen))
	}
	for i, pull := range overview.StillOpen {
		if pull.Number != i+1 {
			t.Fatalf("still open %d is #%d, want oldest first", i, pull.Number)
		}
	}
}

func TestHistoryOverviewEdgesAndRetention(t *testing.T) {
	f := newFixture(t)
	now := (*f.clock).Truncate(time.Millisecond)
	f.followAll(t)
	retained := now.Add(-30 * day)
	f.record(t, appRepo, 1, 100, "merged", false, now.Add(-38*day), now.Add(-37*day)) // before retention, unpruned
	f.record(t, appRepo, 2, 100, "open", false, now.Add(-32*day), time.Time{})        // in a bin, before retention
	f.record(t, appRepo, 3, 100, "open", false, now.Add(-29*day), time.Time{})
	overview := f.overview(t, f.alice, now.Add(-40*day), now.Add(-35*day), now.Add(-25*day), now)
	if overview.Since != retained.UnixMilli() || overview.RetainedSince != retained.UnixMilli() {
		t.Fatalf("since %d retained %d, want %d", overview.Since, overview.RetainedSince, retained.UnixMilli())
	}
	if want := []githubsync.HistoryBin{{Since: now.Add(-40 * day).UnixMilli()}, {Since: now.Add(-35 * day).UnixMilli(), Opened: 1}, {Since: now.Add(-25 * day).UnixMilli()}}; !slices.Equal(overview.Bins, want) {
		t.Fatalf("bins %+v, want %+v", overview.Bins, want)
	}
	if overview.Opened != 1 || overview.Merged != 0 || len(overview.StillOpen) != 1 || overview.StillOpen[0].Number != 3 {
		t.Fatalf("overview counted history before retention %+v", overview)
	}
	edges := func(count int, step time.Duration) []int64 {
		result := []int64{}
		for i := range count {
			result = append(result, now.Add(-time.Duration(count-1-i)*step).UnixMilli())
		}
		return result
	}
	if bins, err := f.service.HistoryOverview(t.Context(), f.alice, edges(121, time.Hour)); err != nil || len(bins.Bins) != 120 {
		t.Fatalf("121 edges: %d bins %v", len(bins.Bins), err)
	}
	if _, err := f.service.HistoryOverview(t.Context(), f.alice, []int64{now.Add(-366 * day).UnixMilli(), now.UnixMilli()}); err != nil {
		t.Fatalf("366-day span: %v", err)
	}
	for name, invalid := range map[string][]int64{
		"none":            {},
		"one":             {now.UnixMilli()},
		"122":             edges(122, time.Hour),
		"repeated":        {now.Add(-day).UnixMilli(), now.Add(-day).UnixMilli(), now.UnixMilli()},
		"decreasing":      {now.UnixMilli(), now.Add(-day).UnixMilli()},
		"longer than 366": {now.Add(-367 * day).UnixMilli(), now.UnixMilli()},
		"overflowing":     {-1 << 63, 1 << 62},
		"before retained": {now.Add(-40 * day).UnixMilli(), retained.UnixMilli()},
	} {
		if _, err := f.service.HistoryOverview(t.Context(), f.alice, invalid); !errors.Is(err, githubsync.ErrInvalidRange) {
			t.Errorf("%s edges: %v", name, err)
		}
	}
}
