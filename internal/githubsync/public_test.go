package githubsync_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
)

// admitter counts fetch admissions and refuses with err when set.
type admitter struct {
	count int
	err   error
}

func (a *admitter) admit(context.Context) error {
	a.count++
	return a.err
}

// publicPull serves octo/app as a public repository with pull request 1.
func (f *fixture) publicPull() {
	f.github.public = map[string]github.Repository{"octo/app": {ID: appRepo.ID, FullName: "octo/app", Visibility: "public", Size: 1024}}
	f.github.pulls = map[int]github.PullRequest{1: pullRequest(headOne)}
}

func (f *fixture) ensure(t *testing.T, admit *admitter) githubsync.PublicPullRequest {
	t.Helper()
	status, err := f.service.EnsurePublic(t.Context(), "octo", "app", 1, admit.admit)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func (f *fixture) status(t *testing.T) githubsync.PublicPullRequest {
	t.Helper()
	status, err := f.service.PublicStatus(t.Context(), "octo", "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

// ingest publishes admitted submissions as the ingestion worker does.
func (f *fixture) ingest(t *testing.T, deliveries []ingestionqueue.Delivery) string {
	t.Helper()
	var contextID string
	for _, delivery := range deliveries {
		binding, err := f.store.IngestContext(t.Context(), delivery.OwnerID, delivery.Request)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.store.Queue().Complete(t.Context(), delivery, binding.ContextID); err != nil {
			t.Fatal(err)
		}
		if delivery.OwnerID == githubsync.PublicOwnerID {
			contextID = binding.ContextID
		}
	}
	return contextID
}

func TestPublicPreviewRefusesPrivateMissingAndOversizedRepositories(t *testing.T) {
	f := newFixture(t)
	f.publicPull()
	admit := &admitter{}
	refusals := []struct {
		name       string
		repository *github.Repository
		pull       bool
		code       string
	}{
		{"missing repository", nil, true, githubsync.FailureNotFound},
		{"private repository", &github.Repository{ID: 10, FullName: "octo/app", Private: true, Visibility: "private"}, true, githubsync.FailureNotFound},
		{"internal repository", &github.Repository{ID: 10, FullName: "octo/app", Visibility: "internal"}, true, githubsync.FailureNotFound},
		{"oversized repository", &github.Repository{ID: 10, FullName: "octo/app", Visibility: "public", Size: 500*1024 + 1}, true, githubsync.FailureTooLarge},
		{"missing pull request", &github.Repository{ID: 10, FullName: "octo/app", Visibility: "public", Size: 1}, false, githubsync.FailureNotFound},
	}
	for _, refusal := range refusals {
		f.github.public = map[string]github.Repository{}
		if refusal.repository != nil {
			f.github.public["octo/app"] = *refusal.repository
		}
		f.github.pulls = map[int]github.PullRequest{}
		if refusal.pull {
			f.github.pulls[1] = pullRequest(headOne)
		}
		status := f.ensure(t, admit)
		if status.Status != githubsync.PublicFailed || status.Failure == nil || status.Failure.Code != refusal.code || status.Failure.Detail == "" || status.ContextID != nil {
			t.Fatalf("%s: %+v", refusal.name, status)
		}
	}
	if admit.count != 0 {
		t.Fatalf("refusals counted %d fetches", admit.count)
	}
	// Refusals are not persisted: there is nothing to report or lease.
	if _, err := f.service.PublicStatus(t.Context(), "octo", "app", 1); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("refused pull request has a status: %v", err)
	}
	if job := f.lease(t); job != nil {
		t.Fatalf("refused pull request leased: %+v", job)
	}
	for _, address := range []struct {
		owner, repo string
		number      int
	}{{"-octo", "app", 1}, {"octo-", "app", 1}, {"oc/to", "app", 1}, {"octo", "..", 1}, {"octo", "a/b", 1}, {"octo", "app", 0}, {"octo", "a pp", 1}} {
		if _, err := f.service.EnsurePublic(t.Context(), address.owner, address.repo, address.number, admit.admit); !errors.Is(err, githubsync.ErrInvalidPullRequest) {
			t.Fatalf("address %+v: %v", address, err)
		}
	}
	unavailable, err := githubsync.New(f.store.GitHub(), f.github, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unavailable.EnsurePublic(t.Context(), "octo", "app", 1, admit.admit); !errors.Is(err, githubsync.ErrNotFound) || unavailable.PublicAvailable() {
		t.Fatalf("preview without a public installation: %v", err)
	}
}

func TestPublicPreviewFetchesOnceAnonymouslyForThePublicOwner(t *testing.T) {
	f := newFixture(t)
	f.publicPull()
	admit := &admitter{}
	status := f.ensure(t, admit)
	if status.Status != githubsync.PublicQueued || status.FullName != "octo/app" || status.ContextID != nil || status.FetchedAt != nil || status.Failure != nil {
		t.Fatalf("first request: %+v", status)
	}
	if status.PullRequest == nil || status.PullRequest.Title != "Add search" || status.PullRequest.URL != "https://github.com/octo/app/pull/1" {
		t.Fatalf("metadata before the fetch: %+v", status.PullRequest)
	}
	if again := f.ensure(t, admit); again.Status != githubsync.PublicQueued || admit.count != 1 {
		t.Fatalf("pending request fetched again: %+v after %d fetches", again, admit.count)
	}
	for _, installation := range f.github.pullReads {
		if installation != publicInstallation {
			t.Fatalf("public read through installation %d", installation)
		}
	}
	job := f.lease(t)
	if job == nil || !job.Public || job.Token != "" || job.TokenExpiresAt != 0 || job.HeadSHA != headOne {
		t.Fatalf("public lease: %+v", job)
	}
	if len(f.github.tokens) != 0 {
		t.Fatalf("public lease minted tokens: %+v", f.github.tokens)
	}
	if got := f.status(t); got.Status != githubsync.PublicFetching {
		t.Fatalf("leased status: %+v", got)
	}
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t); got.Status != githubsync.PublicFetching || got.ContextID != nil {
		t.Fatalf("admitted, unpublished status: %+v", got)
	}
	deliveries := f.admitted(t)
	if len(deliveries) != 1 || deliveries[0].OwnerID != githubsync.PublicOwnerID {
		t.Fatalf("public admissions: %+v", deliveries)
	}
	contextID := f.ingest(t, deliveries)
	ready := f.status(t)
	if ready.Status != githubsync.PublicReady || ready.ContextID == nil || *ready.ContextID != contextID || ready.FetchedAt == nil || *ready.FetchedAt != f.clock.UnixMilli() {
		t.Fatalf("published status: %+v", ready)
	}
	if again := f.ensure(t, admit); again.Status != githubsync.PublicReady || admit.count != 1 {
		t.Fatalf("ready preview fetched again: %+v after %d fetches", again, admit.count)
	}
	if got := f.notifications(t); len(got) != 0 {
		t.Fatalf("public owner notified: %+v", got)
	}
	if f.lease(t) != nil {
		t.Fatal("completed public refresh still pending")
	}
}

func TestPublicRefreshJoinsWaitsAndFetchesOnlyNewHeads(t *testing.T) {
	f := newFixture(t)
	f.publicPull()
	admit := &admitter{}
	if _, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("refresh without a preview: %v", err)
	}
	f.ensure(t, admit)
	if joined, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit); err != nil || joined.Status != githubsync.PublicQueued || admit.count != 1 {
		t.Fatalf("refresh joined the pending fetch: %+v %v after %d fetches", joined, err, admit.count)
	}
	f.publish(t)
	first := f.ingest(t, f.admitted(t))

	if recent, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit); err != nil || recent.Status != githubsync.PublicReady || admit.count != 1 {
		t.Fatalf("refresh within a minute: %+v %v after %d fetches", recent, err, admit.count)
	}
	*f.clock = f.clock.Add(61 * time.Second)
	retitled := pullRequest(headOne)
	retitled.Title = "Add fuzzy search"
	f.github.pulls[1] = retitled
	unchanged, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit)
	if err != nil || unchanged.Status != githubsync.PublicReady || unchanged.ContextID == nil || *unchanged.ContextID != first || admit.count != 2 {
		t.Fatalf("unchanged head: %+v %v after %d fetches", unchanged, err, admit.count)
	}
	if unchanged.PullRequest == nil || unchanged.PullRequest.Title != "Add fuzzy search" {
		t.Fatalf("unchanged head kept stale metadata: %+v", unchanged.PullRequest)
	}
	if job := f.lease(t); job != nil {
		t.Fatalf("unchanged head fetched: %+v", job)
	}
	if again, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit); err != nil || admit.count != 2 || *again.ContextID != first {
		t.Fatalf("refresh within a minute of a check: %+v %v after %d fetches", again, err, admit.count)
	}

	*f.clock = f.clock.Add(61 * time.Second)
	f.github.pulls[1] = pullRequest(headTwo)
	pushed, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, admit.admit)
	if err != nil || pushed.Status != githubsync.PublicQueued || pushed.ContextID == nil || *pushed.ContextID != first || admit.count != 3 {
		t.Fatalf("new head keeps the ready snapshot while queued: %+v %v after %d fetches", pushed, err, admit.count)
	}
	job := f.publish(t)
	if job.HeadSHA != headTwo {
		t.Fatalf("refetched head %s", job.HeadSHA)
	}
	second := f.ingest(t, f.admitted(t))
	if latest := f.status(t); latest.Status != githubsync.PublicReady || *latest.ContextID != second || second == first {
		t.Fatalf("new head snapshot: %+v", latest)
	}

	// A refusal of the fetch limit is returned unchanged and records nothing.
	*f.clock = f.clock.Add(61 * time.Second)
	limited := &admitter{err: errors.New("limit reached")}
	f.github.pulls[1] = pullRequest(headThree)
	if _, err := f.service.RefreshPublic(t.Context(), "octo", "app", 1, limited.admit); !errors.Is(err, limited.err) {
		t.Fatalf("limited refresh: %v", err)
	}
	if job := f.lease(t); job != nil {
		t.Fatalf("limited refresh requested a fetch: %+v", job)
	}
}

