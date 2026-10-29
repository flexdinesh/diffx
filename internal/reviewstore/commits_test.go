package reviewstore

import (
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func commitObservationRequest(submission string) ingestion.Request {
	request := observationRequest("source", submission)
	head := strings.Repeat("b", 40)
	request.Metadata.Head = &head
	request.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("1", 40), MergeBase: strings.Repeat("1", 40)}
	merge := ingestion.Commit{Commit: review.Commit{OID: strings.Repeat("a", 40), Parents: []string{strings.Repeat("1", 40), strings.Repeat("2", 40)}, Subject: "Merge main"}}
	diff := review.RepositoryDiff{Source: "local", Mode: review.DiffCommit, ComparisonKind: review.ComparisonCommit, Head: &head, Revision: "commit-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "commit-fingerprint", Additions: 3, Deletions: 1}}}
	commit := ingestion.Commit{Commit: review.Commit{OID: head, Parents: []string{merge.OID}, PatchID: strings.Repeat("c", 40), Subject: "Change file", Body: "Why.", AuthorName: "Ada", AuthoredAt: 1000, CommittedAt: 2000}, Snapshot: &diff, Patches: map[string]review.FilePatch{"file": {Patch: "commit-patch"}}}
	request.Commits, request.CommitsTotal = []ingestion.Commit{merge, commit}, 5
	return request
}

// The shared contract checks commit reads; rows prove storage is released.
func TestObservationCommitRowsExpireAndDeleteWithObservation(t *testing.T) {
	store := openTestStore(t)
	user := testUser(t, store, "commit-expiry")
	countCommitDiffs := func() int {
		t.Helper()
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM diffs WHERE mode=?`, review.DiffCommit).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	request := commitObservationRequest("first")
	request.ContentHash = strings.Repeat("a", 64)
	first, err := store.Ingest(user.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if countCommitDiffs() != 1 {
		t.Fatal("commit diff not stored")
	}
	// A fresh matching submission extends every diff's retention, commits included.
	cutoff := time.Now().Add(time.Minute)
	expire(t, store, first.ContextID, cutoff)
	fresh := request
	fresh.SubmissionID = "fresh"
	if again, err := store.Ingest(user.ID, fresh); err != nil || again.ContextID != first.ContextID {
		t.Fatalf("reuse %+v %v", again, err)
	}
	var stale int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM diffs WHERE expires_at<=?`, cutoff.UnixMilli()).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("reuse left %d diffs on the old expiry: %v", stale, err)
	}
	if err := store.PruneExpired(time.Now().Add(DefaultRetention + time.Hour)); err != nil {
		t.Fatal(err)
	}
	if countCommitDiffs() != 0 {
		t.Fatal("pruning kept commit diffs")
	}
	second, err := store.Ingest(user.ID, commitObservationRequest("second"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), user.ID, second.ContextID); err != nil {
		t.Fatal(err)
	}
	if countCommitDiffs() != 0 {
		t.Fatal("deleting the observation kept commit diffs")
	}
	var rows int
	if err := store.db.QueryRow(`SELECT (SELECT COUNT(*) FROM observation_commits)+(SELECT COUNT(*) FROM observation_commit_ranges)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("commit rows survived: %d %v", rows, err)
	}
}
