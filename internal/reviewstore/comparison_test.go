package reviewstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestLegacyComparisonReadsPreserveQueuedBytesAndReplay(t *testing.T) {
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
			r := queuedRequest("original")
			if test.branch {
				r.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
			}
			if test.allOnly {
				r.Scopes = r.Scopes[:1]
			}
			if test.patch {
				r.Scopes[0].Snapshot.Source = "stdin"
			}
			original, hash, err := payloadBytes(r)
			if err != nil {
				t.Fatal(err)
			}
			job, err := store.Queue().Accept(t.Context(), owner.ID, r)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := store.Queue().Claim(t.Context(), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := store.Ingest(owner.ID, lease.Request)
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
			var after, payload, recordedHash string
			if err := store.db.QueryRow(`SELECT manifest FROM diff_versions WHERE diff_id=?`, binding.ContextID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`SELECT j.payload,r.payload_hash FROM ingestion_jobs j JOIN ingestion_records r ON r.sequence=j.sequence WHERE j.id=?`, job.ID).Scan(&payload, &recordedHash); err != nil {
				t.Fatal(err)
			}
			if before != after || payload != string(original) || recordedHash != hash {
				t.Fatal("read mutated legacy persisted identity")
			}
			reencoded, err := json.Marshal(lease.Request)
			if err != nil || !bytes.Equal(original, reencoded) {
				t.Fatalf("queued request changed: %v", err)
			}
			replay, err := store.Ingest(owner.ID, lease.Request)
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

func comparisonRequest() ingestion.Request {
	r := queuedRequest("five")
	head := strings.Repeat("c", 40)
	r.Metadata.Head = &head
	r.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
	for _, mode := range []review.DiffMode{review.DiffUncommitted, review.DiffCommitted} {
		scope := r.Scopes[0]
		scope.Snapshot.Mode = mode
		r.Scopes = append(r.Scopes, scope)
	}
	for i := range r.Scopes {
		r.Scopes[i].Snapshot.Head = &head
		r.Scopes[i].Snapshot.ComparisonKind = ingestion.LegacyComparisonKind(r.Scopes[i].Snapshot.Mode, "local", r.Metadata, []review.DiffMode{review.DiffStaged})
	}
	r.ContentHash = strings.Repeat("a", 64)
	return r
}

func TestCapturedScopeCountsIncludeCancellingAndDistinctPaths(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "counts")
	r := comparisonRequest()
	// Combined cancels completely, but three other comparisons share file.txt.
	r.Scopes[0].Snapshot.Files, r.Scopes[0].Patches = []review.ChangedFile{}, map[string]review.FilePatch{}
	r.Scopes[2].Snapshot.Files = []review.ChangedFile{{ID: "other", Path: "other.txt", Fingerprint: "other"}}
	r.Scopes[2].Patches = map[string]review.FilePatch{"other": {Patch: "other"}}
	binding, err := store.Ingest(owner.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner.ID, binding.ContextID, time.Now())
	if err != nil || item.ChangedFileCount == nil || *item.ChangedFileCount != 2 {
		t.Fatalf("union: %+v, %v", item, err)
	}
	want := []review.ScopeSummary{
		{Scope: review.DiffAll, ComparisonKind: review.ComparisonCombined, FileCount: 0},
		{Scope: review.DiffStaged, ComparisonKind: review.ComparisonStaged, FileCount: 1},
		{Scope: review.DiffUnstaged, ComparisonKind: review.ComparisonUnstaged, FileCount: 1},
		{Scope: review.DiffUncommitted, ComparisonKind: review.ComparisonUncommitted, FileCount: 1},
		{Scope: review.DiffCommitted, ComparisonKind: review.ComparisonCommitted, FileCount: 1},
	}
	if !reflect.DeepEqual(item.ScopeSummaries, want) {
		t.Fatalf("summaries: %+v", item.ScopeSummaries)
	}
	listed, err := store.Contexts(t.Context(), owner.ID, 10, 0, "", time.Now())
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].ScopeSummaries, want) || *listed[0].ChangedFileCount != 2 {
		t.Fatalf("catalog: %+v, %v", listed, err)
	}
}

