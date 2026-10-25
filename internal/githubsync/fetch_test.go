package githubsync_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/review"
)

const (
	headOne = "1111111111111111111111111111111111111111"
	headTwo = "2222222222222222222222222222222222222222"
	baseTip = "3333333333333333333333333333333333333333"
)

func pullRequest(head string) github.PullRequest {
	created := time.UnixMilli(1_700_000_000_000)
	return github.PullRequest{
		Number: 1, State: "open", Title: "Add search", Body: "Adds **search**.", HTMLURL: "https://github.com/octo/app/pull/1",
		Author:    github.Account{Login: "mona"},
		Base:      github.Ref{Ref: "main", SHA: baseTip, Repository: github.Repository{ID: appRepo.ID, FullName: "octo/app", CloneURL: "https://github.com/octo/app.git"}},
		Head:      github.Ref{Ref: "search", SHA: head, Repository: github.Repository{ID: 77, FullName: "mona/app"}},
		CreatedAt: created, UpdatedAt: created.Add(time.Hour),
	}
}

// fetched is what a fetcher returns: an owner-free snapshot of its mirror with
// its own labels, which the server must replace.
func fetched(job *fetchprotocol.Job) ingestion.Request {
	head := job.HeadSHA
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: "fetcher-chosen",
		Metadata: ingestion.Metadata{
			SourceID: "fetcher", Hostname: "fetcher-host", Trigger: "manual", RepositoryKey: "mirror", CheckoutKey: "mirror",
			Root: "/mirrors/10", Branch: "diffx/pr/1", Head: &head, CollectedAt: 1, CollectorVersion: "test",
			Comparison: &ingestion.Comparison{Kind: "branch", BaseRef: "refs/remotes/origin/main", BaseCommit: job.BaseSHA, MergeBase: job.BaseSHA},
		},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{
				Source: "local", Root: "/mirrors/10", Name: "10", Branch: "diffx/pr/1", Head: &head, Mode: review.DiffAll,
				ComparisonKind: review.ComparisonCommitted, Revision: "revision-" + head,
				Files: []review.ChangedFile{{ID: "f1", Path: "search.go", Status: "M", Fingerprint: "fp-" + head}},
			},
			Patches: map[string]review.FilePatch{"f1": {Patch: "@@ -1 +1 @@\n-a\n+b\n"}},
		}},
	}
}

