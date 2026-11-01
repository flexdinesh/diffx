package fetcher

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	gh "github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// stubGitHub serves one repository with pull request 7 of an origin.
type stubGitHub struct{ pull gh.PullRequest }

func (stubGitHub) AppSlug(context.Context) (string, error) { return "diffx-test", nil }
func (stubGitHub) VerifyWebhook([]byte, string) bool       { return false }
func (stubGitHub) AuthorizeURL(state, redirect string) string {
	return "https://github.test/?state=" + url.QueryEscape(state)
}
func (stubGitHub) Exchange(context.Context, string, string) (gh.UserToken, error) {
	return gh.UserToken{AccessToken: "user", ExpiresAt: time.Now().Add(time.Hour), RefreshToken: "refresh"}, nil
}
func (stubGitHub) Refresh(context.Context, string) (gh.UserToken, error) {
	return gh.UserToken{}, gh.ErrUnauthorized
}
func (stubGitHub) User(context.Context, string) (gh.Account, error) {
	return gh.Account{ID: 1, Login: "alice"}, nil
}
func (stubGitHub) UserRepositories(context.Context, string) ([]gh.Repository, error) {
	return []gh.Repository{{ID: 42, FullName: "octo/repo", InstallationID: 5}}, nil
}
func (s stubGitHub) OpenPullRequests(context.Context, int64, string) ([]gh.PullRequest, error) {
	return []gh.PullRequest{s.pull}, nil
}
func (s stubGitHub) PullRequest(context.Context, int64, string, int) (gh.PullRequest, error) {
	return s.pull, nil
}
func (stubGitHub) InstallationToken(context.Context, int64, []int64, map[string]string) (gh.Token, error) {
	return gh.Token{Value: testToken, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (stubGitHub) Repository(context.Context, int64, string) (gh.Repository, error) {
	return gh.Repository{ID: 42, FullName: "octo/repo", Visibility: "public", Size: 1}, nil
}

// leaseServer is the fetcher's view of the server, wired straight to the service.
type leaseServer struct {
	service  *githubsync.Service
	reported func()
	err      error
}

func (server *leaseServer) Lease(ctx context.Context) (*fetchprotocol.Job, error) {
	return server.service.Lease(ctx)
}
func (server *leaseServer) Complete(ctx context.Context, job fetchprotocol.Job, request ingestion.Request) error {
	defer server.reported()
	server.err = server.service.Finish(ctx, fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &request})
	return server.err
}
func (server *leaseServer) Fail(ctx context.Context, job fetchprotocol.Job, detail string) error {
	defer server.reported()
	server.err = &fetchprotocol.StatusError{Detail: detail}
	return server.service.Finish(ctx, fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Error: detail})
}

