package reviewstore

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestDeleteLatestObservationKeepsOlderSnapshotsStale(t *testing.T) {
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
	if err := store.DeleteContext(owner.ID, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(owner.ID, older.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("older snapshot promoted after deletion: %#v, %v", item, err)
	}
	lateRequest := observationRequest("source", "late")
	lateRequest.Metadata.CollectedAt = 150
	late, err := store.Ingest(owner.ID, lateRequest)
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.Context(owner.ID, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("late snapshot promoted after deletion: %#v, %v", item, err)
	}
}

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
	if err := store.DeleteContext(owner.ID, binding.ContextID); err == nil {
		t.Fatal("anchor failure did not abort deletion")
	}
	for mode := range binding.DiffIDs {
		if _, err := store.ObservationSnapshot(owner.ID, binding.ContextID, mode); err != nil {
			t.Fatalf("scope %s removed by failed deletion: %v", mode, err)
		}
	}
}

func TestDirectReplayCannotResurrectDeletedObservation(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	input := observationRequest("source", "request")
	binding, err := store.Ingest(owner.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(owner.ID, binding.ContextID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ingest(owner.ID, input); err == nil {
		t.Fatal("exact replay resurrected deleted observation")
	}
	input.SubmissionID = "fresh"
	fresh, err := store.Ingest(owner.ID, input)
	if err != nil || fresh.ContextID == binding.ContextID {
		t.Fatalf("fresh submission: %+v %v", fresh, err)
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

func TestStreamReviewSurvivesSnapshotDeletionUntilStreamEnds(t *testing.T) {
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
	if err := store.PutComment(older.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Path: "file.txt", Body: "keep", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	comments, err := store.Comments(latest.ContextID)
	if err != nil || len(comments) != 1 || comments[0].DiffID != older.DiffIDs[review.DiffAll] {
		t.Fatalf("stream comment not shared by later snapshot: %#v %v", comments, err)
	}
	comments[0].Status = "resolved"
	if err := store.PutComment(latest.ContextID, comments[0]); err != nil {
		t.Fatalf("update through later snapshot: %v", err)
	}
	moved := comments[0]
	moved.DiffID = "other-diff"
	if err := store.PutComment(latest.ContextID, moved); err == nil {
		t.Fatal("comment re-pointed to a foreign diff")
	}
	if err := store.DeleteContext(owner.ID, older.ContextID); err != nil {
		t.Fatal(err)
	}
	comments, err = store.Comments(latest.ContextID)
	if err != nil || len(comments) != 1 || comments[0].Status != "resolved" {
		t.Fatalf("deleting a snapshot removed stream comments: %#v %v", comments, err)
	}
	comments[0].Body = "edited after its snapshot was deleted"
	if err := store.PutComment(latest.ContextID, comments[0]); err != nil {
		t.Fatalf("update after original snapshot deletion: %v", err)
	}
	if err := store.DeleteContext(owner.ID, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"reviews": 0, "comments": 0, "observation_stream_heads": 1} {
		if got := tableCount(t, store, table); got != want {
			t.Fatalf("%s after stream end: %d want %d", table, got, want)
		}
	}
	fresh, err := store.Ingest(owner.ID, observationRequest("source", "fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if comments, err := store.Comments(fresh.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("ended stream review resurrected: %#v %v", comments, err)
	}
}

func TestPruningLastSnapshotEndsStreamReview(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	binding, err := store.Ingest(owner.ID, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(binding.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutMark(binding.ContextID, review.ReviewMark{Scope: review.DiffAll, FileID: "file"}); err != nil {
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

func TestPipedObservationsHaveContextReviews(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	piped := func(id string) ingestion.Request {
		request := observationRequest("source", id)
		request.Metadata.RepositoryKey, request.Metadata.CheckoutKey = "", ""
		return request
	}
	first, err := store.Ingest(owner.ID, piped("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Ingest(owner.ID, piped("second"))
	if err != nil {
		t.Fatal(err)
	}
	comment := review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "piped", Status: "open", CreatedAt: 1}
	if err := store.PutComment(first.ContextID, comment); err != nil {
		t.Fatal(err)
	}
	if comments, err := store.Comments(second.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("piped review leaked across captures: %#v %v", comments, err)
	}
	if err := store.PutComment(second.ContextID, comment); err == nil {
		t.Fatal("comment ID claimed by another stream")
	}
	if deleted, err := store.DeleteComment(second.ContextID, "comment"); err != nil || deleted {
		t.Fatalf("deleted another stream's comment: %v %v", deleted, err)
	}
}

func TestMarksCarryWhileFilePatchIsUnchanged(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "owner")
	first, err := store.Ingest(owner.ID, observationRequest("source", "first"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ObservationSnapshot(owner.ID, first.ContextID, review.DiffAll)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutMark(first.ContextID, review.ReviewMark{Scope: review.DiffAll, VersionID: snapshot.VersionID, FileID: "file", FileVersion: "all-fingerprint"}); err != nil {
		t.Fatal(err)
	}
	same := observationRequest("source", "same")
	same.Scopes[0].Snapshot.Files[0].Fingerprint = "touched-fingerprint"
	carried, err := store.Ingest(owner.ID, same)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.ObservationSnapshot(owner.ID, carried.ContextID, review.DiffAll)
	if err != nil {
		t.Fatal(err)
	}
	marks, err := store.Marks(carried.ContextID, review.DiffAll)
	want := review.ReviewMark{DiffID: current.ID, VersionID: current.VersionID, FileID: "file", FileVersion: "touched-fingerprint", Scope: review.DiffAll}
	if err != nil || len(marks) != 1 || marks[0] != want {
		t.Fatalf("unchanged patch mark not projected: %#v %v", marks, err)
	}
	changed := observationRequest("source", "changed")
	changed.Scopes[0].Patches = map[string]review.FilePatch{"file": {Patch: "changed-patch"}}
	edited, err := store.Ingest(owner.ID, changed)
	if err != nil {
		t.Fatal(err)
	}
	if marks, err := store.Marks(edited.ContextID, review.DiffAll); err != nil || len(marks) != 0 {
		t.Fatalf("changed patch kept its mark: %#v %v", marks, err)
	}
	if err := store.ClearMarks(edited.ContextID, review.DiffAll); err != nil {
		t.Fatal(err)
	}
	if marks, err := store.Marks(first.ContextID, review.DiffAll); err != nil || len(marks) != 1 {
		t.Fatalf("clearing removed a mark hidden in that snapshot: %#v %v", marks, err)
	}
}
