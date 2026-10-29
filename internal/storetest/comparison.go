package storetest

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func comparisonRequest() ingestion.Request {
	r := localRequest("five")
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

func (harness Harness[S]) capturedScopeCountsIncludeCancellingAndDistinctPaths(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "counts")
	r := comparisonRequest()
	// Combined cancels completely, but three other comparisons share file.txt.
	r.Scopes[0].Snapshot.Files, r.Scopes[0].Patches = []review.ChangedFile{}, map[string]review.FilePatch{}
	r.Scopes[2].Snapshot.Files = []review.ChangedFile{{ID: "other", Path: "other.txt", Fingerprint: "other"}}
	r.Scopes[2].Patches = map[string]review.FilePatch{"other": {Patch: "other"}}
	binding, err := ingest(t, store, owner, r)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner, binding.ContextID, time.Now())
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
	listed, err := store.Contexts(t.Context(), owner, 10, 0, "", time.Now())
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].ScopeSummaries, want) || *listed[0].ChangedFileCount != 2 {
		t.Fatalf("catalog: %+v, %v", listed, err)
	}
}

func (harness Harness[S]) explicitComparisonKindsRemainDistinctAndOverrideLegacyInference(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "explicit")
	r := comparisonRequest()
	r.Scopes = r.Scopes[:1]
	// A producer may capture only Combined. No fabricated staging scopes needed.
	first, err := ingest(t, store, owner, r)
	if err != nil {
		t.Fatal(err)
	}
	r.SubmissionID = "committed"
	r.Scopes[0].Snapshot.ComparisonKind = review.ComparisonCommitted
	second, err := ingest(t, store, owner, r)
	if err != nil || first.ContextID == second.ContextID {
		t.Fatalf("different comparison deduped: %+v, %v", second, err)
	}
	for _, test := range []struct {
		id   string
		kind review.ComparisonKind
	}{{first.ContextID, review.ComparisonCombined}, {second.ContextID, review.ComparisonCommitted}} {
		current, err := store.ObservationSnapshot(t.Context(), owner, test.id, review.DiffAll)
		if err != nil || current.ComparisonKind != test.kind {
			t.Fatalf("explicit current kind: %+v, %v", current, err)
		}
		version, err := store.StoredVersion(t.Context(), owner, current.ID, current.VersionID, time.Now())
		if err != nil || version.ComparisonKind != test.kind {
			t.Fatalf("explicit historical kind: %+v, %v", version, err)
		}
		item, err := store.Context(t.Context(), owner, test.id, time.Now())
		if err != nil || item.ScopeSummaries[0].ComparisonKind != test.kind {
			t.Fatalf("explicit summary kind: %+v, %v", item, err)
		}
	}
}

func (harness Harness[S]) newScopesPersistReviewStateThroughDedupeLateArrivalAndRetention(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "new-scopes")
	r := comparisonRequest()
	first, err := ingest(t, store, owner, r)
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
	reused, err := ingest(t, store, owner, r)
	if err != nil || reused.ContextID != first.ContextID {
		t.Fatalf("dedupe: %+v, %v", reused, err)
	}
	r.SubmissionID, r.ContentHash, r.Metadata.CollectedAt = "late", strings.Repeat("b", 64), 150
	late, err := ingest(t, store, owner, r)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Context(t.Context(), owner, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("late arrival became head: %+v, %v", item, err)
	}
	harness.Expire(t, store, first.ContextID, time.UnixMilli(1))
	if err := store.PruneExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []review.DiffMode{review.DiffUncommitted, review.DiffCommitted} {
		if _, err := store.ObservationSnapshot(t.Context(), owner, first.ContextID, mode); !errors.Is(err, reviewdata.ErrNotFound) {
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
	item, err = store.Context(t.Context(), owner, late.ContextID, time.Now())
	if err != nil || !item.Stale {
		t.Fatalf("prune promoted late arrival: %+v, %v", item, err)
	}
}