func TestFetchedPullRequestBecomesFollowerReview(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	store := pgtest.Store(t)
	owner := testsupport.RemoteAccount(t, store, "alice")
	created := time.UnixMilli(1_700_000_000_000)
	repository := gh.Repository{ID: 42, FullName: "octo/repo", CloneURL: "https://github.com/octo/repo.git"}
	pull := gh.PullRequest{Number: 7, State: "open", Title: "Edit files", Body: "Body", Author: gh.Account{Login: "mona"},
		Base: gh.Ref{Ref: "main", SHA: origin.base, Repository: repository}, Head: gh.Ref{Ref: "feature", SHA: origin.head, Repository: repository},
		CreatedAt: created, UpdatedAt: created.Add(time.Minute)}
	service, err := githubsync.New(store.GitHub(), stubGitHub{pull: pull}, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := service.Connect(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(t.Context(), owner.ID, parsed.Query().Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	if err := service.Follow(t.Context(), owner.ID, 42); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := &leaseServer{service: service, reported: cancel}
	if err := run(ctx, server, Settings{Mirrors: t.TempDir(), DiskLimit: 1 << 30, Concurrency: 1, PollInterval: time.Millisecond, CollectorVersion: "test"}, origin.remote()); err != nil {
		t.Fatal(err)
	}
	if server.err != nil {
		t.Fatalf("server refused the fetched snapshot: %v", server.err)
	}
	delivery, err := store.Queue().Claim(t.Context(), time.Minute)
	if err != nil || delivery.OwnerID != owner.ID {
		t.Fatalf("follower admission: %+v %v", delivery, err)
	}
	published, err := store.IngestContext(t.Context(), owner.ID, delivery.Request)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner.ID, published.ContextID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	metadata := item.Metadata
	if metadata == nil || metadata.PullRequest == nil || metadata.PullRequest.Number != 7 || metadata.PullRequest.Author.Login != "mona" {
		t.Fatalf("stored review lacks pull request metadata: %+v", metadata)
	}
	if metadata.RepositoryKey != review.RemoteRepositoryKey("https://github.com/octo/repo.git") || metadata.Branch != "feature" || metadata.Root != "https://github.com/octo/repo/pull/7" {
		t.Fatalf("stored identity %+v", metadata)
	}
}

func TestPublicPullRequestBecomesSharedPreview(t *testing.T) {
	requireTestGit(t)
	origin := newOrigin(t)
	store := pgtest.Store(t)
	created := time.UnixMilli(1_700_000_000_000)
	repository := gh.Repository{ID: 42, FullName: "octo/repo", CloneURL: "https://github.com/octo/repo.git"}
	pull := gh.PullRequest{Number: 7, State: "open", Title: "Edit files", Author: gh.Account{Login: "mona"},
		Base: gh.Ref{Ref: "main", SHA: origin.base, Repository: repository}, Head: gh.Ref{Ref: "feature", SHA: origin.head, Repository: repository},
		CreatedAt: created, UpdatedAt: created.Add(time.Minute)}
	service, err := githubsync.New(store.GitHub(), stubGitHub{pull: pull}, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: make([]byte, 32), PublicInstallationID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if status, err := service.EnsurePublic(t.Context(), "octo", "repo", 7, func(context.Context) error { return nil }); err != nil || status.Status != githubsync.PublicQueued {
		t.Fatalf("ensure: %+v %v", status, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := &leaseServer{service: service, reported: cancel}
	if err := run(ctx, server, Settings{Mirrors: t.TempDir(), DiskLimit: 1 << 30, Concurrency: 1, PollInterval: time.Millisecond, CollectorVersion: "test"}, origin.remote()); err != nil {
		t.Fatal(err)
	}
	if server.err != nil {
		t.Fatalf("server refused the public snapshot: %v", server.err)
	}
	delivery, err := store.Queue().Claim(t.Context(), time.Minute)
	if err != nil || delivery.OwnerID != githubsync.PublicOwnerID {
		t.Fatalf("public admission: %+v %v", delivery, err)
	}
	if status, err := service.PublicStatus(t.Context(), "octo", "repo", 7); err != nil || status.Status != githubsync.PublicFetching {
		t.Fatalf("admitted snapshot status: %+v %v", status, err)
	}
	published, err := store.IngestContext(t.Context(), delivery.OwnerID, delivery.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Queue().Complete(t.Context(), delivery, published.ContextID); err != nil {
		t.Fatal(err)
	}
	status, err := service.PublicStatus(t.Context(), "Octo", "Repo", 7)
	if err != nil || status.Status != githubsync.PublicReady || status.ContextID == nil || *status.ContextID != published.ContextID || status.FetchedAt == nil {
		t.Fatalf("published status: %+v %v", status, err)
	}
	item, err := store.Context(t.Context(), githubsync.PublicOwnerID, published.ContextID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Public snapshots keep the default public retention of one day.
	if item.ExpiresAt == nil {
		t.Fatal("public snapshot never expires")
	}
	if expires := time.UnixMilli(*item.ExpiresAt); expires.After(time.Now().Add(25*time.Hour)) || expires.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("public snapshot expires %v", expires)
	}
}
