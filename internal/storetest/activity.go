package storetest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func (harness Harness[S]) activityProjectionPreservesHeadOrderAndObservationProvenance(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "activity")
	other := harness.Owner(t, store, "other")
	publish := func(request ingestion.Request) reviewdata.Binding {
		t.Helper()
		binding, err := ingest(t, store, owner, request)
		if err != nil {
			t.Fatal(err)
		}
		return binding
	}
	assertActivity := func(id string, activity, captured int64, stale bool) {
		t.Helper()
		item, err := store.Context(t.Context(), owner, id, time.Now())
		if err != nil || item.ActivityAt != activity || item.Metadata == nil || item.Metadata.ActivityAt != captured || item.Stale != stale {
			t.Fatalf("activity projection: %+v, %v; want activity %d, captured %d, stale %v", item, err, activity, captured, stale)
		}
	}
	request := localRequest("original")
	request.ContentHash = strings.Repeat("a", 64)
	request.Metadata.CollectedAt, request.Metadata.ActivityAt = 100, 90
	original := publish(request)
	assertActivity(original.ContextID, 90, 90, false)
	fresh := request
	fresh.SubmissionID = "fresh"
	fresh.Metadata.CollectedAt, fresh.Metadata.ActivityAt = 200, 190
	reused := publish(fresh)
	if reused.ContextID != original.ContextID {
		t.Fatal("activity metadata changed content identity")
	}
	assertActivity(original.ContextID, 190, 90, false)
	late := fresh
	late.SubmissionID = "late"
	late.Metadata.CollectedAt, late.Metadata.ActivityAt = 150, 999
	publish(late)
	assertActivity(original.ContextID, 190, 90, false)
	if _, err := ingest(t, store, owner, request); err != nil {
		t.Fatal(err)
	}
	assertActivity(original.ContextID, 190, 90, false)
	// Equal collection time advances by arrival sequence, even when the
	// activity estimate decreases. Activity never elects the stream head.
	equal := fresh
	equal.SubmissionID = "equal"
	equal.Metadata.ActivityAt = 180
	publish(equal)
	assertActivity(original.ContextID, 180, 90, false)
	legacy := equal
	legacy.SubmissionID = "without-activity"
	legacy.Metadata.CollectedAt, legacy.Metadata.ActivityAt = 300, 0
	publish(legacy)
	assertActivity(original.ContextID, 100, 90, false)
	newer := fresh
	newer.SubmissionID, newer.ContentHash = "different-content", strings.Repeat("b", 64)
	newer.Metadata.CollectedAt, newer.Metadata.ActivityAt = 400, 390
	latest := publish(newer)
	assertActivity(original.ContextID, 90, 90, true)
	assertActivity(latest.ContextID, 390, 390, false)
	if _, err := ingest(t, store, other, request); err != nil {
		t.Fatal(err)
	}
	assertActivity(latest.ContextID, 390, 390, false)
	if _, err := store.Context(t.Context(), other, latest.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("activity leaked across owners: %v", err)
	}
	store = harness.Reopen(t, store, retention)
	assertActivity(original.ContextID, 90, 90, true)
	assertActivity(latest.ContextID, 390, 390, false)
}
