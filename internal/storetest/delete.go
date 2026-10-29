package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func (harness Harness[S]) deleteLatestObservationKeepsOlderSnapshotsStale(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	olderRequest := observationRequest("source", "older")
	olderRequest.Metadata.CollectedAt = 100
	older, err := ingest(t, store, owner, olderRequest)
	if err != nil {
		t.Fatal(err)
	}
	latestRequest := observationRequest("source", "latest")
	latestRequest.Metadata.CollectedAt = 200
	latest, err := ingest(t, store, owner, latestRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), owner, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner, older.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("older snapshot promoted after deletion: %#v, %v", item, err)
	}
	lateRequest := observationRequest("source", "late")
	lateRequest.Metadata.CollectedAt = 150
	late, err := ingest(t, store, owner, lateRequest)
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.Context(t.Context(), owner, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("late snapshot promoted after deletion: %#v, %v", item, err)
	}
}

func (harness Harness[S]) directReplayCannotResurrectDeletedObservation(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	input := observationRequest("source", "request")
	binding, err := ingest(t, store, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), owner, binding.ContextID); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest(t, store, owner, input); err == nil {
		t.Fatal("exact replay resurrected deleted observation")
	}
	input.SubmissionID = "fresh"
	fresh, err := ingest(t, store, owner, input)
	if err != nil || fresh.ContextID == binding.ContextID {
		t.Fatalf("fresh submission: %+v %v", fresh, err)
	}
}

