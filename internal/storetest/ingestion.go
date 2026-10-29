package storetest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func (harness Harness[S]) observationRetryAndIndependentSources(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "observations")
	request := observationRequest("container-a", "submission")
	first, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := ingest(t, store, user, request)
	if err != nil || retry.ContextID != first.ContextID || len(retry.DiffIDs) != 3 {
		t.Fatalf("retry: %#v, %v", retry, err)
	}
	request.Metadata.Branch = "other"
	if _, err := ingest(t, store, user, request); !errors.Is(err, reviewdata.ErrSubmissionConflict) {
		t.Fatalf("conflict: %v", err)
	}
	request = observationRequest("container-b", "submission")
	second, err := ingest(t, store, user, request)
	if err != nil || second.ContextID == first.ContextID || first.RepositoryID == nil || second.RepositoryID == nil || *first.RepositoryID != *second.RepositoryID {
		t.Fatalf("source provenance: %#v, %v", second, err)
	}
	request.SubmissionID = "another"
	third, err := ingest(t, store, user, request)
	if err != nil || third.ContextID == second.ContextID {
		t.Fatalf("identical independent submission: %#v, %v", third, err)
	}
	other := harness.Owner(t, store, "other-user")
	if _, err := store.Context(t.Context(), other, first.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("cross-user metadata: %v", err)
	}
	if _, err := store.ContextBinding(t.Context(), other, first.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("cross-user binding: %v", err)
	}
	if _, err := store.ObservationSnapshot(t.Context(), other, first.ContextID, review.DiffAll); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("cross-user snapshot: %v", err)
	}
	if _, err := store.ObservationPatch(t.Context(), other, first.ContextID, review.DiffAll, "file", "all-fingerprint"); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("cross-user patch: %v", err)
	}
}

func (harness Harness[S]) observationFreshnessUsesCollectionOrderAndRetries(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "freshness")
	requests := []ingestion.Request{
		observationRequest("source", "old"),
		observationRequest("source", "new"),
		observationRequest("source", "late"),
		observationRequest("source", "equal-time"),
	}
	requests[0].Metadata.CollectedAt = 100
	requests[0].Metadata.RunID = "old-run"
	requests[1].Metadata.CollectedAt = 200
	for index := range requests[1].Scopes {
		requests[1].Scopes[index].Snapshot.Files = []review.ChangedFile{}
		requests[1].Scopes[index].Patches = map[string]review.FilePatch{}
	}
	requests[2].Metadata.CollectedAt = 150
	requests[3].Metadata.CollectedAt = 200
	bindings := make([]reviewdata.Binding, 0, len(requests))
	expected := [][]bool{{false}, {true, false}, {true, false, true}, {true, true, true, false}}
	for index, request := range requests {
		binding, err := ingest(t, store, user, request)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
		for previous, binding := range bindings {
			item, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
			if err != nil || item.Stale != expected[index][previous] {
				t.Fatalf("freshness after %s for %s: %#v, %v", request.SubmissionID, requests[previous].SubmissionID, item, err)
			}
		}
	}
	// Equal collection times use arrival order; replaying an earlier arrival
	// must not make it latest again.
	retry, err := ingest(t, store, user, requests[1])
	if err != nil || retry.ContextID != bindings[1].ContextID {
		t.Fatalf("retry: %#v, %v", retry, err)
	}
	for index, binding := range bindings {
		item, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
		if err != nil || item.Stale != (index < len(bindings)-1) {
			t.Fatalf("freshness for %s: %#v, %v", requests[index].SubmissionID, item, err)
		}
	}
	filtered, err := store.ObservationContexts(t.Context(), user, 1, 0, "", ingestion.Filter{RunID: "old-run"})
	if err != nil || len(filtered) != 1 || !filtered[0].Stale {
		t.Fatalf("staleness with newer observation outside search: %#v, %v", filtered, err)
	}
	all, err := store.Contexts(t.Context(), user, 100, 0, "", time.Now())
	if err != nil || len(all) != len(bindings) {
		t.Fatalf("catalog: %#v, %v", all, err)
	}
	page, err := store.Contexts(t.Context(), user, 1, all[0].LastSubmittedAt, all[0].ID, time.Now())
	if err != nil || len(page) != 1 || page[0].ID != all[1].ID || page[0].Stale != all[1].Stale {
		t.Fatalf("paginated freshness: %#v, %v", page, err)
	}
}

