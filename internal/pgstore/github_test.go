package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

// Millisecond values with a sub-second part prove exact round trips.
const epoch int64 = 1_760_000_000_123

type githubHarness struct {
	github *pgstore.GitHubStore
	conn   *pgx.Conn
}

func newGitHubHarness(t *testing.T, owners ...string) githubHarness {
	t.Helper()
	ctx := context.Background()
	url := pgtest.URL(t)
	store, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	for _, owner := range owners {
		if _, err := conn.Exec(ctx, `INSERT INTO diffx.users(id, name, created_at) VALUES ($1, $1, now())`, owner); err != nil {
			t.Fatal(err)
		}
	}
	return githubHarness{github: store.GitHub(), conn: conn}
}

func (h githubHarness) count(t *testing.T, query string, arguments ...any) int {
	t.Helper()
	var count int
	if err := h.conn.QueryRow(context.Background(), query, arguments...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func connection(owner string, user int64) githubsync.Connection {
	return githubsync.Connection{OwnerID: owner, UserID: user, Login: "login-" + owner, AccessToken: []byte("sealed-access-" + owner), AccessExpiresAt: epoch + 1,
		RefreshToken: []byte("sealed-refresh-" + owner), RefreshExpiresAt: epoch + 2, Status: "active", ConnectedAt: epoch + 3}
}

func follow(owner string, repository int64, installation int64, name string) githubsync.Follow {
	return githubsync.Follow{OwnerID: owner, RepositoryID: repository, InstallationID: installation, FullName: name, Status: "active", VerifiedAt: epoch + 4, CreatedAt: epoch + 5}
}

func pull(repository int64, number int) githubsync.PullRequestRef {
	return githubsync.PullRequestRef{RepositoryID: repository, InstallationID: 5, FullName: "octo/app", Number: number}
}

func submission(id string) ingestion.Request {
	head := "1111111111111111111111111111111111111111"
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: id,
		Metadata: ingestion.Metadata{SourceID: "github", Hostname: "github.com", RepositoryKey: "repo", RepositoryName: "app", CheckoutKey: "checkout",
			Root: "/app", Branch: "feature-" + id, Head: &head, CollectedAt: 1, Trigger: "manual", CollectorVersion: "test"},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{Source: "local", Root: "/app", Branch: "feature-" + id, Head: &head, Mode: review.DiffAll, Revision: "revision-" + id,
				Files: []review.ChangedFile{{ID: "f", Path: "search.go", Status: "M", Fingerprint: "fp-" + id}}},
			Patches: map[string]review.FilePatch{"f": {Patch: "@@ -1 +1 @@\n-a\n+b\n"}},
		}},
	}
}

func TestGitHubOAuthStateIsTakenOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t, "alice")
	if err := h.github.PutOAuthState(ctx, "expired", "alice", epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := h.github.TakeOAuthState(ctx, "expired", epoch); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("take expired = %v", err)
	}
	if err := h.github.PutOAuthState(ctx, "state", "alice", epoch+1); err != nil {
		t.Fatal(err)
	}
	owner, err := h.github.TakeOAuthState(ctx, "state", epoch)
	if err != nil || owner != "alice" {
		t.Fatalf("take = %q, %v", owner, err)
	}
	if _, err := h.github.TakeOAuthState(ctx, "state", epoch); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("second take = %v", err)
	}
	if _, err := h.github.TakeOAuthState(ctx, "unknown", epoch); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("take unknown = %v", err)
	}
}