func TestExplicitComparisonKindsRemainDistinctAndOverrideLegacyInference(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "explicit")
	r := comparisonRequest()
	r.Scopes = r.Scopes[:1]
	// A producer may capture only Combined. No fabricated staging scopes needed.
	first, err := store.Ingest(owner.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	r.SubmissionID = "committed"
	r.Scopes[0].Snapshot.ComparisonKind = review.ComparisonCommitted
	second, err := store.Ingest(owner.ID, r)
	if err != nil || first.ContextID == second.ContextID {
		t.Fatalf("different comparison deduped: %+v, %v", second, err)
	}
	for _, test := range []struct {
		id   string
		kind review.ComparisonKind
	}{{first.ContextID, review.ComparisonCombined}, {second.ContextID, review.ComparisonCommitted}} {
		current, err := store.ObservationSnapshot(t.Context(), owner.ID, test.id, review.DiffAll)
		if err != nil || current.ComparisonKind != test.kind {
			t.Fatalf("explicit current kind: %+v, %v", current, err)
		}
		version, err := store.StoredVersion(t.Context(), owner.ID, current.ID, current.VersionID, time.Now())
		if err != nil || version.ComparisonKind != test.kind {
			t.Fatalf("explicit historical kind: %+v, %v", version, err)
		}
		item, err := store.Context(t.Context(), owner.ID, test.id, time.Now())
		if err != nil || item.ScopeSummaries[0].ComparisonKind != test.kind {
			t.Fatalf("explicit summary kind: %+v, %v", item, err)
		}
	}
}

func TestNewScopesPersistReviewStateThroughDedupeLateArrivalAndRetention(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "new-scopes")
	r := comparisonRequest()
	first, err := store.Ingest(owner.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []review.DiffMode{review.DiffUncommitted, review.DiffCommitted} {
		if err := store.PutComment(t.Context(), first.ContextID, review.ReviewComment{ID: string(mode), Scope: mode, Path: "file.txt", Body: string(mode), Status: "open", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutMark(t.Context(), first.ContextID, review.ReviewMark{Scope: mode, FileID: "file", FileVersion: "all-fingerprint"}); err != nil {
			t.Fatal(err)
		}
	}
	r.SubmissionID, r.Metadata.CollectedAt = "fresh", 200
	reused, err := store.Ingest(owner.ID, r)
	if err != nil || reused.ContextID != first.ContextID {
		t.Fatalf("dedupe: %+v, %v", reused, err)
	}
	r.SubmissionID, r.ContentHash, r.Metadata.CollectedAt = "late", strings.Repeat("b", 64), 150
	late, err := store.Ingest(owner.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner.ID, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("late arrival became head: %+v, %v", item, err)
	}
	if _, err := store.db.Exec(`UPDATE diffs SET expires_at=1 WHERE id IN (SELECT diff_id FROM observation_scopes WHERE context_id=?)`, first.ContextID); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []review.DiffMode{review.DiffUncommitted, review.DiffCommitted} {
		if _, err := store.ObservationSnapshot(t.Context(), owner.ID, first.ContextID, mode); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired scope readable: %v", err)
		}
		marks, err := store.Marks(t.Context(), late.ContextID, mode)
		if err != nil || len(marks) != 1 || marks[0].Scope != mode {
			t.Fatalf("marks: %+v, %v", marks, err)
		}
	}
	comments, err := store.Comments(t.Context(), late.ContextID)
	if err != nil || len(comments) != 2 || comments[0].Scope == comments[1].Scope {
		t.Fatalf("comments: %+v, %v", comments, err)
	}
	item, err = store.Context(t.Context(), owner.ID, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("prune promoted late arrival: %+v, %v", item, err)
	}
}