func (harness Harness[S]) observationFreshnessTracksDeduplicatedSubmissions(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "dedupe-freshness")
	steps := []struct {
		id, content, latest string
		collectedAt         int64
	}{
		{"a-first", "a", "a", 100},
		{"b-first", "b", "b", 200},
		{"empty-first", "empty", "empty", 300},
		{"a-fresh", "a", "a", 400},
		{"empty-late", "empty", "a", 350},
		{"b-equal-time", "b", "b", 400},
		{"a-fresh", "a", "b", 400}, // A retry cannot supersede B's equal-time arrival.
	}
	contexts := make(map[string]string)
	metadata := make(map[string]ingestion.Metadata)
	for _, step := range steps {
		request := observationRequest("source", step.id)
		request.Metadata.CollectedAt = step.collectedAt
		request.Metadata.RunID = step.id
		if step.content == "empty" {
			for index := range request.Scopes {
				request.Scopes[index].Snapshot.Files = []review.ChangedFile{}
				request.Scopes[index].Patches = map[string]review.FilePatch{}
			}
		} else {
			request.ContentHash = strings.Repeat(step.content, 64)
		}
		binding, err := ingest(t, store, user, request)
		if err != nil {
			t.Fatal(err)
		}
		if id, exists := contexts[step.content]; exists {
			if binding.ContextID != id {
				t.Fatalf("dedupe for %s created %s, want %s", step.id, binding.ContextID, id)
			}
		} else {
			contexts[step.content] = binding.ContextID
			metadata[step.content] = request.Metadata
		}
		for content, id := range contexts {
			item, err := store.Context(t.Context(), user, id, time.Now())
			if err != nil || item.Stale != (content != step.latest) || item.Metadata == nil || *item.Metadata != metadata[content] {
				t.Fatalf("freshness after %s for %s: %#v, %v", step.id, content, item, err)
			}
		}
	}
}

func (harness Harness[S]) observationFreshnessKeepsStreamsSeparate(t *testing.T) {
	for _, field := range []string{"owner", "source", "repository", "checkout", "branch", "no repository", "no checkout"} {
		t.Run(field, func(t *testing.T) {
			store := harness.open(t)
			user := harness.Owner(t, store, "streams")
			request := observationRequest("source", "old")
			if field == "no repository" {
				request.Metadata.RepositoryKey = ""
			}
			if field == "no checkout" {
				request.Metadata.CheckoutKey = ""
			}
			old, err := ingest(t, store, user, request)
			if err != nil {
				t.Fatal(err)
			}
			request.SubmissionID = "new"
			request.Metadata.CollectedAt++
			ownerID := user
			switch field {
			case "owner":
				ownerID = harness.Owner(t, store, "another-owner")
			case "source":
				request.Metadata.SourceID = "another-source"
			case "repository":
				request.Metadata.RepositoryKey = "another-repository"
			case "checkout":
				request.Metadata.CheckoutKey = "another-checkout"
			case "branch":
				request.Metadata.Branch = "another-branch"
			}
			newest, err := ingest(t, store, ownerID, request)
			if err != nil {
				t.Fatal(err)
			}
			for _, identity := range []struct{ ownerID, contextID string }{{user, old.ContextID}, {ownerID, newest.ContextID}} {
				item, err := store.Context(t.Context(), identity.ownerID, identity.contextID, time.Now())
				if err != nil || item.Stale {
					t.Fatalf("independent stream became stale: %#v, %v", item, err)
				}
			}
		})
	}
}