func TestGitHubConnectionsRoundTripAndRevoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t, "alice", "bob", "carol")
	if _, err := h.github.Connection(ctx, "alice"); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("missing connection = %v", err)
	}
	for _, c := range []githubsync.Connection{connection("bob", 7), connection("alice", 7), connection("carol", 8)} {
		if err := h.github.PutConnection(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.github.Connection(ctx, "alice")
	if err != nil || !reflect.DeepEqual(got, connection("alice", 7)) {
		t.Fatalf("connection = %+v, %v", got, err)
	}
	updated := connection("alice", 7)
	updated.Login, updated.AccessToken, updated.AccessExpiresAt, updated.Status = "renamed", []byte("rotated"), epoch+100, "expired"
	if err := h.github.PutConnection(ctx, updated); err != nil {
		t.Fatal(err)
	}
	all, err := h.github.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []githubsync.Connection{updated, connection("bob", 7), connection("carol", 8)}; !reflect.DeepEqual(all, want) {
		t.Fatalf("connections = %+v", all)
	}

	for _, owner := range []string{"alice", "bob", "carol"} {
		if err := h.github.PutFollow(ctx, follow(owner, 10, 5, "octo/app")); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.github.RevokeUser(ctx, 7); err != nil {
		t.Fatal(err)
	}
	all, err = h.github.Connections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, c := range all {
		statuses[c.OwnerID] = c.Status
	}
	if want := map[string]string{"alice": "revoked", "bob": "revoked", "carol": "active"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("connection statuses = %v", statuses)
	}
	follows, err := h.github.RepositoryFollows(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	statuses = map[string]string{}
	for _, f := range follows {
		statuses[f.OwnerID] = f.Status
	}
	if want := map[string]string{"alice": "lost", "bob": "lost", "carol": "active"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("follow statuses = %v", statuses)
	}

	if err := h.github.SetMuted(ctx, "alice", 10, true); err != nil {
		t.Fatal(err)
	}
	if err := h.github.DeleteConnection(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.github.Connection(ctx, "alice"); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("deleted connection = %v", err)
	}
	if follows, err := h.github.Follows(ctx, "alice"); err != nil || len(follows) != 0 {
		t.Fatalf("follows after delete = %+v, %v", follows, err)
	}
	if mutes := h.count(t, `SELECT count(*) FROM diffx.github_follow_mutes WHERE owner_id='alice'`); mutes != 0 {
		t.Fatalf("mutes after delete = %d", mutes)
	}
}

func TestGitHubFollowsAndMutes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t, "alice", "bob")
	for _, owner := range []string{"alice", "bob"} {
		if err := h.github.PutConnection(ctx, connection(owner, 7)); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []githubsync.Follow{follow("alice", 11, 5, "octo/zeta"), follow("alice", 10, 5, "octo/app"), follow("bob", 10, 5, "octo/app")} {
		if err := h.github.PutFollow(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	follows, err := h.github.Follows(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if want := []githubsync.Follow{follow("alice", 10, 5, "octo/app"), follow("alice", 11, 5, "octo/zeta")}; !reflect.DeepEqual(follows, want) {
		t.Fatalf("follows = %+v", follows)
	}

	if err := h.github.SetMuted(ctx, "alice", 10, true); err != nil {
		t.Fatal(err)
	}
	if err := h.github.SetMuted(ctx, "alice", 10, true); err != nil {
		t.Fatalf("mute twice = %v", err)
	}
	// A put refreshes access but leaves the mute.
	refreshed := follow("alice", 10, 6, "octo/renamed")
	refreshed.Status, refreshed.VerifiedAt, refreshed.CreatedAt = "lost", epoch+50, epoch+60
	if err := h.github.PutFollow(ctx, refreshed); err != nil {
		t.Fatal(err)
	}
	repository, err := h.github.RepositoryFollows(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := follow("alice", 10, 6, "octo/renamed")
	want.Status, want.VerifiedAt, want.Muted = "lost", epoch+50, true
	if expected := []githubsync.Follow{follow("bob", 10, 5, "octo/app"), want}; !reflect.DeepEqual(repository, expected) {
		t.Fatalf("repository follows = %+v", repository)
	}

	if err := h.github.SetMuted(ctx, "alice", 99, true); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("mute unknown follow = %v", err)
	}
	if err := h.github.SetMuted(ctx, "alice", 99, false); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("unmute unknown follow = %v", err)
	}
	if err := h.github.SetMuted(ctx, "alice", 10, false); err != nil {
		t.Fatal(err)
	}
	if follows, err := h.github.Follows(ctx, "alice"); err != nil || follows[0].Muted {
		t.Fatalf("unmuted follows = %+v, %v", follows, err)
	}

	// A mute ends with its follow.
	if err := h.github.SetMuted(ctx, "alice", 10, true); err != nil {
		t.Fatal(err)
	}
	if err := h.github.DeleteFollow(ctx, "alice", 10); err != nil {
		t.Fatal(err)
	}
	if err := h.github.PutFollow(ctx, follow("alice", 10, 5, "octo/app")); err != nil {
		t.Fatal(err)
	}
	if follows, err := h.github.Follows(ctx, "alice"); err != nil || len(follows) != 2 || follows[0].Muted {
		t.Fatalf("follows after refollow = %+v, %v", follows, err)
	}
}

func TestGitHubLoseInstallation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t, "alice")
	if err := h.github.PutConnection(ctx, connection("alice", 7)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []githubsync.Follow{follow("alice", 10, 5, "octo/a"), follow("alice", 11, 5, "octo/b"), follow("alice", 12, 5, "octo/c"), follow("alice", 20, 6, "other/d")} {
		if err := h.github.PutFollow(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	statuses := func() map[int64]string {
		follows, err := h.github.Follows(ctx, "alice")
		if err != nil {
			t.Fatal(err)
		}
		result := map[int64]string{}
		for _, f := range follows {
			result[f.RepositoryID] = f.Status
		}
		return result
	}
	if err := h.github.LoseInstallation(ctx, 5, []int64{11, 20}); err != nil {
		t.Fatal(err)
	}
	if got, want := statuses(), map[int64]string{10: "active", 11: "lost", 12: "active", 20: "active"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after listed = %v", got)
	}
	if err := h.github.LoseInstallation(ctx, 5, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := statuses(), map[int64]string{10: "lost", 11: "lost", 12: "lost", 20: "active"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after installation = %v", got)
	}
}

func TestGitHubDeliveriesDeduplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t)
	if seen, err := h.github.SeenDelivery(ctx, "guid"); err != nil || seen {
		t.Fatalf("seen before record = %v, %v", seen, err)
	}
	for range 2 {
		if err := h.github.RecordDelivery(ctx, "guid", "pull_request", epoch); err != nil {
			t.Fatal(err)
		}
	}
	if seen, err := h.github.SeenDelivery(ctx, "guid"); err != nil || !seen {
		t.Fatalf("seen after record = %v, %v", seen, err)
	}
	if seen, err := h.github.SeenDelivery(ctx, "other"); err != nil || seen {
		t.Fatalf("seen other = %v, %v", seen, err)
	}
}

func TestGitHubRequestRefreshCoalescesAndRecordsOpened(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t)
	if head, opened, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "" || opened {
		t.Fatalf("unknown head = %q, %v, %v", head, opened, err)
	}
	if err := h.github.RequestRefresh(ctx, pull(10, 1), "run-1", false, epoch); err != nil {
		t.Fatal(err)
	}
	if head, opened, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "" || opened {
		t.Fatalf("unannounced head = %q, %v, %v", head, opened, err)
	}
	renamed := pull(10, 1)
	renamed.InstallationID, renamed.FullName = 6, "octo/renamed"
	if err := h.github.RequestRefresh(ctx, renamed, "", true, epoch+1); err != nil {
		t.Fatal(err)
	}
	if head, opened, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "" || !opened {
		t.Fatalf("announced head = %q, %v, %v", head, opened, err)
	}
	if err := h.github.RequestRefresh(ctx, pull(10, 2), "", false, epoch+2); err != nil {
		t.Fatal(err)
	}
	if open, err := h.github.KnownOpen(ctx, 10); err != nil || !reflect.DeepEqual(open, []int{1, 2}) {
		t.Fatalf("known open = %v, %v", open, err)
	}
	if open, err := h.github.KnownOpen(ctx, 11); err != nil || len(open) != 0 {
		t.Fatalf("known open other = %v, %v", open, err)
	}

	lease, err := h.github.ClaimRefresh(ctx, "token", epoch+3, epoch+1000)
	if err != nil {
		t.Fatal(err)
	}
	// An empty retention run keeps the pending one.
	if want := (githubsync.Lease{Ref: renamed, Token: "token", Generation: 2, RetentionRun: "run-1"}); lease != want {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestGitHubClaimRefreshOldestFirstAndFencesEarlierLeases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t)
	for _, request := range []struct {
		ref githubsync.PullRequestRef
		at  int64
	}{{pull(10, 3), epoch + 2}, {pull(10, 1), epoch + 1}, {pull(9, 2), epoch + 2}} {
		if err := h.github.RequestRefresh(ctx, request.ref, "", false, request.at); err != nil {
			t.Fatal(err)
		}
	}
	// Oldest request first; ties break by repository and number.
	for i, want := range []githubsync.PullRequestRef{pull(10, 1), pull(9, 2), pull(10, 3)} {
		lease, err := h.github.ClaimRefresh(ctx, fmt.Sprint("token-", i), epoch+10, epoch+100)
		if err != nil || lease.Ref != want {
			t.Fatalf("claim %d = %+v, %v; want %+v", i, lease.Ref, err, want)
		}
	}
	if _, err := h.github.ClaimRefresh(ctx, "none", epoch+99, epoch+200); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("claim while leased = %v", err)
	}

	// An expired lease is reclaimable and the new claim fences the old token.
	stale := githubsync.Lease{Ref: pull(10, 1), Token: "token-0", Generation: 1}
	fresh, err := h.github.ClaimRefresh(ctx, "fresh", epoch+100, epoch+1000)
	if err != nil || fresh.Ref != pull(10, 1) || fresh.Generation != 1 {
		t.Fatalf("reclaim = %+v, %v", fresh, err)
	}
	if err := h.github.RecordLease(ctx, stale, []byte("stale"), epoch+101); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("record fenced = %v", err)
	}
	if _, _, err := h.github.Leased(ctx, 10, 1, "token-0", epoch+101); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("leased fenced = %v", err)
	}
	if err := h.github.ReleaseRefresh(ctx, stale, epoch+101, "late"); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("release fenced = %v", err)
	}
	if err := h.github.CompleteRefresh(ctx, stale, "closed", "stale", []githubsync.Admission{{OwnerID: "nobody", Request: submission("stale")}}, epoch+101); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("complete fenced = %v", err)
	}
	if head, _, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "" {
		t.Fatalf("fenced head = %q, %v", head, err)
	}
	if err := h.github.RecordLease(ctx, fresh, []byte("fresh"), epoch+101); err != nil {
		t.Fatalf("record current = %v", err)
	}
}

func TestGitHubLeaseRecordAndRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t)
	if err := h.github.RequestRefresh(ctx, pull(10, 1), "run", false, epoch); err != nil {
		t.Fatal(err)
	}
	lease, err := h.github.ClaimRefresh(ctx, "token", epoch, epoch+100)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing recorded yet.
	if _, _, err := h.github.Leased(ctx, 10, 1, "token", epoch+1); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("leased before record = %v", err)
	}
	leased := []byte{0, 1, 2, 0xff}
	if err := h.github.RecordLease(ctx, lease, leased, epoch+1); err != nil {
		t.Fatal(err)
	}
	got, payload, err := h.github.Leased(ctx, 10, 1, "token", epoch+99)
	if err != nil || got != lease || !reflect.DeepEqual(payload, leased) {
		t.Fatalf("leased = %+v, %v, %v", got, payload, err)
	}
	if _, _, err := h.github.Leased(ctx, 10, 1, "other", epoch+99); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("leased other token = %v", err)
	}
	if _, _, err := h.github.Leased(ctx, 10, 1, "token", epoch+100); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("leased expired = %v", err)
	}
	if err := h.github.RecordLease(ctx, lease, leased, epoch+100); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("record expired = %v", err)
	}

	if err := h.github.ReleaseRefresh(ctx, lease, epoch+500, "fetch failed"); err != nil {
		t.Fatal(err)
	}
	if err := h.github.ReleaseRefresh(ctx, lease, epoch+500, "again"); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("release twice = %v", err)
	}
	if _, _, err := h.github.Leased(ctx, 10, 1, "token", epoch+2); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("leased after release = %v", err)
	}
	if _, err := h.github.ClaimRefresh(ctx, "early", epoch+499, epoch+1000); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("claim before retry = %v", err)
	}
	retry, err := h.github.ClaimRefresh(ctx, "retry", epoch+500, epoch+1000)
	if err != nil {
		t.Fatal(err)
	}
	if want := (githubsync.Lease{Ref: pull(10, 1), Token: "retry", Generation: 1, RetentionRun: "run", Attempt: 1}); retry != want {
		t.Fatalf("retry lease = %+v", retry)
	}
	if detail := h.count(t, `SELECT count(*) FROM diffx.github_pull_requests WHERE detail='fetch failed' AND octet_length(leased)=0`); detail != 1 {
		t.Fatalf("released row detail = %d", detail)
	}
}