func TestFailedPublicFetchReportsGenericFailureAndRetriesOnRequest(t *testing.T) {
	f := newFixture(t)
	f.publicPull()
	admit := &admitter{}
	f.ensure(t, admit)
	job := f.lease(t)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Error: "git fetch: secret internal detail"}); err != nil {
		t.Fatal(err)
	}
	failed := f.status(t)
	if failed.Status != githubsync.PublicFailed || failed.Failure == nil || failed.Failure.Code != githubsync.FailureFetchFailed || failed.Failure.Detail == "" {
		t.Fatalf("failed status: %+v", failed)
	}
	if bytes.Contains([]byte(failed.Failure.Detail), []byte("secret")) {
		t.Fatalf("failure leaked internal detail: %q", failed.Failure.Detail)
	}
	if f.lease(t) != nil {
		t.Fatal("failed fetch retried before its backoff")
	}
	retried := f.ensure(t, admit)
	if retried.Status != githubsync.PublicQueued || admit.count != 2 {
		t.Fatalf("retry request: %+v after %d fetches", retried, admit.count)
	}
	if again := f.lease(t); again == nil || !again.Public {
		t.Fatalf("retry not leased now: %+v", again)
	}
}

func TestFollowedPullRequestAdmitsThePublicOwnerWithoutNotification(t *testing.T) {
	f := newFixture(t)
	f.github.open = map[string][]github.PullRequest{}
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	f.publicPull()
	// A webhook announces the pull request, so its first snapshot notifies.
	f.webhook(t, "opened", "opened")
	admit := &admitter{}
	if status := f.ensure(t, admit); status.Status != githubsync.PublicQueued || admit.count != 1 {
		t.Fatalf("public request beside a follower refresh: %+v after %d fetches", status, admit.count)
	}
	job := f.lease(t)
	if job == nil || job.Public || job.Token != "installation-token" {
		t.Fatalf("followed lease: %+v", job)
	}
	// Public demand never replaced the follower's installation.
	if grant := f.github.tokens[len(f.github.tokens)-1]; grant.installation != appRepo.InstallationID {
		t.Fatalf("token minted for installation %d", grant.installation)
	}
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); err != nil {
		t.Fatal(err)
	}
	deliveries := f.admitted(t)
	owners := map[string]bool{}
	for _, delivery := range deliveries {
		owners[delivery.OwnerID] = true
	}
	if len(deliveries) != 2 || !owners[f.alice] || !owners[githubsync.PublicOwnerID] {
		t.Fatalf("admissions: %+v", owners)
	}
	notifications := f.notifications(t)
	if len(notifications) != 1 || notifications[0].owner != f.alice {
		t.Fatalf("notifications: %+v", notifications)
	}
	f.ingest(t, deliveries)
	if status := f.status(t); status.Status != githubsync.PublicReady {
		t.Fatalf("public status: %+v", status)
	}
	// Later follower refreshes no longer carry public demand.
	f.github.pulls[1] = pullRequest(headTwo)
	f.webhook(t, "pushed", "synchronize")
	f.publish(t)
	for _, delivery := range f.admitted(t) {
		if delivery.OwnerID == githubsync.PublicOwnerID {
			t.Fatal("follower refresh admitted the public owner without public demand")
		}
	}
}
