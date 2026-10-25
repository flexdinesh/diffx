package githubsync_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

type fakeGitHub struct {
	mu           sync.Mutex
	repositories map[string][]github.Repository // by access token
	open         map[string][]github.PullRequest
	users        map[string]github.Account
	refreshes    int
	refreshError error
	listings     int
	pulls        map[int]github.PullRequest
	tokens       []tokenGrant
}

type tokenGrant struct {
	installation int64
	repositories []int64
	permissions  map[string]string
}

func (f *fakeGitHub) AppSlug(context.Context) (string, error) { return "diffx-test", nil }
func (f *fakeGitHub) VerifyWebhook(_ []byte, signature string) bool {
	return signature == "valid"
}
func (f *fakeGitHub) AuthorizeURL(state, redirect string) string {
	return "https://github.test/authorize?" + url.Values{"state": {state}, "redirect_uri": {redirect}}.Encode()
}
func (f *fakeGitHub) Exchange(_ context.Context, code, _ string) (github.UserToken, error) {
	return github.UserToken{AccessToken: "access-" + code, ExpiresAt: time.Now().Add(8 * time.Hour), RefreshToken: "refresh-" + code}, nil
}
func (f *fakeGitHub) Refresh(_ context.Context, refresh string) (github.UserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	if f.refreshError != nil {
		return github.UserToken{}, f.refreshError
	}
	return github.UserToken{AccessToken: "access-" + refresh[len("refresh-"):], ExpiresAt: time.Now().Add(8 * time.Hour), RefreshToken: refresh}, nil
}
func (f *fakeGitHub) User(_ context.Context, token string) (github.Account, error) {
	return f.users[token], nil
}
func (f *fakeGitHub) UserRepositories(_ context.Context, token string) ([]github.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listings++
	return slices.Clone(f.repositories[token]), nil
}
func (f *fakeGitHub) OpenPullRequests(_ context.Context, _ int64, fullName string) ([]github.PullRequest, error) {
	return f.open[fullName], nil
}
func (f *fakeGitHub) PullRequest(_ context.Context, _ int64, _ string, number int) (github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pull, ok := f.pulls[number]
	if !ok {
		return github.PullRequest{}, &github.Error{Status: 404, Message: "Not Found"}
	}
	return pull, nil
}
func (f *fakeGitHub) InstallationToken(_ context.Context, installation int64, repositories []int64, permissions map[string]string) (github.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, tokenGrant{installation, repositories, permissions})
	return github.Token{Value: "installation-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// refreshLog records refresh intent while delegating to the real store.
type refreshLog struct {
	githubsync.Store
	mu   sync.Mutex
	refs []githubsync.PullRequestRef
	runs []string
}

func (r *refreshLog) RequestRefresh(ctx context.Context, ref githubsync.PullRequestRef, run string, now int64) error {
	r.mu.Lock()
	r.refs, r.runs = append(r.refs, ref), append(r.runs, run)
	r.mu.Unlock()
	return r.Store.RequestRefresh(ctx, ref, run, now)
}

func (r *refreshLog) take() ([]githubsync.PullRequestRef, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	refs, runs := r.refs, r.runs
	r.refs, r.runs = nil, nil
	return refs, runs
}

var appRepo = github.Repository{ID: 10, FullName: "octo/app", InstallationID: 5}
var otherRepo = github.Repository{ID: 11, FullName: "octo/secret", InstallationID: 5}

type fixture struct {
	store   *reviewstore.Store
	github  *fakeGitHub
	log     *refreshLog
	service *githubsync.Service
	clock   *time.Time
	alice   string
	bob     string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	alice, err := store.User("alice", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.User("bob", "bob")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	f := &fixture{store: store, clock: &clock, alice: alice.ID, bob: bob.ID,
		github: &fakeGitHub{
			repositories: map[string][]github.Repository{"access-alice": {appRepo, otherRepo}, "access-bob": {appRepo}},
			open:         map[string][]github.PullRequest{"octo/app": {{Number: 1}, {Number: 2}}},
			users:        map[string]github.Account{"access-alice": {ID: 100, Login: "alice-gh"}, "access-bob": {ID: 200, Login: "bob-gh"}},
		}}
	f.log = &refreshLog{Store: store.GitHub()}
	f.service, err = githubsync.New(f.log, f.github, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return *f.clock }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) connect(t *testing.T, owner, code string) {
	t.Helper()
	authorize, err := f.service.Connect(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorize)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != "https://diffx.test/api/v2/github/callback" {
		t.Fatalf("redirect %q", got)
	}
	if err := f.service.Complete(t.Context(), owner, parsed.Query().Get("state"), code); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizationIsBoundToStartingAccountAndSealed(t *testing.T) {
	f := newFixture(t)
	authorize, err := f.service.Connect(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorize)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if err := f.service.Complete(t.Context(), f.bob, state, "bob"); !errors.Is(err, githubsync.ErrForbidden) {
		t.Fatalf("another account completed alice's authorization: %v", err)
	}
	// The failed attempt consumed the state, so it cannot be replayed either.
	if err := f.service.Complete(t.Context(), f.alice, state, "alice"); !errors.Is(err, githubsync.ErrForbidden) {
		t.Fatalf("consumed state reused: %v", err)
	}
	f.connect(t, f.alice, "alice")
	connection, err := f.store.GitHub().Connection(t.Context(), f.alice)
	if err != nil || connection.Login != "alice-gh" || connection.UserID != 100 {
		t.Fatalf("connection %+v %v", connection, err)
	}
	for _, sealed := range [][]byte{connection.AccessToken, connection.RefreshToken} {
		if bytes.Contains(sealed, []byte("alice")) {
			t.Fatal("token stored in plaintext")
		}
	}
	status, err := f.service.Status(t.Context(), f.alice)
	if err != nil || !status.Connected || status.State != "active" || status.InstallURL != "https://github.com/apps/diffx-test/installations/new" {
		t.Fatalf("status %+v %v", status, err)
	}
}

func TestFollowRequiresAccessAndRefreshesOpenPullRequests(t *testing.T) {
	f := newFixture(t)
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); !errors.Is(err, githubsync.ErrNotConnected) {
		t.Fatalf("unconnected follow: %v", err)
	}
	f.connect(t, f.bob, "bob")
	if err := f.service.Follow(t.Context(), f.bob, otherRepo.ID); !errors.Is(err, githubsync.ErrForbidden) {
		t.Fatalf("followed a repository the GitHub user cannot read: %v", err)
	}
	if err := f.service.Follow(t.Context(), f.bob, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	refs, _ := f.log.take()
	if len(refs) != 2 || refs[0].Number != 1 || refs[1].Number != 2 || refs[0].InstallationID != 5 {
		t.Fatalf("open pull requests not requested: %+v", refs)
	}
	repositories, err := f.service.Repositories(t.Context(), f.bob)
	if err != nil || len(repositories) != 1 || !repositories[0].Followed || repositories[0].Lost {
		t.Fatalf("repositories %+v %v", repositories, err)
	}
	// Access removed on GitHub: the follow is listed as lost and gets nothing.
	f.github.repositories["access-bob"] = nil
	repositories, err = f.service.Repositories(t.Context(), f.bob)
	if err != nil || len(repositories) != 1 || !repositories[0].Lost {
		t.Fatalf("lost follow %+v %v", repositories, err)
	}
	followers, err := f.service.Followers(t.Context(), appRepo.ID)
	if err != nil || len(followers) != 0 {
		t.Fatalf("lost follower kept: %v %v", followers, err)
	}
}

func TestWebhookVerifiesDeduplicatesAndIgnoresUnfollowed(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	f.log.take()
	body := []byte(`{"action":"synchronize","number":3,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`)
	if err := f.service.Webhook(t.Context(), "pull_request", "d1", "forged", body); !errors.Is(err, githubsync.ErrSignature) {
		t.Fatalf("unsigned delivery: %v", err)
	}
	for range 2 {
		if err := f.service.Webhook(t.Context(), "pull_request", "d1", "valid", body); err != nil {
			t.Fatal(err)
		}
	}
	refs, runs := f.log.take()
	if len(refs) != 1 || refs[0].Number != 3 || runs[0] != "" {
		t.Fatalf("duplicate delivery applied twice: %+v", refs)
	}
	unfollowed := []byte(`{"action":"opened","number":4,"installation":{"id":5},"repository":{"id":11,"full_name":"octo/secret"}}`)
	labeled := []byte(`{"action":"labeled","number":3,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`)
	for id, payload := range map[string][]byte{"d2": unfollowed, "d3": labeled} {
		if err := f.service.Webhook(t.Context(), "pull_request", id, "valid", payload); err != nil {
			t.Fatal(err)
		}
	}
	if refs, _ := f.log.take(); len(refs) != 0 {
		t.Fatalf("unfollowed or irrelevant delivery requested refresh: %+v", refs)
	}
	revoke := []byte(`{"action":"revoked","sender":{"id":100}}`)
	if err := f.service.Webhook(t.Context(), "github_app_authorization", "d4", "valid", revoke); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.Status(t.Context(), f.alice)
	if err != nil || status.State != "revoked" {
		t.Fatalf("revocation not applied: %+v %v", status, err)
	}
	if err := f.service.Webhook(t.Context(), "pull_request", "d5", "valid", body); err != nil {
		t.Fatal(err)
	}
	if refs, _ := f.log.take(); len(refs) != 0 {
		t.Fatalf("revoked follower still requests refreshes: %+v", refs)
	}
}

func TestInstallationRemovalLosesFollows(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	for _, id := range []int64{appRepo.ID, otherRepo.ID} {
		if err := f.service.Follow(t.Context(), f.alice, id); err != nil {
			t.Fatal(err)
		}
	}
	removed := []byte(`{"action":"removed","installation":{"id":5},"repositories_added":[],"repositories_removed":[{"id":11,"full_name":"octo/secret"}]}`)
	if err := f.service.Webhook(t.Context(), "installation_repositories", "r1", "valid", removed); err != nil {
		t.Fatal(err)
	}
	follows, err := f.store.GitHub().Follows(t.Context(), f.alice)
	if err != nil || len(follows) != 2 || follows[0].Status != "active" || follows[1].Status != "lost" {
		t.Fatalf("follows %+v %v", follows, err)
	}
	deleted := []byte(`{"action":"deleted","installation":{"id":5}}`)
	if err := f.service.Webhook(t.Context(), "installation", "r2", "valid", deleted); err != nil {
		t.Fatal(err)
	}
	follows, err = f.store.GitHub().Follows(t.Context(), f.alice)
	if err != nil || follows[0].Status != "lost" {
		t.Fatalf("uninstall kept follows active: %+v %v", follows, err)
	}
}

func TestExpiredRefreshLosesFollowsAndAsksToReconnect(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	*f.clock = f.clock.Add(9 * time.Hour)
	if _, err := f.service.Repositories(t.Context(), f.alice); err != nil {
		t.Fatalf("expired access token not refreshed: %v", err)
	}
	if f.github.refreshes != 1 {
		t.Fatalf("refreshes %d", f.github.refreshes)
	}
	*f.clock = f.clock.Add(9 * time.Hour)
	f.github.refreshError = github.ErrUnauthorized
	if _, err := f.service.Repositories(t.Context(), f.alice); !errors.Is(err, githubsync.ErrReconnect) {
		t.Fatalf("rejected refresh: %v", err)
	}
	status, err := f.service.Status(t.Context(), f.alice)
	if err != nil || status.State != "expired" {
		t.Fatalf("status %+v %v", status, err)
	}
	followers, err := f.service.Followers(t.Context(), appRepo.ID)
	if err != nil || len(followers) != 0 {
		t.Fatalf("expired follower kept: %v %v", followers, err)
	}
}

func TestFollowersRecheckOnlyStaleVerification(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	f.connect(t, f.bob, "bob")
	for _, owner := range []string{f.alice, f.bob} {
		if err := f.service.Follow(t.Context(), owner, appRepo.ID); err != nil {
			t.Fatal(err)
		}
	}
	listings := f.github.listings
	followers, err := f.service.Followers(t.Context(), appRepo.ID)
	if err != nil || len(followers) != 2 || f.github.listings != listings {
		t.Fatalf("fresh verification rechecked: %v %v %d", followers, err, f.github.listings-listings)
	}
	*f.clock = f.clock.Add(7 * time.Hour)
	f.github.repositories["access-bob"] = nil
	if followers, _ := f.service.Followers(t.Context(), appRepo.ID); len(followers) != 2 {
		t.Fatalf("verification within 8h must be trusted: %v", followers)
	}
	*f.clock = f.clock.Add(2 * time.Hour)
	followers, err = f.service.Followers(t.Context(), appRepo.ID)
	if err != nil || !slices.Equal(followers, []string{f.alice}) {
		t.Fatalf("stale verification not rechecked: %v %v", followers, err)
	}
}

func TestReconcileRecoversMissedClosesAndResyncExtendsRetention(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	f.log.take()
	// PR 2 closed without a delivery; it is still known open.
	f.github.open["octo/app"] = []github.PullRequest{{Number: 1}}
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	refs, runs := f.log.take()
	numbers := make([]int, 0, len(refs))
	for _, ref := range refs {
		numbers = append(numbers, ref.Number)
	}
	if !slices.Equal(numbers, []int{1, 2}) || runs[0] != "" {
		t.Fatalf("reconcile requested %v runs %q", numbers, runs)
	}
	if err := f.service.Resync(t.Context(), f.alice); err != nil {
		t.Fatal(err)
	}
	_, runs = f.log.take()
	if len(runs) != 2 || runs[0] == "" || runs[0] != runs[1] {
		t.Fatalf("resync must share one retention run: %q", runs)
	}
	if err := f.service.Resync(t.Context(), f.alice); err != nil {
		t.Fatal(err)
	}
	_, again := f.log.take()
	if again[0] == runs[0] {
		t.Fatal("each resync needs a fresh retention run")
	}
}

func TestDisconnectRemovesFollows(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	if err := f.service.Follow(t.Context(), f.alice, appRepo.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Disconnect(t.Context(), f.alice); err != nil {
		t.Fatal(err)
	}
	follows, err := f.store.GitHub().Follows(t.Context(), f.alice)
	if err != nil || len(follows) != 0 {
		t.Fatalf("follows survived disconnect: %+v %v", follows, err)
	}
	if status, err := f.service.Status(t.Context(), f.alice); err != nil || status.Connected {
		t.Fatalf("status %+v %v", status, err)
	}
}
