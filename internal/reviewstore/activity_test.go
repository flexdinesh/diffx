package reviewstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

// The shared contract covers direct publication; this covers queued delivery.
func TestQueuedActivityProjectionPreservesHeadOrderAndObservationProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner := testUser(t, store, "activity")
	other := testUser(t, store, "other")
	publish := func(request ingestion.Request) Binding {
		t.Helper()
		if _, err := store.Queue().Accept(t.Context(), owner.ID, request); err != nil {
			t.Fatal(err)
		}
		delivery, err := store.Queue().Claim(t.Context(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := store.Ingest(owner.ID, delivery.Request)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Queue().Complete(t.Context(), delivery, binding.ContextID); err != nil {
			t.Fatal(err)
		}
		return binding
	}
	assertActivity := func(id string, activity, captured int64, stale bool) {
		t.Helper()
		item, err := store.Context(t.Context(), owner.ID, id, time.Now())
		if err != nil || item.ActivityAt != activity || item.Metadata == nil || item.Metadata.ActivityAt != captured || item.Stale != stale {
			t.Fatalf("activity projection: %+v, %v; want activity %d, captured %d, stale %v", item, err, activity, captured, stale)
		}
	}
	request := queuedRequest("original")
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
	if _, err := store.Ingest(owner.ID, request); err != nil {
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
	if _, err := store.Ingest(other.ID, request); err != nil {
		t.Fatal(err)
	}
	assertActivity(latest.ContextID, 390, 390, false)
	if _, err := store.Context(t.Context(), other.ID, latest.ContextID, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("activity leaked across owners: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertActivity(original.ContextID, 90, 90, true)
	assertActivity(latest.ContextID, 390, 390, false)
}

func TestOptionalActivityPreservesLegacyPayloadAndRetryHash(t *testing.T) {
	request := queuedRequest("legacy")
	raw, hash, err := ingestion.Payload(request)
	// Protocol-4 bytes captured before adding the optional activity field.
	const legacyHash = "d5c453b0eef29e58e4b8401b793752afc15849a1008a6fd660fa79cd04750299"
	if hash != legacyHash {
		t.Fatalf("legacy retry hash changed: %s, want %s", hash, legacyHash)
	}
	if err != nil || bytes.Contains(raw, []byte("activityAt")) {
		t.Fatalf("omitted activity changed wire shape: %s, %v", raw, err)
	}
	// A previously persisted request must hash identically after decoding.
	var decoded ingestion.Request
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	_, replayHash, err := ingestion.Payload(decoded)
	if err != nil || replayHash != hash {
		t.Fatalf("legacy replay hash changed: %q, %v", replayHash, err)
	}
	decoded.Metadata.ActivityAt = 1
	_, changedHash, err := ingestion.Payload(decoded)
	if err != nil || changedHash == hash {
		t.Fatal("activity mutation must conflict for exact retries")
	}
}