func (harness Harness[S]) streamReviewSurvivesSnapshotDeletionUntilStreamEnds(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	olderRequest := observationRequest("source", "older")
	olderRequest.Metadata.CollectedAt = 100
	older, err := ingest(t, store, owner, olderRequest)
	if err != nil {
		t.Fatal(err)
	}
	latestRequest := observationRequest("source", "latest")
	latestRequest.Metadata.CollectedAt = 200
	latest, err := ingest(t, store, owner, latestRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(t.Context(), older.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Path: "file.txt", Body: "keep", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	comments, err := store.Comments(t.Context(), latest.ContextID)
	if err != nil || len(comments) != 1 || comments[0].DiffID != older.DiffIDs[review.DiffAll] {
		t.Fatalf("stream comment not shared by later snapshot: %#v %v", comments, err)
	}
	comments[0].Status = "resolved"
	if err := store.PutComment(t.Context(), latest.ContextID, comments[0]); err != nil {
		t.Fatalf("update through later snapshot: %v", err)
	}
	moved := comments[0]
	moved.DiffID = "other-diff"
	if err := store.PutComment(t.Context(), latest.ContextID, moved); err == nil {
		t.Fatal("comment re-pointed to a foreign diff")
	}
	if err := store.DeleteContext(t.Context(), owner, older.ContextID); err != nil {
		t.Fatal(err)
	}
	comments, err = store.Comments(t.Context(), latest.ContextID)
	if err != nil || len(comments) != 1 || comments[0].Status != "resolved" {
		t.Fatalf("deleting a snapshot removed stream comments: %#v %v", comments, err)
	}
	comments[0].Body = "edited after its snapshot was deleted"
	if err := store.PutComment(t.Context(), latest.ContextID, comments[0]); err != nil {
		t.Fatalf("update after original snapshot deletion: %v", err)
	}
	if err := store.DeleteContext(t.Context(), owner, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	fresh, err := ingest(t, store, owner, observationRequest("source", "fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if comments, err := store.Comments(t.Context(), fresh.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("ended stream review resurrected: %#v %v", comments, err)
	}
	// The stream head outlives its review, so an earlier collection stays stale.
	if item, err := store.Context(t.Context(), owner, fresh.ContextID, time.Now()); err != nil || !item.Stale {
		t.Fatalf("ended stream head forgotten: %#v %v", item, err)
	}
}

func (harness Harness[S]) pruningLastSnapshotEndsStreamReview(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	binding, err := ingest(t, store, owner, observationRequest("source", "only"))
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
	if items, err := store.Contexts(t.Context(), owner, 100, 0, "", time.Now()); err != nil || len(items) != 0 {
		t.Fatalf("observation after pruning the stream: %#v %v", items, err)
	}
	if _, err := store.Context(t.Context(), owner, binding.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("pruned context: %v", err)
	}
	// An unchanged, earlier collection would inherit surviving review state and
	// stays stale behind the surviving stream head.
	request := observationRequest("source", "fresh")
	request.Metadata.CollectedAt--
	fresh, err := ingest(t, store, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if comments, err := store.Comments(t.Context(), fresh.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("comments after pruning the stream: %#v %v", comments, err)
	}
	if marks, err := store.Marks(t.Context(), fresh.ContextID, review.DiffAll); err != nil || len(marks) != 0 {
		t.Fatalf("marks after pruning the stream: %#v %v", marks, err)
	}
	if item, err := store.Context(t.Context(), owner, fresh.ContextID, time.Now()); err != nil || !item.Stale {
		t.Fatalf("pruned stream head forgotten: %#v %v", item, err)
	}
}

func (harness Harness[S]) pipedObservationsHaveContextReviews(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	piped := func(id string) ingestion.Request {
		request := observationRequest("source", id)
		request.Metadata.RepositoryKey, request.Metadata.CheckoutKey = "", ""
		return request
	}
	first, err := ingest(t, store, owner, piped("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ingest(t, store, owner, piped("second"))
	if err != nil {
		t.Fatal(err)
	}
	comment := review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "piped", Status: "open", CreatedAt: 1}
	if err := store.PutComment(t.Context(), first.ContextID, comment); err != nil {
		t.Fatal(err)
	}
	if comments, err := store.Comments(t.Context(), second.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("piped review leaked across captures: %#v %v", comments, err)
	}
	if err := store.PutComment(t.Context(), second.ContextID, comment); err == nil {
		t.Fatal("comment ID claimed by another stream")
	}
	if deleted, err := store.DeleteComment(t.Context(), second.ContextID, "comment"); err != nil || deleted {
		t.Fatalf("deleted another stream's comment: %v %v", deleted, err)
	}
}

func (harness Harness[S]) marksCarryWhileFilePatchIsUnchanged(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	first, err := ingest(t, store, owner, observationRequest("source", "first"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ObservationSnapshot(t.Context(), owner, first.ContextID, review.DiffAll)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutMark(t.Context(), first.ContextID, review.ReviewMark{Scope: review.DiffAll, VersionID: snapshot.VersionID, FileID: "file", FileVersion: "all-fingerprint"}); err != nil {
		t.Fatal(err)
	}
	same := observationRequest("source", "same")
	same.Scopes[0].Snapshot.Files[0].Fingerprint = "touched-fingerprint"
	carried, err := ingest(t, store, owner, same)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.ObservationSnapshot(t.Context(), owner, carried.ContextID, review.DiffAll)
	if err != nil {
		t.Fatal(err)
	}
	marks, err := store.Marks(t.Context(), carried.ContextID, review.DiffAll)
	want := review.ReviewMark{DiffID: current.ID, VersionID: current.VersionID, FileID: "file", FileVersion: "touched-fingerprint", Scope: review.DiffAll}
	if err != nil || len(marks) != 1 || marks[0] != want {
		t.Fatalf("unchanged patch mark not projected: %#v %v", marks, err)
	}
	changed := observationRequest("source", "changed")
	changed.Scopes[0].Patches = map[string]review.FilePatch{"file": {Patch: "changed-patch"}}
	edited, err := ingest(t, store, owner, changed)
	if err != nil {
		t.Fatal(err)
	}
	if marks, err := store.Marks(t.Context(), edited.ContextID, review.DiffAll); err != nil || len(marks) != 0 {
		t.Fatalf("changed patch kept its mark: %#v %v", marks, err)
	}
	if err := store.ClearMarks(t.Context(), edited.ContextID, review.DiffAll); err != nil {
		t.Fatal(err)
	}
	if marks, err := store.Marks(t.Context(), first.ContextID, review.DiffAll); err != nil || len(marks) != 1 {
		t.Fatalf("clearing removed a mark hidden in that snapshot: %#v %v", marks, err)
	}
}

func (harness Harness[S]) replaceCommentCannotResurrectDeletedComment(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	binding, err := ingest(t, store, owner, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(t.Context(), binding.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	comments, err := store.Comments(t.Context(), binding.ContextID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("stored comment: %#v %v", comments, err)
	}
	edited := comments[0]
	edited.Status = "resolved"
	if replaced, err := store.ReplaceComment(t.Context(), binding.ContextID, edited); err != nil || !replaced {
		t.Fatalf("replace existing comment: %v %v", replaced, err)
	}
	if deleted, err := store.DeleteComment(t.Context(), binding.ContextID, "comment"); err != nil || !deleted {
		t.Fatalf("delete comment: %v %v", deleted, err)
	}
	// An edit read before the delete must not recreate the comment.
	if replaced, err := store.ReplaceComment(t.Context(), binding.ContextID, edited); err != nil || replaced {
		t.Fatalf("replace deleted comment: %v %v", replaced, err)
	}
	if comments, err := store.Comments(t.Context(), binding.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("deleted comment resurrected: %#v %v", comments, err)
	}
}

func (harness Harness[S]) cancelledCommentWriteStopsWithoutChange(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	binding, err := ingest(t, store, owner, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	comment := review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}
	if err := store.PutComment(cancelled, binding.ContextID, comment); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write = %v, want context.Canceled", err)
	}
	if _, err := store.Comments(cancelled, binding.ContextID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read = %v, want context.Canceled", err)
	}
	if comments, err := store.Comments(t.Context(), binding.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("cancelled write stored a comment: %#v %v", comments, err)
	}
}
