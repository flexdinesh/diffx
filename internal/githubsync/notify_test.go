package githubsync_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
)

const headThree = "4444444444444444444444444444444444444444"

type notified struct {
	owner, title, body, job string
}

// notifications lists recorded push intent, oldest first.
func (f *fixture) notifications(t *testing.T) []notified {
	t.Helper()
	due, err := f.store.Push().DueNotifications(t.Context(), time.Now().Add(72*time.Hour).UnixMilli(), 100)
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