func TestGitHubCompleteRefreshAdmitsWithNotificationsAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newGitHubHarness(t, "alice", "bob")
	if err := h.github.RequestRefresh(ctx, pull(10, 1), "run-1", true, epoch); err != nil {
		t.Fatal(err)
	}
	lease, err := h.github.ClaimRefresh(ctx, "token", epoch, epoch+100)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.github.RecordLease(ctx, lease, []byte("{}"), epoch); err != nil {
		t.Fatal(err)
	}
	notification := &githubsync.Notification{Title: "octo/app #1", Body: "Add search"}

	// An admission failure rolls back the whole completion.
	invalid := submission("bad")
	invalid.SubmissionID = " "
	failing := []githubsync.Admission{{OwnerID: "alice", Request: submission("a1"), Notification: notification}, {OwnerID: "bob", Request: invalid}}
	if err := h.github.CompleteRefresh(ctx, lease, "closed", "head-1", failing, epoch+1); err == nil {
		t.Fatal("complete with invalid admission succeeded")
	}
	if head, _, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "" {
		t.Fatalf("head after failed complete = %q, %v", head, err)
	}
	if jobs := h.count(t, `SELECT count(*) FROM diffx.ingestion_jobs`); jobs != 0 {
		t.Fatalf("jobs after failed complete = %d", jobs)
	}
	if notifications := h.count(t, `SELECT count(*) FROM diffx.push_notifications`); notifications != 0 {
		t.Fatalf("notifications after failed complete = %d", notifications)
	}

	// A refresh requested during the lease stays pending.
	if err := h.github.RequestRefresh(ctx, pull(10, 1), "run-2", false, epoch+2); err != nil {
		t.Fatal(err)
	}
	admissions := []githubsync.Admission{{OwnerID: "alice", Request: submission("a1"), Notification: notification}, {OwnerID: "bob", Request: submission("b1")}}
	if err := h.github.CompleteRefresh(ctx, lease, "closed", "head-1", admissions, epoch+3); err != nil {
		t.Fatal(err)
	}
	if head, opened, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "head-1" || !opened {
		t.Fatalf("head = %q, %v, %v", head, opened, err)
	}
	if open, err := h.github.KnownOpen(ctx, 10); err != nil || len(open) != 0 {
		t.Fatalf("known open after close = %v, %v", open, err)
	}
	if jobs := h.count(t, `SELECT count(*) FROM diffx.ingestion_jobs j JOIN diffx.ingestion_records r ON r.sequence=j.sequence WHERE j.state='queued' AND r.owner_id IN ('alice','bob')`); jobs != 2 {
		t.Fatalf("queued jobs = %d", jobs)
	}
	var payload []byte
	if err := h.conn.QueryRow(ctx, `SELECT n.payload FROM diffx.push_notifications n JOIN diffx.ingestion_jobs j ON j.id=n.job_id JOIN diffx.ingestion_records r ON r.sequence=j.sequence
		WHERE n.owner_id='alice' AND r.owner_id='alice' AND r.submission_id='a1' AND n.kind='pull_request' AND n.state='pending'`).Scan(&payload); err != nil {
		t.Fatalf("alice notification: %v", err)
	}
	if string(payload) != `{"title":"octo/app #1","body":"Add search"}` {
		t.Fatalf("notification payload = %s", payload)
	}
	if notifications := h.count(t, `SELECT count(*) FROM diffx.push_notifications`); notifications != 1 {
		t.Fatalf("notifications = %d", notifications)
	}
	if err := h.github.CompleteRefresh(ctx, lease, "closed", "head-1", admissions, epoch+4); !errors.Is(err, githubsync.ErrLeaseLost) {
		t.Fatalf("complete twice = %v", err)
	}

	next, err := h.github.ClaimRefresh(ctx, "next", epoch+5, epoch+100)
	if err != nil {
		t.Fatalf("pending after complete = %v", err)
	}
	if next.Generation != 2 || next.RetentionRun != "run-2" || next.Attempt != 0 {
		t.Fatalf("next lease = %+v", next)
	}
	// An empty head keeps the captured one; the consumed run clears.
	if err := h.github.CompleteRefresh(ctx, next, "open", "", nil, epoch+6); err != nil {
		t.Fatal(err)
	}
	if head, _, err := h.github.PullRequestHead(ctx, 10, 1); err != nil || head != "head-1" {
		t.Fatalf("head after empty = %q, %v", head, err)
	}
	if _, err := h.github.ClaimRefresh(ctx, "idle", epoch+7, epoch+100); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("claim when refreshed = %v", err)
	}
	if err := h.github.RequestRefresh(ctx, pull(10, 1), "", false, epoch+8); err != nil {
		t.Fatal(err)
	}
	last, err := h.github.ClaimRefresh(ctx, "last", epoch+9, epoch+100)
	if err != nil || last.Generation != 3 || last.RetentionRun != "" {
		t.Fatalf("last lease = %+v, %v", last, err)
	}
}

