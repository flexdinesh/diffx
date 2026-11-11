package githubsync_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
)

const headThree = "4444444444444444444444444444444444444444"

type notified struct {
	owner, title, body, job string
}

// notifications lists recorded push intent, oldest first. Its claim lapses
// at once, so every later listing sees the same notifications.
func (f *fixture) notifications(t *testing.T) []notified {
	t.Helper()
	at := time.Now().Add(72 * time.Hour).UnixMilli()
	due, err := f.store.Push().ClaimNotifications(t.Context(), at, at, 100)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]notified, 0, len(due))
	for _, notification := range due {
		var content struct{ Title, Body string }
		if err := json.Unmarshal(notification.Content, &content); err != nil {
			t.Fatal(err)
		}
		if notification.Kind != "pull_request" {
			t.Fatalf("kind %q", notification.Kind)
		}
		result = append(result, notified{notification.OwnerID, content.Title, content.Body, notification.JobState})
	}
	return result
}

// prefer saves the owner's preferences, changed from the defaults.
func (f *fixture) prefer(t *testing.T, owner string, change func(*githubsync.Preferences)) {
	t.Helper()
	preferences := githubsync.DefaultPreferences()
	change(&preferences)
	if _, err := f.service.SetNotificationPreferences(t.Context(), owner, preferences); err != nil {
		t.Fatal(err)
	}
}

// notifyNewCommits enables new commit notifications for others' pull requests.
func notifyNewCommits(preferences *githubsync.Preferences) { preferences.NewCommits.ByOthers = true }

func TestExistingPullRequestsImportSilently(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	f.publish(t)
	if got := f.notifications(t); len(got) != 0 {
		t.Fatalf("first follow notified: %+v", got)
	}
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.publish(t)
	if err := f.service.Resync(t.Context(), f.alice); err != nil {
		t.Fatal(err)
	}
	f.publish(t)
	edited := pullRequest(headOne)
	edited.Title, edited.UpdatedAt = "Add fuzzy search", edited.UpdatedAt.Add(time.Hour)
	f.github.pulls[1] = edited
	f.webhook(t, "edited", "edited")
	f.publish(t)
	closed := edited
	closed.State, closed.Merged = "closed", true
	f.github.pulls[1] = closed
	f.webhook(t, "merged", "closed")
	f.publish(t)
	if deliveries := f.admitted(t); len(deliveries) < 3 {
		t.Fatalf("resync, edit and merge must still publish: %d", len(deliveries))
	}
	if got := f.notifications(t); len(got) != 0 {
		t.Fatalf("reconcile, resync, edit or merge notified: %+v", got)
	}
}

