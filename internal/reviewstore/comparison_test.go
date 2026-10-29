package reviewstore

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestLegacyComparisonReadsPreservePersistedIdentityAndReplay(t *testing.T) {
	for _, test := range []struct {
		name                   string
		branch, allOnly, patch bool
		want                   review.ComparisonKind
	}{
		{"HEAD", false, false, false, review.ComparisonUncommitted},
		{"branch checkout", true, false, false, review.ComparisonCombined},
		{"branch objects", true, true, false, review.ComparisonCommitted},
		{"patch", false, true, true, review.ComparisonPatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t)
			owner := testUser(t, store, "legacy")
			r := localRequest("original")
			if test.branch {
				r.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
			}
			if test.allOnly {
				r.Scopes = r.Scopes[:1]
			}
			if test.patch {
				r.Scopes[0].Snapshot.Source = "stdin"
			}
			original, hash, err := ingestion.Payload(r)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := store.Ingest(owner.ID, r)
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err := store.db.QueryRow(`SELECT manifest FROM diff_versions WHERE diff_id=?`, binding.ContextID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(before, "comparisonKind") {
				t.Fatal("legacy manifest normalized on write")
			}
			current, err := store.ObservationSnapshot(t.Context(), owner.ID, binding.ContextID, review.DiffAll)
			if err != nil || current.ComparisonKind != test.want {
				t.Fatalf("current: %+v, %v", current, err)
			}
			version, err := store.StoredVersion(t.Context(), owner.ID, current.ID, current.VersionID, time.Now())
			if err != nil || version.ComparisonKind != test.want {
				t.Fatalf("version: %+v, %v", version, err)
			}
			item, err := store.Context(t.Context(), owner.ID, binding.ContextID, time.Now())
			if err != nil || item.ScopeSummaries[0].ComparisonKind != test.want {
				t.Fatalf("summary: %+v, %v", item, err)
			}
			var after, recordedHash string
			if err := store.db.QueryRow(`SELECT manifest FROM diff_versions WHERE diff_id=?`, binding.ContextID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`SELECT payload_hash FROM ingestion_records WHERE owner_id=? AND submission_id=?`, owner.ID, r.SubmissionID).Scan(&recordedHash); err != nil {
				t.Fatal(err)
			}
			if before != after || recordedHash != hash {
				t.Fatal("read mutated legacy persisted identity")
			}
			reencoded, err := json.Marshal(r)
			if err != nil || !bytes.Equal(original, reencoded) {
				t.Fatalf("submitted request changed: %v", err)
			}
			replay, err := store.Ingest(owner.ID, r)
			if err != nil || replay.ContextID != binding.ContextID {
				t.Fatalf("replay: %+v, %v", replay, err)
			}
			retried, err := store.Context(t.Context(), owner.ID, replay.ContextID, time.Now())
			if err != nil || !reflect.DeepEqual(item.ExpiresAt, retried.ExpiresAt) || item.LastSubmittedAt != retried.LastSubmittedAt {
				t.Fatalf("retry changed retention: %+v, %v", retried, err)
			}
		})
	}
}