func (harness Harness[S]) observationFreshnessSurvivesPruningLatestSnapshot(t *testing.T) {
	for _, test := range []struct {
		name                string
		firstTime, lateTime int64
	}{{"newer collection", 100, 150}, {"equal-time arrival", 200, 199}} {
		t.Run(test.name, func(t *testing.T) {
			store := harness.open(t)
			user := harness.Owner(t, store, "pruned-freshness")
			request := observationRequest("source", "a-first")
			request.ContentHash = strings.Repeat("a", 64)
			request.Metadata.CollectedAt = test.firstTime
			first, err := ingest(t, store, user, request)
			if err != nil {
				t.Fatal(err)
			}
			newer := observationRequest("source", "b-first")
			newer.ContentHash = strings.Repeat("b", 64)
			newer.Metadata.CollectedAt = 200
			latest, err := ingest(t, store, user, newer)
			if err != nil {
				t.Fatal(err)
			}
			request.SubmissionID = "a-delayed"
			request.Metadata.CollectedAt = test.lateTime
			late, err := ingest(t, store, user, request)
			if err != nil || late.ContextID != first.ContextID {
				t.Fatalf("delayed dedupe: %#v, %v", late, err)
			}
			harness.Expire(t, store, latest.ContextID, time.Now().Add(-time.Second))
			if err := store.PruneExpired(time.Now()); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"after prune", "after restart"} {
				if phase == "after restart" {
					store = harness.Reopen(t, store, retention)
				}
				item, err := store.Context(t.Context(), user, first.ContextID, time.Now())
				if err != nil || !item.Stale {
					t.Fatalf("old snapshot promoted %s: %#v, %v", phase, item, err)
				}
				items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{SourceID: "source"})
				if err != nil || len(items) != 1 || !items[0].Stale {
					t.Fatalf("catalog freshness %s: %#v, %v", phase, items, err)
				}
			}
			request.SubmissionID = "a-fresh"
			request.Metadata.CollectedAt = 201
			fresh, err := ingest(t, store, user, request)
			if err != nil || fresh.ContextID != first.ContextID {
				t.Fatalf("fresh dedupe: %#v, %v", fresh, err)
			}
			item, err := store.Context(t.Context(), user, first.ContextID, time.Now())
			if err != nil || item.Stale {
				t.Fatalf("fresh snapshot remained stale: %#v, %v", item, err)
			}
		})
	}
}

func (harness Harness[S]) observationRollbackOnIncompleteScope(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "rollback")
	request := observationRequest("source", "submission")
	request.Scopes[1].Patches = nil
	if _, err := ingest(t, store, user, request); err == nil {
		t.Fatal("incomplete scope accepted")
	}
	if items, err := store.Contexts(t.Context(), user, 100, 0, "", time.Now()); err != nil || len(items) != 0 {
		t.Fatalf("partial commit in catalog: %#v, %v", items, err)
	}
	if items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{SourceID: "source"}); err != nil || len(items) != 0 {
		t.Fatalf("partial commit in observation catalog: %#v, %v", items, err)
	}
	// A recorded submission would conflict; a recorded stream head would make
	// the earlier collection stale.
	corrected := observationRequest("source", "submission")
	corrected.Metadata.CollectedAt--
	binding, err := ingest(t, store, user, corrected)
	if err != nil {
		t.Fatalf("partial commit kept submission: %v", err)
	}
	item, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || item.Stale {
		t.Fatalf("partial commit kept stream head: %#v, %v", item, err)
	}
	if comments, err := store.Comments(t.Context(), binding.ContextID); err != nil || len(comments) != 0 {
		t.Fatalf("partial commit kept review: %#v, %v", comments, err)
	}
}