func TestOpenedPullRequestNotifiesFollowersThatDidNotMute(t *testing.T) {
	f := newFixture(t)
	f.github.open = map[string][]github.PullRequest{}
	f.github.pulls = map[int]github.PullRequest{}
	f.connect(t, f.alice, "alice")
	f.connect(t, f.bob, "bob")
	for _, owner := range []string{f.alice, f.bob} {
		if err := f.service.Follow(t.Context(), owner, appRepo.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.SetNotify(t.Context(), f.bob, appRepo.ID, false); err != nil {
		t.Fatal(err)
	}
	for owner, want := range map[string]bool{f.alice: true, f.bob: false} {
		repositories, err := f.service.Repositories(t.Context(), owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, repository := range repositories {
			if repository.Notify != (want && repository.ID == appRepo.ID) {
				t.Fatalf("listing notify for %s: %+v", owner, repositories)
			}
		}
	}
	f.github.pulls[1] = pullRequest(headOne)
	f.webhook(t, "opened", "opened")
	f.publish(t)
	got := f.notifications(t)
	if len(got) != 1 || got[0].owner != f.alice || got[0].title != "octo/app #1" || got[0].body != "Add search\nNew PR by @mona" || got[0].job != "queued" {
		t.Fatalf("notifications %+v", got)
	}
	if deliveries := f.admitted(t); len(deliveries) != 2 {
		t.Fatalf("muting must not stop snapshots: %d admitted", len(deliveries))
	}
}

func TestNewHeadNotifiesFollowersWithVerifiedAccess(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice, f.bob)
	f.prefer(t, f.alice, notifyNewCommits)
	f.prefer(t, f.bob, notifyNewCommits)
	f.publish(t)
	f.github.pulls[1] = pullRequest(headTwo)
	f.webhook(t, "pushed", "synchronize")
	f.publish(t)
	got := f.notifications(t)
	if len(got) != 2 || got[0].body != "Add search\nUpdated by @mona" || got[0].owner == got[1].owner {
		t.Fatalf("force push notifications %+v", got)
	}
	// Bob loses access; his follow stops receiving anything.
	f.github.repositories["access-bob"] = nil
	*f.clock = f.clock.Add(9 * time.Hour)
	f.github.pulls[1] = pullRequest(headThree)
	f.webhook(t, "pushed-again", "synchronize")
	f.publish(t)
	got = f.notifications(t)
	if len(got) != 3 || got[2].owner != f.alice {
		t.Fatalf("lost follower notified: %+v", got)
	}
}

func TestReconcileAfterDowntimeNotifiesChangedHeads(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	f.prefer(t, f.alice, notifyNewCommits)
	f.publish(t)
	// A push GitHub never delivered while the server was down.
	f.github.pulls[1] = pullRequest(headTwo)
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.publish(t)
	if got := f.notifications(t); len(got) != 1 || got[0].owner != f.alice || got[0].body != "Add search\nUpdated by @mona" {
		t.Fatalf("missed push not notified: %+v", got)
	}
}

func TestNotificationTitleIsBounded(t *testing.T) {
	f := newFixture(t)
	f.github.open = map[string][]github.PullRequest{}
	f.github.pulls = map[int]github.PullRequest{}
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	long := pullRequest(headOne)
	long.Title = ""
	for range 30 {
		long.Title += "überlong "
	}
	f.github.pulls[1] = long
	f.webhook(t, "ready", "ready_for_review")
	f.publish(t)
	got := f.notifications(t)
	if len(got) != 1 {
		t.Fatalf("ready for review did not notify: %+v", got)
	}
	title, _, _ := strings.Cut(got[0].body, "\n")
	if runes := []rune(title); len(runes) != 120 || runes[119] != '…' {
		t.Fatalf("title not truncated to 120 characters: %q", title)
	}
}

func TestAnnouncementClassifiesOneEventPerSnapshot(t *testing.T) {
	ref := githubsync.PullRequestRef{RepositoryID: appRepo.ID, InstallationID: 5, FullName: "octo/app", Number: 1}
	open := pullRequest(headOne)
	moved := pullRequest(headTwo)
	draft := pullRequest(headOne)
	draft.Draft = true
	movedDraft := pullRequest(headTwo)
	movedDraft.Draft = true
	closed := pullRequest(headOne)
	closed.State = "closed"
	closedMoved := pullRequest(headTwo)
	closedMoved.State = "closed"
	merged := closed
	merged.Merged, merged.MergedBy = true, &github.Account{ID: 9, Login: "octocat"}
	mergedMoved := closedMoved
	mergedMoved.Merged = true
	mergedUnknown := closed
	mergedUnknown.Merged = true

	recorded := githubsync.Observation{State: "open", Head: headOne}
	recordedDraft := githubsync.Observation{State: "open", Head: headOne, Draft: true}
	recordedClosed := githubsync.Observation{State: "closed", Head: headOne}
	recordedMerged := githubsync.Observation{State: "merged", Head: headOne}
	for _, test := range []struct {
		name     string
		previous githubsync.Observation
		opened   bool
		pull     github.PullRequest
		event    githubsync.Event
		line     string
	}{
		{"silent import", githubsync.Observation{}, false, open, githubsync.EventNone, ""},
		{"silent import after refresh without snapshot", githubsync.Observation{State: "closed"}, false, open, githubsync.EventNone, ""},
		{"announced first snapshot", githubsync.Observation{}, true, open, githubsync.EventOpened, "New PR by @mona"},
		{"announced draft first snapshot", githubsync.Observation{}, true, draft, githubsync.EventOpened, "New PR by @mona"},
		{"announced but closed first snapshot", githubsync.Observation{}, true, closed, githubsync.EventNone, ""},
		{"metadata edit", recorded, true, open, githubsync.EventNone, ""},
		{"head change", recorded, false, moved, githubsync.EventNewCommits, "Updated by @mona"},
		{"draft head change", recordedDraft, false, movedDraft, githubsync.EventNewCommits, "Updated by @mona"},
		{"converted to draft", recorded, false, draft, githubsync.EventNone, ""},
		{"ready for review", recordedDraft, false, open, githubsync.EventOpened, "Ready for review: PR by @mona"},
		{"ready for review outranks head change", recordedDraft, false, moved, githubsync.EventOpened, "Ready for review: PR by @mona"},
		{"reopened", recordedClosed, false, open, githubsync.EventOpened, "Reopened: PR by @mona"},
		{"reopened outranks head change", recordedClosed, false, moved, githubsync.EventOpened, "Reopened: PR by @mona"},
		{"head change while closed", recordedClosed, false, closedMoved, githubsync.EventNone, ""},
		{"still closed", recordedClosed, false, closed, githubsync.EventNone, ""},
		{"merged", recorded, false, merged, githubsync.EventMerged, "Merged by @octocat"},
		{"merged without merger", recorded, false, mergedUnknown, githubsync.EventMerged, "Merged: PR by @mona"},
		{"merge outranks head change", recordedDraft, false, mergedMoved, githubsync.EventMerged, "Merged: PR by @mona"},
		{"still merged", recordedMerged, false, merged, githubsync.EventNone, ""},
		{"closed without merging", recorded, false, closed, githubsync.EventClosed, "Closed without merging: PR by @mona"},
		{"close outranks head change", recorded, false, closedMoved, githubsync.EventClosed, "Closed without merging: PR by @mona"},
	} {
		t.Run(test.name, func(t *testing.T) {
			event, notification := githubsync.Announcement(ref, test.previous, test.opened, test.pull)
			if event != test.event {
				t.Fatalf("event %v, want %v", event, test.event)
			}
			if test.event == githubsync.EventNone {
				if notification != nil {
					t.Fatalf("notification without event: %+v", notification)
				}
				return
			}
			if notification == nil || notification.Title != "octo/app #1" || notification.Body != "Add search\n"+test.line {
				t.Fatalf("notification %+v, want line %q", notification, test.line)
			}
		})
	}
}

func TestPreferencesFilterEvents(t *testing.T) {
	const viewer = 100
	others := pullRequest(headOne)
	others.Author = github.Account{ID: 7, Login: "Mona"}
	mine := pullRequest(headOne)
	mine.Author = github.Account{ID: viewer, Login: "alice-gh"}
	unknownAuthor := pullRequest(headOne)
	draft := others
	draft.Draft = true
	into := func(base string) github.PullRequest {
		pull := others
		pull.Base.Ref = base
		return pull
	}
	titled := others
	titled.Title = "WIP: Add search"
	all := githubsync.Audience{ByMe: true, ByOthers: true}
	everything := githubsync.Preferences{Opened: all, NewCommits: all, Merged: all, Closed: all}
	with := func(change func(*githubsync.Preferences)) githubsync.Preferences {
		preferences := everything
		change(&preferences)
		return preferences
	}
	defaults := githubsync.DefaultPreferences()
	for _, test := range []struct {
		name        string
		preferences githubsync.Preferences
		event       githubsync.Event
		pull        github.PullRequest
		viewer      int64
		want        bool
	}{
		{"defaults: others' opened", defaults, githubsync.EventOpened, others, viewer, true},
		{"defaults: my opened", defaults, githubsync.EventOpened, mine, viewer, true},
		{"defaults: new commits", defaults, githubsync.EventNewCommits, others, viewer, false},
		{"defaults: merged", defaults, githubsync.EventMerged, mine, viewer, false},
		{"defaults: closed", defaults, githubsync.EventClosed, others, viewer, false},
		{"no event", everything, githubsync.EventNone, others, viewer, false},

		{"opened by me only: mine", with(func(p *githubsync.Preferences) { p.Opened.ByOthers = false }), githubsync.EventOpened, mine, viewer, true},
		{"opened by me only: others'", with(func(p *githubsync.Preferences) { p.Opened.ByOthers = false }), githubsync.EventOpened, others, viewer, false},
		{"new commits by others only: mine", with(func(p *githubsync.Preferences) { p.NewCommits.ByMe = false }), githubsync.EventNewCommits, mine, viewer, false},
		{"new commits by others only: others'", with(func(p *githubsync.Preferences) { p.NewCommits.ByMe = false }), githubsync.EventNewCommits, others, viewer, true},
		{"merged by me only: mine", with(func(p *githubsync.Preferences) { p.Merged.ByOthers = false }), githubsync.EventMerged, mine, viewer, true},
		{"merged by me only: others'", with(func(p *githubsync.Preferences) { p.Merged.ByOthers = false }), githubsync.EventMerged, others, viewer, false},
		{"closed off: mine", with(func(p *githubsync.Preferences) { p.Closed = githubsync.Audience{} }), githubsync.EventClosed, mine, viewer, false},
		{"closed off: others'", with(func(p *githubsync.Preferences) { p.Closed = githubsync.Audience{} }), githubsync.EventClosed, others, viewer, false},
		{"closed setting leaves merged alone", with(func(p *githubsync.Preferences) { p.Closed = githubsync.Audience{} }), githubsync.EventMerged, others, viewer, true},

		{"unknown viewer owns nothing", with(func(p *githubsync.Preferences) { p.Opened.ByOthers = false }), githubsync.EventOpened, unknownAuthor, 0, false},
		{"unknown viewer sees others'", with(func(p *githubsync.Preferences) { p.Opened.ByMe = false }), githubsync.EventOpened, unknownAuthor, 0, true},

		{"skip drafts: draft", with(func(p *githubsync.Preferences) { p.SkipDrafts = true }), githubsync.EventOpened, draft, viewer, false},
		{"skip drafts: ready", with(func(p *githubsync.Preferences) { p.SkipDrafts = true }), githubsync.EventOpened, others, viewer, true},
		{"drafts kept", everything, githubsync.EventNewCommits, draft, viewer, true},

		{"only authors ignores case", with(func(p *githubsync.Preferences) { p.OnlyAuthors = []string{"hubot", "MONA"} }), githubsync.EventOpened, others, viewer, true},
		{"only authors excludes others", with(func(p *githubsync.Preferences) { p.OnlyAuthors = []string{"hubot"} }), githubsync.EventOpened, others, viewer, false},
		{"only authors leaves mine", with(func(p *githubsync.Preferences) { p.OnlyAuthors = []string{"hubot"} }), githubsync.EventOpened, mine, viewer, true},
		{"never authors ignores case", with(func(p *githubsync.Preferences) { p.NeverAuthors = []string{"mona"} }), githubsync.EventOpened, others, viewer, false},
		{"never authors applies to mine", with(func(p *githubsync.Preferences) { p.NeverAuthors = []string{"ALICE-GH"} }), githubsync.EventOpened, mine, viewer, false},
		{"never authors outranks only authors", with(func(p *githubsync.Preferences) {
			p.OnlyAuthors, p.NeverAuthors = []string{"mona"}, []string{"Mona"}
		}), githubsync.EventOpened, others, viewer, false},

		{"branch exact ignores case", with(func(p *githubsync.Preferences) { p.BaseBranches = []string{"Main"} }), githubsync.EventOpened, into("main"), viewer, true},
		{"branch exact is not a prefix", with(func(p *githubsync.Preferences) { p.BaseBranches = []string{"main"} }), githubsync.EventOpened, into("main-next"), viewer, false},
		{"branch glob matches prefix", with(func(p *githubsync.Preferences) { p.BaseBranches = []string{"main", "Release/*"} }), githubsync.EventOpened, into("release/2.0"), viewer, true},
		{"branch glob needs its prefix", with(func(p *githubsync.Preferences) { p.BaseBranches = []string{"release/*"} }), githubsync.EventOpened, into("releases"), viewer, false},
		{"branch filter applies to mine", with(func(p *githubsync.Preferences) { p.BaseBranches = []string{"release/*"} }), githubsync.EventOpened, mine, viewer, false},

		{"muted title term ignores case", with(func(p *githubsync.Preferences) { p.MutedTitleTerms = []string{"wip"} }), githubsync.EventOpened, titled, viewer, false},
		{"muted title term elsewhere", with(func(p *githubsync.Preferences) { p.MutedTitleTerms = []string{"wip"} }), githubsync.EventOpened, others, viewer, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.preferences.Notifies(test.event, test.pull, test.viewer); got != test.want {
				t.Fatalf("Notifies = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNotificationPreferencesNormalizeAndBound(t *testing.T) {
	f := newFixture(t)
	if got, err := f.service.NotificationPreferences(t.Context(), f.alice); err != nil || !reflect.DeepEqual(got, githubsync.DefaultPreferences()) {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	input := githubsync.DefaultPreferences()
	input.OnlyAuthors = []string{" Mona ", "mona", "", "  ", "hubot", "HUBOT"}
	input.NeverAuthors = nil
	input.BaseBranches = []string{"release/*", "main"}
	input.MutedTitleTerms = []string{"WIP", "wip "}
	stored, err := f.service.SetNotificationPreferences(t.Context(), f.alice, input)
	want := githubsync.DefaultPreferences()
	want.OnlyAuthors, want.BaseBranches, want.MutedTitleTerms = []string{"Mona", "hubot"}, []string{"release/*", "main"}, []string{"WIP"}
	if err != nil || !reflect.DeepEqual(stored, want) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if got, err := f.service.NotificationPreferences(t.Context(), f.alice); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("read back = %+v, %v", got, err)
	}

	// Limits apply after normalization.
	twenty := make([]string, 0, 25)
	for index := range 20 {
		twenty = append(twenty, "author-"+strconv.Itoa(index))
	}
	withDuplicates := append(slices.Clone(twenty), "AUTHOR-0", " ", "author-1 ")
	accepted := githubsync.DefaultPreferences()
	accepted.NeverAuthors = withDuplicates
	if stored, err := f.service.SetNotificationPreferences(t.Context(), f.alice, accepted); err != nil || !reflect.DeepEqual(stored.NeverAuthors, twenty) {
		t.Fatalf("20 entries after normalizing = %+v, %v", stored.NeverAuthors, err)
	}
	for name, change := range map[string]func(*githubsync.Preferences){
		"21 entries":       func(p *githubsync.Preferences) { p.OnlyAuthors = append(slices.Clone(twenty), "author-20") },
		"101 characters":   func(p *githubsync.Preferences) { p.BaseBranches = []string{strings.Repeat("b", 101)} },
		"101 runes":        func(p *githubsync.Preferences) { p.MutedTitleTerms = []string{strings.Repeat("ü", 101)} },
		"long after a dup": func(p *githubsync.Preferences) { p.NeverAuthors = []string{"x", strings.Repeat("y", 101)} },
	} {
		refused := githubsync.DefaultPreferences()
		change(&refused)
		if _, err := f.service.SetNotificationPreferences(t.Context(), f.alice, refused); !errors.Is(err, githubsync.ErrInvalidPreferences) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	hundred := githubsync.DefaultPreferences()
	hundred.MutedTitleTerms = []string{strings.Repeat("ü", 100)}
	if _, err := f.service.SetNotificationPreferences(t.Context(), f.alice, hundred); err != nil {
		t.Fatalf("100 characters: %v", err)
	}
}

// TestPullRequestLifecycleNotifiesByPreferences follows one pull request
// authored by alice's GitHub user through every event: alice wants her own
// pull requests' events, bob keeps the defaults but skips drafts.
func TestPullRequestLifecycleNotifiesByPreferences(t *testing.T) {
	f := newFixture(t)
	f.github.open = map[string][]github.PullRequest{}
	f.github.pulls = map[int]github.PullRequest{}
	f.connect(t, f.alice, "alice")
	f.connect(t, f.bob, "bob")
	for _, owner := range []string{f.alice, f.bob} {
		if err := f.service.Follow(t.Context(), owner, appRepo.ID); err != nil {
			t.Fatal(err)
		}
	}
	mine := githubsync.Audience{ByMe: true}
	f.prefer(t, f.alice, func(p *githubsync.Preferences) {
		p.Opened, p.NewCommits, p.Merged, p.Closed = mine, mine, mine, mine
	})
	f.prefer(t, f.bob, func(p *githubsync.Preferences) { p.SkipDrafts = true })

	// Listings are unordered, so each step diffs counts of what was notified.
	seen := map[notified]int{}
	step := func(delivery, action string, pull github.PullRequest, want map[string]string) {
		t.Helper()
		f.github.pulls[1] = pull
		f.webhook(t, delivery, action)
		f.publish(t)
		counts := map[notified]int{}
		got := map[string]string{}
		for _, notification := range f.notifications(t) {
			notification.job = ""
			if counts[notification]++; counts[notification] > seen[notification] {
				got[notification.owner] = notification.body
			}
		}
		seen = counts
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s notified %+v, want %+v", delivery, got, want)
		}
	}
	authored := func(head string) github.PullRequest {
		pull := pullRequest(head)
		pull.Author = github.Account{ID: 100, Login: "alice-gh"}
		return pull
	}
	draft := authored(headOne)
	draft.Draft = true
	step("opened", "opened", draft, map[string]string{f.alice: "Add search\nNew PR by @alice-gh"})
	step("ready", "ready_for_review", authored(headOne), map[string]string{
		f.alice: "Add search\nReady for review: PR by @alice-gh", f.bob: "Add search\nReady for review: PR by @alice-gh"})
	step("pushed", "synchronize", authored(headTwo), map[string]string{f.alice: "Add search\nUpdated by @alice-gh"})
	closed := authored(headTwo)
	closed.State = "closed"
	step("closed", "closed", closed, map[string]string{f.alice: "Add search\nClosed without merging: PR by @alice-gh"})
	step("reopened", "reopened", authored(headTwo), map[string]string{
		f.alice: "Add search\nReopened: PR by @alice-gh", f.bob: "Add search\nReopened: PR by @alice-gh"})
	merged := authored(headTwo)
	merged.State, merged.Merged, merged.MergedBy = "closed", true, &github.Account{ID: 9, Login: "octocat"}
	step("merged", "closed", merged, map[string]string{f.alice: "Add search\nMerged by @octocat"})

	// A muted repository notifies nothing, whatever the preferences.
	if err := f.service.SetNotify(t.Context(), f.alice, appRepo.ID, false); err != nil {
		t.Fatal(err)
	}
	step("reopened-again", "reopened", authored(headThree), map[string]string{f.bob: "Add search\nReopened: PR by @alice-gh"})
}