func (f *fixture) followPull(t *testing.T, owners ...string) {
	t.Helper()
	f.github.open = map[string][]github.PullRequest{"octo/app": {{Number: 1}}}
	f.github.pulls = map[int]github.PullRequest{1: pullRequest(headOne)}
	for _, owner := range owners {
		f.connect(t, owner, map[string]string{f.alice: "alice", f.bob: "bob"}[owner])
		if err := f.service.Follow(t.Context(), owner, appRepo.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fixture) lease(t *testing.T) *fetchprotocol.Job {
	t.Helper()
	job, err := f.service.Lease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func (f *fixture) publish(t *testing.T) *fetchprotocol.Job {
	t.Helper()
	job := f.lease(t)
	if job == nil {
		t.Fatal("no pending pull request")
	}
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); err != nil {
		t.Fatal(err)
	}
	return job
}

// admitted claims every newly admitted submission.
func (f *fixture) admitted(t *testing.T) []ingestionqueue.Delivery {
	t.Helper()
	var deliveries []ingestionqueue.Delivery
	for {
		delivery, err := f.store.Queue().Claim(t.Context(), time.Hour)
		if errors.Is(err, ingestionqueue.ErrEmpty) {
			return deliveries
		}
		if err != nil {
			t.Fatal(err)
		}
		deliveries = append(deliveries, delivery)
	}
}

func TestLeasePublishesServerAuthoredSnapshotToFollowers(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice, f.bob)
	job := f.lease(t)
	if job == nil || job.HeadSHA != headOne || job.BaseSHA != baseTip || job.BaseRef != "main" || job.Repository.CloneURL != "https://github.com/octo/app.git" || job.Token != "installation-token" {
		t.Fatalf("job %+v", job)
	}
	grant := f.github.tokens[len(f.github.tokens)-1]
	if grant.installation != appRepo.InstallationID || !slices.Equal(grant.repositories, []int64{appRepo.ID}) || len(grant.permissions) != 1 || grant.permissions["contents"] != "read" {
		t.Fatalf("fetch token not narrowed to the repository's contents: %+v", grant)
	}
	if again := f.lease(t); again != nil {
		t.Fatalf("leased pull request leased twice: %+v", again)
	}
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); err != nil {
		t.Fatal(err)
	}
	deliveries := f.admitted(t)
	if len(deliveries) != 2 {
		t.Fatalf("admitted %d submissions, want one per follower", len(deliveries))
	}
	owners := []string{deliveries[0].OwnerID, deliveries[1].OwnerID}
	slices.Sort(owners)
	want := []string{f.alice, f.bob}
	slices.Sort(want)
	if !slices.Equal(owners, want) {
		t.Fatalf("admitted to %v, want both followers", owners)
	}
	got := deliveries[0].Request
	metadata := got.Metadata
	pull := metadata.PullRequest
	if metadata.SourceID != "github" || metadata.Trigger != "github" || metadata.Hostname != "github.com" || pull == nil {
		t.Fatalf("fetcher provenance survived: %+v", metadata)
	}
	if metadata.RepositoryKey != review.RemoteRepositoryKey("https://github.com/octo/app.git") || metadata.Root != "https://github.com/octo/app/pull/1" || metadata.Branch != "search" || metadata.WorktreeName != "PR #1" {
		t.Fatalf("identity %+v", metadata)
	}
	if got.Scopes[0].Snapshot.Root != metadata.Root || got.Scopes[0].Snapshot.Branch != "search" || !strings.HasPrefix(got.SubmissionID, "gh-") {
		t.Fatalf("scope or submission not rewritten: %+v %q", got.Scopes[0].Snapshot, got.SubmissionID)
	}
	if pull.Title != "Add search" || pull.Author.Login != "mona" || pull.Author.URL != "https://github.com/mona" || pull.State != "open" || pull.HeadRepository != "mona/app" || metadata.CollectedAt != pull.UpdatedAt {
		t.Fatalf("pull request %+v", pull)
	}
	if deliveries[1].Request.SubmissionID != got.SubmissionID {
		t.Fatal("followers received different submissions of one snapshot")
	}
	if f.lease(t) != nil {
		t.Fatal("completed refresh still pending")
	}
}

func TestRefetchReplaysAndNewHeadIsFresh(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	f.publish(t)
	first := f.admitted(t)
	if len(first) != 1 {
		t.Fatalf("admitted %d", len(first))
	}
	f.webhook(t, "again", "synchronize")
	f.publish(t)
	if replayed := f.admitted(t); len(replayed) != 0 {
		t.Fatalf("unchanged pull request admitted again: %+v", replayed)
	}
	f.github.pulls[1] = pullRequest(headTwo)
	f.webhook(t, "pushed", "synchronize")
	f.publish(t)
	pushed := f.admitted(t)
	if len(pushed) != 1 || pushed[0].SubmissionID == first[0].SubmissionID || *pushed[0].Request.Metadata.Head != headTwo {
		t.Fatalf("force push not a fresh submission: %+v", pushed)
	}
	if pushed[0].Request.Metadata.CheckoutKey != first[0].Request.Metadata.CheckoutKey || pushed[0].Request.Metadata.BranchID != first[0].Request.Metadata.BranchID {
		t.Fatal("new head left the pull request's stream")
	}
}