func (harness Harness[S]) observationReopensWithoutCheckoutWithinRetention(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "durable")
	request := observationRequest("source", "request")
	request.Metadata.Root = filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(request.Metadata.Root, 0700); err != nil {
		t.Fatal(err)
	}
	binding, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	newer := observationRequest("source", "newer")
	newer.Metadata.CollectedAt++
	newest, err := ingest(t, store, user, newer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(request.Metadata.Root); err != nil {
		t.Fatal(err)
	}
	store = harness.Reopen(t, store, retention)
	if err := store.PruneExpired(time.Now().Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	retry, err := ingest(t, store, user, request)
	if err != nil || retry.ContextID != binding.ContextID {
		t.Fatalf("durable replay: %#v, %v", retry, err)
	}
	item, err := store.Context(t.Context(), user, binding.ContextID, time.Now().Add(24*time.Hour))
	if err != nil || !item.Stale || item.Kind != "observation" || item.Metadata == nil || item.Metadata.SourceID != "source" || item.Root == nil || *item.Root != request.Metadata.Root || item.ExpiresAt == nil {
		t.Fatalf("metadata: %#v, %v", item, err)
	}
	latest, err := store.Context(t.Context(), user, newest.ContextID, time.Now())
	if err != nil || latest.Stale {
		t.Fatalf("latest after reopening and retry: %#v, %v", latest, err)
	}
	snapshot, err := store.ObservationSnapshot(t.Context(), user, binding.ContextID, review.DiffStaged)
	if err != nil || snapshot.ID != binding.DiffIDs[review.DiffStaged] || snapshot.Revision != "staged-revision" {
		t.Fatalf("snapshot: %#v, %v", snapshot, err)
	}
	patch, err := store.ObservationPatch(t.Context(), user, binding.ContextID, review.DiffAll, "file", "all-fingerprint")
	if err != nil || patch.Contents == nil || patch.Contents.After != "new" {
		t.Fatalf("contents: %#v, %v", patch, err)
	}
	rebound, err := store.ContextBinding(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || len(rebound.DiffIDs) != 3 || rebound.DiffIDs[review.DiffStaged] != snapshot.ID {
		t.Fatalf("binding: %#v, %v", rebound, err)
	}
}

func (harness Harness[S]) observationReviewScopesAndMetadataSearch(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "search")
	binding, err := ingest(t, store, user, observationRequest("source", "request"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ObservationSnapshot(t.Context(), user, binding.ContextID, review.DiffStaged)
	if err != nil {
		t.Fatal(err)
	}
	comment := review.ReviewComment{ID: "comment", DiffID: snapshot.ID, VersionID: snapshot.VersionID, Scope: review.DiffStaged, Path: "file.txt", Body: "review", CreatedAt: 1}
	if err := store.PutComment(t.Context(), binding.ContextID, comment); err != nil {
		t.Fatal(err)
	}
	comments, err := store.Comments(t.Context(), binding.ContextID)
	if err != nil || len(comments) != 1 || comments[0].VersionID != snapshot.VersionID {
		t.Fatalf("comments: %#v, %v", comments, err)
	}
	mark := review.ReviewMark{DiffID: snapshot.ID, VersionID: snapshot.VersionID, Scope: review.DiffStaged, FileID: "file", FileVersion: "staged-fingerprint"}
	if err := store.PutMark(t.Context(), binding.ContextID, mark); err != nil {
		t.Fatal(err)
	}
	marks, err := store.Marks(t.Context(), binding.ContextID, review.DiffStaged)
	if err != nil || len(marks) != 1 {
		t.Fatalf("marks: %#v, %v", marks, err)
	}
	filters := []ingestion.Filter{{Query: "AGENT-HOST"}, {Repository: "my-repo", Branch: "feature", Worktree: "sandbox", Hostname: "agent-host", SourceID: "source"}}
	for _, filter := range filters {
		items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", filter)
		if err != nil || len(items) != 1 || items[0].Metadata == nil {
			t.Fatalf("filter %#v: %#v, %v", filter, items, err)
		}
	}
	items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{Branch: "absent"})
	if err != nil || len(items) != 0 {
		t.Fatalf("nonmatching filter: %#v, %v", items, err)
	}
	if err := store.ClearMarks(t.Context(), binding.ContextID, review.DiffStaged); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.DeleteComment(t.Context(), binding.ContextID, "comment"); err != nil || !deleted {
		t.Fatalf("delete comment: %v,%v", deleted, err)
	}
}

func (harness Harness[S]) observationDedupePreservesReviewAndRefreshesOnlyFreshSubmissions(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "dedupe")
	request := observationRequest("source", "first")
	request.ContentHash = strings.Repeat("a", 64)
	first, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, review.DiffStaged)
	if err != nil {
		t.Fatal(err)
	}
	comment := review.ReviewComment{ID: "retained", DiffID: snapshot.ID, VersionID: snapshot.VersionID, Scope: review.DiffStaged, Path: "file.txt", Body: "check", CreatedAt: 1}
	if err := store.PutComment(t.Context(), first.ContextID, comment); err != nil {
		t.Fatal(err)
	}
	mark := review.ReviewMark{DiffID: snapshot.ID, VersionID: snapshot.VersionID, Scope: review.DiffStaged, FileID: "file", FileVersion: "staged-fingerprint"}
	if err := store.PutMark(t.Context(), first.ContextID, mark); err != nil {
		t.Fatal(err)
	}
	oldExpiry := time.Now().Add(time.Hour).UnixMilli()
	harness.Expire(t, store, first.ContextID, time.UnixMilli(oldExpiry))
	harness.Submitted(t, store, first.ContextID, time.UnixMilli(1))
	retry, err := ingest(t, store, user, request)
	if err != nil || retry.ContextID != first.ContextID {
		t.Fatalf("retry: %#v %v", retry, err)
	}
	item, err := store.Context(t.Context(), user, first.ContextID, time.Now())
	if err != nil || item.ExpiresAt == nil || *item.ExpiresAt != oldExpiry || item.LastSubmittedAt != 1 {
		t.Fatalf("retry refreshed TTL: %#v %v", item, err)
	}

	fresh := request
	fresh.SubmissionID = "second"
	fresh.Metadata.CollectedAt++
	fresh.Scopes = append([]ingestion.Scope(nil), request.Scopes...)
	fresh.Scopes[0].Snapshot.Revision = "stat-only-change"
	reused, err := ingest(t, store, user, fresh)
	if err != nil || reused.ContextID != first.ContextID {
		t.Fatalf("dedupe: %#v %v", reused, err)
	}
	item, err = store.Context(t.Context(), user, first.ContextID, time.Now())
	if err != nil || item.ExpiresAt == nil || *item.ExpiresAt-item.LastSubmittedAt != retention.Milliseconds() || *item.ExpiresAt <= oldExpiry {
		t.Fatalf("fresh TTL: %#v %v", item, err)
	}
	// A scope left on the old expiry would be pruned with it.
	if err := store.PruneExpired(time.UnixMilli(oldExpiry)); err != nil {
		t.Fatal(err)
	}
	for _, scope := range request.Scopes {
		if _, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, scope.Snapshot.Mode); err != nil {
			t.Fatalf("scope retention %s: %v", scope.Snapshot.Mode, err)
		}
	}
	comments, err := store.Comments(t.Context(), first.ContextID)
	if err != nil || len(comments) != 1 || comments[0].VersionID != snapshot.VersionID {
		t.Fatalf("review lost: %#v %v", comments, err)
	}
	marks, err := store.Marks(t.Context(), first.ContextID, review.DiffStaged)
	if err != nil || len(marks) != 1 {
		t.Fatalf("mark lost: %#v %v", marks, err)
	}
	original, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, review.DiffStaged)
	if err != nil || original.VersionID != snapshot.VersionID || original.Revision != snapshot.Revision {
		t.Fatalf("snapshot replaced: %#v %v", original, err)
	}
	for _, replay := range []ingestion.Request{request, fresh} {
		binding, err := ingest(t, store, user, replay)
		if err != nil || binding.ContextID != first.ContextID {
			t.Fatalf("alias replay: %#v %v", binding, err)
		}
	}
	fresh.Metadata.Branch = "conflict"
	if _, err := ingest(t, store, user, fresh); !errors.Is(err, reviewdata.ErrSubmissionConflict) {
		t.Fatalf("alias conflict: %v", err)
	}
}

