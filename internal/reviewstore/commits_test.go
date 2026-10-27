package reviewstore

import (
	"errors"
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

func TestObservationCommitsStoreAndRead(t *testing.T) {
	store := openTestStore(t)
	user := testUser(t, store, "commits")
	other := testUser(t, store, "other")
	binding, err := store.Ingest(user.ID, commitObservationRequest("first"))
	if err != nil {
		t.Fatal(err)
	}
	commits, summary, err := store.ObservationCommits(t.Context(), user.ID, binding.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	if summary != (review.CommitRange{Count: 2, Omitted: 3}) || len(commits) != 2 {
		t.Fatalf("summary %+v commits %+v", summary, commits)
	}
	if !commits[0].Merge || commits[0].FileCount != 0 || commits[1].Merge || commits[1].Subject != "Change file" || commits[1].Body != "Why." || commits[1].FileCount != 1 || commits[1].Additions != 3 || commits[1].Deletions != 1 {
		t.Fatalf("commits %+v", commits)
	}
	snapshot, err := store.ObservationCommitSnapshot(t.Context(), user.ID, binding.ContextID, commits[1].OID)
	if err != nil || snapshot.Mode != review.DiffCommit || len(snapshot.Files) != 1 || snapshot.VersionID == "" {
		t.Fatalf("commit snapshot %+v %v", snapshot, err)
	}
	version, err := store.ObservationCommitFileVersion(t.Context(), user.ID, binding.ContextID, commits[1].OID, "file")
	if err != nil || version.Fingerprint == nil || *version.Fingerprint != "commit-fingerprint" || version.VersionID != snapshot.VersionID {
		t.Fatalf("file version %+v %v", version, err)
	}
	patch, err := store.ObservationCommitPatch(t.Context(), user.ID, binding.ContextID, commits[1].OID, "file", "commit-fingerprint")
	if err != nil || patch.Patch != "commit-patch" {
		t.Fatalf("patch %+v %v", patch, err)
	}
	if _, err := store.ObservationCommitSnapshot(t.Context(), user.ID, binding.ContextID, commits[0].OID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merge commit diff: %v", err)
	}
	if _, _, err := store.ObservationCommits(t.Context(), other.ID, binding.ContextID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner listed commits: %v", err)
	}
	if _, err := store.ObservationCommitSnapshot(t.Context(), other.ID, binding.ContextID, commits[1].OID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner read commit diff: %v", err)
	}
	item, err := store.Context(t.Context(), user.ID, binding.ContextID, time.Now())
	if err != nil || item.Commits == nil || *item.Commits != summary {
		t.Fatalf("context commits %+v %v", item.Commits, err)
	}
	if item.ChangedFileCount == nil || *item.ChangedFileCount != 1 || len(item.ScopeSummaries) != 3 {
		t.Fatalf("commit diffs leaked into scopes or counts: %+v %+v", item.ChangedFileCount, item.ScopeSummaries)
	}
	plain, err := store.Ingest(user.ID, observationRequest("source", "plain"))
	if err != nil {
		t.Fatal(err)
	}
	if item, err := store.Context(t.Context(), user.ID, plain.ContextID, time.Now()); err != nil || item.Commits != nil {
		t.Fatalf("observation without commits: %+v %v", item.Commits, err)
	}
	if commits, summary, err := store.ObservationCommits(t.Context(), user.ID, plain.ContextID); err != nil || len(commits) != 0 || summary.Count != 0 {
		t.Fatalf("empty commits: %+v %+v %v", commits, summary, err)
	}
}

func TestObservationCommitsExpireAndDeleteWithObservation(t *testing.T) {
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
	if _, err := store.db.Exec(`UPDATE diffs SET expires_at=?`, cutoff.UnixMilli()); err != nil {
		t.Fatal(err)
	}
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