func TestResyncMakesSubmissionFresh(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	f.publish(t)
	first := f.admitted(t)
	if err := f.service.Resync(t.Context(), f.alice); err != nil {
		t.Fatal(err)
	}
	f.publish(t)
	resynced := f.admitted(t)
	if len(resynced) != 1 || resynced[0].SubmissionID == first[0].SubmissionID {
		t.Fatalf("resync replayed instead of extending retention: %+v", resynced)
	}
	f.webhook(t, "after", "edited")
	f.publish(t)
	if later := f.admitted(t); len(later) != 0 {
		t.Fatalf("retention run outlived its refresh: %+v", later)
	}
}

func TestFinishIsFencedByLease(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	job := f.lease(t)
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: "forged", Request: &request}); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("forged lease: %v", err)
	}
	*f.clock = f.clock.Add(11 * time.Minute)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("expired lease: %v", err)
	}
	next := f.lease(t)
	if next == nil || next.LeaseToken == job.LeaseToken {
		t.Fatal("expired lease not reclaimable")
	}
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("superseded lease: %v", err)
	}
	if deliveries := f.admitted(t); len(deliveries) != 0 {
		t.Fatalf("fenced result admitted: %+v", deliveries)
	}
}

func TestMismatchedSnapshotRetriesWithBackoff(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	job := f.lease(t)
	staleBase := *job
	staleBase.BaseSHA = headTwo
	request := fetched(&staleBase)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); !errors.Is(err, githubsync.ErrInvalidResult) {
		t.Fatalf("snapshot against another base: %v", err)
	}
	*f.clock = f.clock.Add(time.Minute)
	job = f.lease(t)
	staleHead := *job
	staleHead.HeadSHA = headTwo
	request = fetched(&staleHead)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); !errors.Is(err, githubsync.ErrInvalidResult) {
		t.Fatalf("snapshot of another head: %v", err)
	}
	if deliveries := f.admitted(t); len(deliveries) != 0 {
		t.Fatalf("mismatched snapshot admitted: %+v", deliveries)
	}
	if f.lease(t) != nil {
		t.Fatal("failed refresh retried without backoff")
	}
	*f.clock = f.clock.Add(2 * time.Minute)
	retry := f.lease(t)
	if retry == nil {
		t.Fatal("failed refresh not retried")
	}
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: retry.ID, LeaseToken: retry.LeaseToken, Error: "mirror unavailable"}); err != nil {
		t.Fatal(err)
	}
	*f.clock = f.clock.Add(2 * time.Minute)
	if f.lease(t) != nil {
		t.Fatal("third failure backed off no longer than the second")
	}
}

func TestRefreshDuringLeaseStaysPending(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	job := f.lease(t)
	f.github.pulls[1] = pullRequest(headTwo)
	f.webhook(t, "during", "synchronize")
	request := fetched(job)
	if err := f.service.Finish(t.Context(), fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request}); err != nil {
		t.Fatal(err)
	}
	next := f.lease(t)
	if next == nil || next.HeadSHA != headTwo {
		t.Fatalf("push during the fetch was lost: %+v", next)
	}
}

func TestDeletedOrUnfollowedPullRequestsNeedNoFetch(t *testing.T) {
	f := newFixture(t)
	f.followPull(t, f.alice)
	delete(f.github.pulls, 1)
	if job := f.lease(t); job != nil {
		t.Fatalf("deleted pull request leased: %+v", job)
	}
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.github.pulls[1] = pullRequest(headOne)
	if err := f.service.Unfollow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	if job := f.lease(t); job != nil {
		t.Fatalf("unfollowed pull request leased: %+v", job)
	}
	if deliveries := f.admitted(t); len(deliveries) != 0 {
		t.Fatalf("admitted without followers: %+v", deliveries)
	}
}

func (f *fixture) webhook(t *testing.T, delivery, action string) {
	t.Helper()
	body := []byte(`{"action":"` + action + `","number":1,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`)
	if err := f.service.Webhook(t.Context(), "pull_request", delivery, "valid", body); err != nil {
		t.Fatal(err)
	}
}