func (harness Harness[S]) observationIdentitySeparatesProvenanceHeadAndContent(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "identity")
	base := observationRequest("source", "base")
	base.ContentHash = strings.Repeat("a", 64)
	first, err := ingest(t, store, user, base)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ingestion.Request){
		"content":    func(r *ingestion.Request) { r.ContentHash = strings.Repeat("b", 64) },
		"head":       func(r *ingestion.Request) { head := "new-head"; r.Metadata.Head = &head },
		"branch":     func(r *ingestion.Request) { r.Metadata.Branch = "other" },
		"checkout":   func(r *ingestion.Request) { r.Metadata.CheckoutKey = "other" },
		"repository": func(r *ingestion.Request) { r.Metadata.RepositoryKey = "other" },
		"source":     func(r *ingestion.Request) { r.Metadata.SourceID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			request.SubmissionID = name
			change(&request)
			binding, err := ingest(t, store, user, request)
			if err != nil || binding.ContextID == first.ContextID {
				t.Fatalf("identity merged: %#v %v", binding, err)
			}
		})
	}
	other := harness.Owner(t, store, "other-owner")
	binding, err := ingest(t, store, other, base)
	if err != nil || binding.ContextID == first.ContextID {
		t.Fatalf("owner merged: %#v %v", binding, err)
	}
}

func (harness Harness[S]) emptyObservationDedupeWithoutProducerHash(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "empty")
	request := observationRequest("source", "first")
	for i := range request.Scopes {
		request.Scopes[i].Snapshot.Files = []review.ChangedFile{}
		request.Scopes[i].Patches = map[string]review.FilePatch{}
	}
	first, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	request.SubmissionID = "second"
	request.Metadata.CollectedAt++
	request.Scopes[0], request.Scopes[2] = request.Scopes[2], request.Scopes[0]
	second, err := ingest(t, store, user, request)
	if err != nil || second.ContextID != first.ContextID {
		t.Fatalf("empty dedupe: %#v %v", second, err)
	}
}

