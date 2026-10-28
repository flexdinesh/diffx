package reviewstore

import (
	"errors"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/pushservice"
)

func remoteAccount(t *testing.T, store *Store, name string) string {
	t.Helper()
	account := identityservice.Account{ID: "id-" + name, Username: name}
	if err := store.CreateAccount(t.Context(), account, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return account.ID
}

// leasedPullRequest requests and leases a refresh of pull request 1, as a
// webhook announcing it would.
func leasedPullRequest(t *testing.T, store *Store) githubsync.Lease {
	t.Helper()
	github := store.GitHub()
	ref := githubsync.PullRequestRef{RepositoryID: 10, InstallationID: 5, FullName: "octo/app", Number: 1}
	if err := github.RequestRefresh(t.Context(), ref, "", true, 1); err != nil {
		t.Fatal(err)
	}
	lease, err := github.ClaimRefresh(t.Context(), "token", 1, time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := github.RecordLease(t.Context(), lease, []byte("{}"), 1); err != nil {
		t.Fatal(err)
	}
	return lease
}

func dueNotifications(t *testing.T, store *Store) []pushservice.Notification {
	t.Helper()
	due, err := store.Push().DueNotifications(t.Context(), time.Now().Add(48*time.Hour).UnixMilli(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return due
}

func TestCompleteRefreshCommitsNotificationsWithAdmissionsOrNeither(t *testing.T) {
	store := openTestStore(t)
	alice, bob := remoteAccount(t, store, "alice"), remoteAccount(t, store, "bob")
	lease := leasedPullRequest(t, store)
	if head, opened, err := store.GitHub().PullRequestHead(t.Context(), 10, 1); err != nil || head != "" || !opened {
		t.Fatalf("announced pull request: %q %v %v", head, opened, err)
	}
	notification := &githubsync.Notification{Title: "octo/app #1", Body: "Add search\nNew PR by @mona"}
	invalid := queuedRequest("")
	failing := []githubsync.Admission{{OwnerID: alice, Request: queuedRequest("a"), Notification: notification}, {OwnerID: bob, Request: invalid, Notification: notification}}
	if err := store.GitHub().CompleteRefresh(t.Context(), lease, "open", "head-1", failing, time.Now().UnixMilli()); err == nil {
		t.Fatal("invalid admission completed")
	}
	if due := dueNotifications(t, store); len(due) != 0 {
		t.Fatalf("notification outlived its rolled back admission: %+v", due)
	}
	if _, err := store.Queue().Claim(t.Context(), time.Minute); !errors.Is(err, ingestionqueue.ErrEmpty) {
		t.Fatalf("admission outlived its rolled back refresh: %v", err)
	}
	if head, _, err := store.GitHub().PullRequestHead(t.Context(), 10, 1); err != nil || head != "" {
		t.Fatalf("head recorded by a rolled back refresh: %q %v", head, err)
	}

	admissions := []githubsync.Admission{{OwnerID: alice, Request: queuedRequest("a"), Notification: notification}, {OwnerID: bob, Request: queuedRequest("b")}}
	if err := store.GitHub().CompleteRefresh(t.Context(), lease, "open", "head-1", admissions, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	due := dueNotifications(t, store)
	if len(due) != 1 || due[0].OwnerID != alice || due[0].Kind != "pull_request" || due[0].JobState != "queued" || !due[0].Available ||
		string(due[0].Content) != `{"title":"octo/app #1","body":"Add search\nNew PR by @mona"}` {
		t.Fatalf("notifications %+v", due)
	}
	delivery, err := store.Queue().Claim(t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for delivery.OwnerID != alice {
		if delivery, err = store.Queue().Claim(t.Context(), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Queue().Fail(t.Context(), delivery, "rejected"); err != nil {
		t.Fatal(err)
	}
	if due := dueNotifications(t, store); len(due) != 1 || due[0].JobState != "failed" {
		t.Fatalf("notification not linked to its follower's job: %+v", due)
	}
	if head, _, err := store.GitHub().PullRequestHead(t.Context(), 10, 1); err != nil || head != "head-1" {
		t.Fatalf("captured head %q %v", head, err)
	}
}

func TestPushSubscriptionsRebindAndCascadeWithAccounts(t *testing.T) {
	store := openTestStore(t)
	push := store.Push()
	alice, bob := remoteAccount(t, store, "alice"), remoteAccount(t, store, "bob")
	device := pushservice.Subscription{OwnerID: alice, Endpoint: "https://push.test/a", P256dh: "key", Auth: "auth", Label: "Phone", CreatedAt: 1}
	first, err := push.PutSubscription(t.Context(), device)
	if err != nil {
		t.Fatal(err)
	}
	device.Label, device.CreatedAt = "Renamed", 2
	again, err := push.PutSubscription(t.Context(), device)
	if err != nil || again.ID != first.ID || again.CreatedAt != 1 || again.Label != "Renamed" {
		t.Fatalf("same account re-registration: %+v %v", again, err)
	}
	if err := push.DeleteSubscription(t.Context(), bob, first.ID); !errors.Is(err, pushservice.ErrNotFound) {
		t.Fatalf("another account deleted the device: %v", err)
	}
	device.OwnerID = bob
	moved, err := push.PutSubscription(t.Context(), device)
	if err != nil || moved.ID == first.ID || moved.OwnerID != bob {
		t.Fatalf("rebind: %+v %v", moved, err)
	}
	if devices, err := push.Subscriptions(t.Context(), alice); err != nil || len(devices) != 0 {
		t.Fatalf("endpoint stayed with its previous account: %+v %v", devices, err)
	}

	lease := leasedPullRequest(t, store)
	notification := &githubsync.Notification{Title: "t", Body: "b"}
	if err := store.GitHub().CompleteRefresh(t.Context(), lease, "open", "head", []githubsync.Admission{{OwnerID: bob, Request: queuedRequest("b"), Notification: notification}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAccount(t.Context(), bob); err != nil {
		t.Fatal(err)
	}
	if devices, err := push.Subscriptions(t.Context(), bob); err != nil || len(devices) != 0 {
		t.Fatalf("devices survived account deletion: %+v %v", devices, err)
	}
	if due := dueNotifications(t, store); len(due) != 0 {
		t.Fatalf("notifications survived account deletion: %+v", due)
	}
}

func TestFollowMuteEndsWithTheFollow(t *testing.T) {
	store := openTestStore(t)
	github := store.GitHub()
	alice := remoteAccount(t, store, "alice")
	if err := github.SetMuted(t.Context(), alice, 10, true); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("muted an unfollowed repository: %v", err)
	}
	if err := github.PutConnection(t.Context(), githubsync.Connection{OwnerID: alice, AccessToken: []byte{1}, RefreshToken: []byte{1}, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	follow := githubsync.Follow{OwnerID: alice, RepositoryID: 10, InstallationID: 5, FullName: "octo/app", Status: "active"}
	if err := github.PutFollow(t.Context(), follow); err != nil {
		t.Fatal(err)
	}
	if err := github.SetMuted(t.Context(), alice, 10, true); err != nil {
		t.Fatal(err)
	}
	follow.Status = "lost"
	if err := github.PutFollow(t.Context(), follow); err != nil {
		t.Fatal(err)
	}
	if follows, err := github.RepositoryFollows(t.Context(), 10); err != nil || len(follows) != 1 || !follows[0].Muted {
		t.Fatalf("access update cleared the mute: %+v %v", follows, err)
	}
	if err := github.DeleteFollow(t.Context(), alice, 10); err != nil {
		t.Fatal(err)
	}
	if err := github.PutFollow(t.Context(), follow); err != nil {
		t.Fatal(err)
	}
	if follows, err := github.Follows(t.Context(), alice); err != nil || len(follows) != 1 || follows[0].Muted {
		t.Fatalf("mute outlived its follow: %+v %v", follows, err)
	}
}
