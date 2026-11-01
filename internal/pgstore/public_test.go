package pgstore_test

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

// Public snapshots expire after the admins' public retention, read when they
// are ingested; every account keeps the store's retention.
func TestPublicOwnerAloneUsesPublicRetention(t *testing.T) {
	t.Parallel()
	stores, owner, _ := replicas(t, 1)
	store := stores[0]
	expiry := func(ownerID, submission, content string) time.Duration {
		t.Helper()
		binding, err := store.IngestContext(t.Context(), ownerID, request(submission, content))
		if err != nil {
			t.Fatal(err)
		}
		item, err := store.Context(t.Context(), ownerID, binding.ContextID, time.Now())
		if err != nil || item.ExpiresAt == nil {
			t.Fatalf("context %+v %v", item, err)
		}
		return time.Until(time.UnixMilli(*item.ExpiresAt))
	}
	within := func(got, want time.Duration) bool { return got > want-time.Hour && got <= want }
	day := 24 * time.Hour
	if got := expiry(githubsync.PublicOwnerID, "public-1", "a"); !within(got, day) {
		t.Fatalf("default public retention %v", got)
	}
	testsupport.PutSettings(t, store, identityservice.Settings{PublicPullRequests: true, PublicRetentionDays: 3})
	if got := expiry(githubsync.PublicOwnerID, "public-2", "b"); !within(got, 3*day) {
		t.Fatalf("changed public retention %v", got)
	}
	if got := expiry(owner, "account-1", "c"); !within(got, 7*day) {
		t.Fatalf("account retention %v", got)
	}
}

// Fetch counters share the authentication table but keep their own hour-long
// windows: authentication pruning never resets them.
func TestFetchWindowsSurviveAuthenticationPruning(t *testing.T) {
	t.Parallel()
	stores, _, _ := replicas(t, 1)
	store := stores[0]
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range 3 {
		count, window, err := store.CountFetchAttempt(t.Context(), "192.0.2.1", start.Add(time.Duration(i)*time.Minute), time.Hour)
		if err != nil || count != i+1 || !window.Equal(start) {
			t.Fatalf("fetch %d: %d %v %v", i+1, count, window, err)
		}
	}
	// An authentication window starting later prunes ended authentication windows.
	if _, err := store.CountAuthAttempt(t.Context(), "192.0.2.9", start.Add(10*time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	count, window, err := store.CountFetchAttempt(t.Context(), "192.0.2.1", start.Add(20*time.Minute), time.Hour)
	if err != nil || count != 4 || !window.Equal(start) {
		t.Fatalf("fetch after authentication pruning: %d %v %v", count, window, err)
	}
	// The authentication client and the fetch client of one IP count apart.
	if attempts, err := store.CountAuthAttempt(t.Context(), "192.0.2.1", start.Add(21*time.Minute), time.Minute); err != nil || attempts != 1 {
		t.Fatalf("authentication attempts of a fetching client: %d %v", attempts, err)
	}
	if count, window, err := store.CountFetchAttempt(t.Context(), "192.0.2.1", start.Add(time.Hour), time.Hour); err != nil || count != 1 || !window.Equal(start.Add(time.Hour)) {
		t.Fatalf("fetch after the window: %d %v %v", count, window, err)
	}
}