func TestGitHubConcurrentClaimsLeaseEachPullRequestOnce(t *testing.T) {
	t.Parallel()
	for _, pending := range []int{1, 2} {
		t.Run(fmt.Sprint(pending, " pending"), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			h := newGitHubHarness(t)
			for number := 1; number <= pending; number++ {
				if err := h.github.RequestRefresh(ctx, pull(10, number), "", false, epoch); err != nil {
					t.Fatal(err)
				}
			}
			const claimers = 16
			type result struct {
				lease githubsync.Lease
				err   error
			}
			results := make(chan result, claimers)
			start := make(chan struct{})
			var wait sync.WaitGroup
			for i := range claimers {
				wait.Go(func() {
					<-start
					lease, err := h.github.ClaimRefresh(ctx, fmt.Sprint("token-", i), epoch+1, epoch+time.Minute.Milliseconds())
					results <- result{lease, err}
				})
			}
			close(start)
			wait.Wait()
			close(results)
			claimed := map[int]string{}
			for r := range results {
				if errors.Is(r.err, githubsync.ErrNotFound) {
					continue
				}
				if r.err != nil {
					t.Fatal(r.err)
				}
				if previous, ok := claimed[r.lease.Ref.Number]; ok {
					t.Fatalf("pull request %d leased by %s and %s", r.lease.Ref.Number, previous, r.lease.Token)
				}
				claimed[r.lease.Ref.Number] = r.lease.Token
			}
			if len(claimed) != pending {
				t.Fatalf("leases = %v, want %d", claimed, pending)
			}
			for number, token := range claimed {
				lease := githubsync.Lease{Ref: pull(10, number), Token: token, Generation: 1}
				if err := h.github.RecordLease(ctx, lease, []byte("x"), epoch+2); err != nil {
					t.Fatalf("winning lease %s fenced: %v", token, err)
				}
			}
		})
	}
}
