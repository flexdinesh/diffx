package reviewstore

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
)

func TestDeleteContextRollsBackWhenAnchorDeletionFails(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	binding, err := store.Ingest(owner.ID, observationRequest("source", "request"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_anchor BEFORE DELETE ON diffs WHEN OLD.mode='all' BEGIN SELECT RAISE(ABORT,'anchor deletion failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), owner.ID, binding.ContextID); err == nil {
		t.Fatal("anchor failure did not abort deletion")
	}
	for mode := range binding.DiffIDs {
		if _, err := store.ObservationSnapshot(t.Context(), owner.ID, binding.ContextID, mode); err != nil {
			t.Fatalf("scope %s removed by failed deletion: %v", mode, err)
		}
	}
}

func tableCount(t *testing.T, store *Store, table string) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// The shared contract checks ended review reads; rows prove review state is
// released while the stream head survives.
func TestStreamEndDeletesReviewRowsAndKeepsHead(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	olderRequest := observationRequest("source", "older")
	olderRequest.Metadata.CollectedAt = 100
	older, err := store.Ingest(owner.ID, olderRequest)
	if err != nil {
		t.Fatal(err)
	}
	latestRequest := observationRequest("source", "latest")
	latestRequest.Metadata.CollectedAt = 200
	latest, err := store.Ingest(owner.ID, latestRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(t.Context(), older.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Path: "file.txt", Body: "keep", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), owner.ID, older.ContextID); err != nil {
		t.Fatal(err)
	}
	if got := tableCount(t, store, "comments"); got != 1 {
		t.Fatalf("comments before stream end: %d want 1", got)
	}
	if err := store.DeleteContext(t.Context(), owner.ID, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"reviews": 0, "comments": 0, "observation_stream_heads": 1} {
		if got := tableCount(t, store, table); got != want {
			t.Fatalf("%s after stream end: %d want %d", table, got, want)
		}
	}
}

// The shared contract checks pruned review reads; rows prove review state is
// released while the stream head survives.
func TestPruningLastSnapshotDeletesReviewRows(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	binding, err := store.Ingest(owner.ID, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(t.Context(), binding.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutMark(t.Context(), binding.ContextID, review.ReviewMark{Scope: review.DiffAll, FileID: "file"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneExpired(time.Now().Add(30 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"observations": 0, "reviews": 0, "comments": 0, "marks": 0, "observation_stream_heads": 1} {
		if got := tableCount(t, store, table); got != want {
			t.Fatalf("%s after pruning the stream: %d want %d", table, got, want)
		}
	}
}