func (harness Harness[S]) observationDedupeConcurrentFreshSubmissions(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "concurrent")
	var group sync.WaitGroup
	ids := make(chan string, 8)
	for i := range 8 {
		group.Go(func() {
			request := observationRequest("source", string(rune('a'+i)))
			request.ContentHash = strings.Repeat("a", 64)
			binding, err := ingest(t, store, user, request)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- binding.ContextID
		})
	}
	group.Wait()
	close(ids)
	var id string
	for next := range ids {
		if id != "" && next != id {
			t.Fatalf("concurrent duplicates: %s %s", id, next)
		}
		id = next
	}
	// Every submission is a recorded alias: changed input under its ID conflicts.
	for i := range 8 {
		request := observationRequest("source", string(rune('a'+i)))
		request.ContentHash = strings.Repeat("a", 64)
		request.Metadata.Branch = "conflict"
		if _, err := ingest(t, store, user, request); !errors.Is(err, reviewdata.ErrSubmissionConflict) {
			t.Fatalf("submission alias %s: %v", request.SubmissionID, err)
		}
	}
}

func (harness Harness[S]) observationExpiryHidesReadsAndPrunesEveryScope(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "expiry")
	request := observationRequest("source", "first")
	request.ContentHash = strings.Repeat("a", 64)
	first, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged} {
		snapshot, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, mode)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.PutComment(t.Context(), first.ContextID, review.ReviewComment{ID: string(mode), Scope: mode, VersionID: snapshot.VersionID, Path: "file.txt", Body: "check", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutMark(t.Context(), first.ContextID, review.ReviewMark{Scope: mode, VersionID: snapshot.VersionID, FileID: "file"}); err != nil {
			t.Fatal(err)
		}
	}
	harness.Expire(t, store, first.ContextID, time.Now().Add(-time.Second))
	if _, err := store.Context(t.Context(), user, first.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrExpired) {
		t.Fatalf("expired context: %v", err)
	}
	if _, err := store.ContextBinding(t.Context(), user, first.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrExpired) {
		t.Fatalf("expired binding: %v", err)
	}
	if _, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, review.DiffAll); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("expired snapshot: %v", err)
	}
	if _, err := store.ObservationPatch(t.Context(), user, first.ContextID, review.DiffStaged, "file", "staged-fingerprint"); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("expired patch: %v", err)
	}
	items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{Repository: "my-repo"})
	if err != nil || len(items) != 0 {
		t.Fatalf("expired filtered catalog: %#v %v", items, err)
	}
	items, err = store.Contexts(t.Context(), user, 100, 0, "", time.Now())
	if err != nil || len(items) != 0 {
		t.Fatalf("expired catalog: %#v %v", items, err)
	}
	comments, err := store.Comments(t.Context(), first.ContextID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("expired comments visible: %#v %v", comments, err)
	}
	marks, err := store.Marks(t.Context(), first.ContextID, review.DiffStaged)
	if err != nil || len(marks) != 0 {
		t.Fatalf("expired marks visible: %#v %v", marks, err)
	}
	if _, err := ingest(t, store, user, request); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("expired replay resurrected: %v", err)
	}
	fresh := request
	fresh.SubmissionID = "fresh"
	second, err := ingest(t, store, user, fresh)
	if err != nil || second.ContextID == first.ContextID {
		t.Fatalf("fresh review after expiry: %#v %v", second, err)
	}
	if err := store.PruneExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	// Review state spans the stream; pruning an older snapshot keeps it.
	comments, err = store.Comments(t.Context(), second.ContextID)
	if err != nil || len(comments) != 2 {
		t.Fatalf("stream comments lost with pruned snapshot: %#v %v", comments, err)
	}
	marks, err = store.Marks(t.Context(), second.ContextID, review.DiffStaged)
	if err != nil || len(marks) != 1 || marks[0].DiffID != second.DiffIDs[review.DiffStaged] {
		t.Fatalf("unchanged file mark not carried: %#v %v", marks, err)
	}
	marks, err = store.Marks(t.Context(), second.ContextID, review.DiffAll)
	if err != nil || len(marks) != 1 || marks[0].DiffID != second.DiffIDs[review.DiffAll] {
		t.Fatalf("unchanged all-scope mark not carried: %#v %v", marks, err)
	}
	if _, err := store.Context(t.Context(), user, first.ContextID, time.Now()); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("pruned context: %v", err)
	}
	items, err = store.Contexts(t.Context(), user, 100, 0, "", time.Now())
	if err != nil || len(items) != 1 || items[0].ID != second.ContextID {
		t.Fatalf("catalog after prune: %#v %v", items, err)
	}
	for mode, id := range second.DiffIDs {
		if snapshot, err := store.ObservationSnapshot(t.Context(), user, second.ContextID, mode); err != nil || snapshot.ID != id {
			t.Fatalf("fresh scope %s pruned: %#v %v", mode, snapshot, err)
		}
	}
}
