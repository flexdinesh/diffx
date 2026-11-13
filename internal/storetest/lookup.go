package storetest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func (harness Harness[S]) commentAndStreamLookupStayOwnerScopedAndRetained(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	foreign := harness.Owner(t, store, "foreign")
	older := observationRequest("source", "older")
	newer := observationRequest("source", "newer")
	newer.Metadata.CollectedAt = 456
	for index := range newer.Scopes {
		newer.Scopes[index].Snapshot.Files = []review.ChangedFile{}
		newer.Scopes[index].Patches = map[string]review.FilePatch{}
	}
	first, err := ingest(t, store, owner, older)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ingest(t, store, owner, newer)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ingest(t, store, owner, observationRequest("other-source", "other"))
	if err != nil {
		t.Fatal(err)
	}
	foreignBinding, err := ingest(t, store, foreign, observationRequest("source", "foreign"))
	if err != nil {
		t.Fatal(err)
	}
	stream := ingestion.StreamKey(first.ContextID, older.Metadata)
	otherStream := ingestion.StreamKey(other.ContextID, observationRequest("other-source", "other").Metadata)
	if stream == otherStream || ingestion.StreamKey(second.ContextID, newer.Metadata) != stream {
		t.Fatalf("stream fixtures: %q %q", stream, otherStream)
	}
	for contextID, id := range map[string]string{first.ContextID: "abcd1234-b", other.ContextID: "abcd1234-a", second.ContextID: "abcd9999", foreignBinding.ContextID: "abcd1234-c"} {
		if err := store.PutComment(t.Context(), contextID, review.ReviewComment{ID: id, Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.CommentStreams(t.Context(), owner, "abcd1234", 10)
	want := []reviewdata.CommentStream{{CommentID: "abcd1234-a", StreamKey: otherStream}, {CommentID: "abcd1234-b", StreamKey: stream}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("comment streams = %#v, %v; want %#v", got, err, want)
	}
	if got, err := store.CommentStreams(t.Context(), owner, "abcd1234", 1); err != nil || len(got) != 1 || got[0].CommentID != "abcd1234-a" {
		t.Fatalf("limited comment streams = %#v, %v", got, err)
	}
	if got, err := store.CommentStreams(t.Context(), owner, "abcd9999", 10); err != nil || len(got) != 1 || got[0].StreamKey != stream {
		t.Fatalf("full comment ID = %#v, %v", got, err)
	}
	if got, err := store.CommentStreams(t.Context(), owner, "ABCD1234", 10); err != nil || len(got) != 0 {
		t.Fatalf("prefixes are case-sensitive: %#v, %v", got, err)
	}

	// The most recently submitted retained context wins.
	now := time.Now()
	harness.Submitted(t, store, first.ContextID, now.Add(-time.Hour))
	harness.Submitted(t, store, second.ContextID, now)
	if id, err := store.StreamContext(t.Context(), owner, stream); err != nil || id != second.ContextID {
		t.Fatalf("stream context = %q, %v; want %q", id, err, second.ContextID)
	}
	harness.Submitted(t, store, first.ContextID, now.Add(time.Hour))
	if id, err := store.StreamContext(t.Context(), owner, stream); err != nil || id != first.ContextID {
		t.Fatalf("resubmitted stream context = %q, %v; want %q", id, err, first.ContextID)
	}
	if _, err := store.StreamContext(t.Context(), foreign, otherStream); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("foreign stream: %v", err)
	}
	harness.Expire(t, store, first.ContextID, now.Add(-time.Minute))
	if id, err := store.StreamContext(t.Context(), owner, stream); err != nil || id != second.ContextID {
		t.Fatalf("stream context after expiry = %q, %v", id, err)
	}
	harness.Expire(t, store, second.ContextID, now.Add(-time.Minute))
	if _, err := store.StreamContext(t.Context(), owner, stream); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("expired stream: %v", err)
	}
}
