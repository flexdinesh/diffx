package storetest

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

func commitObservationRequest(submission string) ingestion.Request {
	request := observationRequest("source", submission)
	head := strings.Repeat("b", 40)
	request.Metadata.Head = &head
	request.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("1", 40), MergeBase: strings.Repeat("1", 40)}
	merge := ingestion.Commit{Commit: review.Commit{OID: strings.Repeat("a", 40), Parents: []string{strings.Repeat("1", 40), strings.Repeat("2", 40)}, Subject: "Merge main"}}
	diff := review.RepositoryDiff{Source: "local", Mode: review.DiffCommit, ComparisonKind: review.ComparisonCommit, Head: &head, Revision: "commit-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "commit-fingerprint", Additions: 3, Deletions: 1}}}
	commit := ingestion.Commit{Commit: review.Commit{OID: head, Parents: []string{merge.OID}, PatchID: strings.Repeat("c", 40), Subject: "Change file", Body: "Why.", AuthorName: "Ada", AuthoredAt: 1000, CommittedAt: 2000}, Snapshot: &diff, Patches: map[string]review.FilePatch{"file": {Patch: "commit-patch"}}}
	request.Commits, request.CommitsTotal = []ingestion.Commit{merge, commit}, 5
	return request
}

func (harness Harness[S]) observationCommitsStoreAndRead(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "commits")
	other := harness.Owner(t, store, "other")
	binding, err := ingest(t, store, user, commitObservationRequest("first"))
	if err != nil {
		t.Fatal(err)
	}
	commits, summary, err := store.ObservationCommits(t.Context(), user, binding.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	if summary != (review.CommitRange{Count: 2, Omitted: 3}) || len(commits) != 2 {
		t.Fatalf("summary %+v commits %+v", summary, commits)
	}
	if !commits[0].Merge || commits[0].FileCount != 0 || commits[1].Merge || commits[1].Subject != "Change file" || commits[1].Body != "Why." || commits[1].FileCount != 1 || commits[1].Additions != 3 || commits[1].Deletions != 1 {
		t.Fatalf("commits %+v", commits)
	}
	snapshot, err := store.ObservationCommitSnapshot(t.Context(), user, binding.ContextID, commits[1].OID)
	if err != nil || snapshot.Mode != review.DiffCommit || len(snapshot.Files) != 1 || snapshot.VersionID == "" {
		t.Fatalf("commit snapshot %+v %v", snapshot, err)
	}
	version, err := store.ObservationCommitFileVersion(t.Context(), user, binding.ContextID, commits[1].OID, "file")
	if err != nil || version.Fingerprint == nil || *version.Fingerprint != "commit-fingerprint" || version.VersionID != snapshot.VersionID {
		t.Fatalf("file version %+v %v", version, err)
	}
	patch, err := store.ObservationCommitPatch(t.Context(), user, binding.ContextID, commits[1].OID, "file", "commit-fingerprint")
	if err != nil || patch.Patch != "commit-patch" {
		t.Fatalf("patch %+v %v", patch, err)
	}
	if _, err := store.ObservationCommitSnapshot(t.Context(), user, binding.ContextID, commits[0].OID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("merge commit diff: %v", err)
	}
	if _, _, err := store.ObservationCommits(t.Context(), other, binding.ContextID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("other owner listed commits: %v", err)
	}
	if _, err := store.ObservationCommitSnapshot(t.Context(), other, binding.ContextID, commits[1].OID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("other owner read commit diff: %v", err)
	}
	item, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || item.Commits == nil || *item.Commits != summary {
		t.Fatalf("context commits %+v %v", item.Commits, err)
	}
	if item.ChangedFileCount == nil || *item.ChangedFileCount != 1 || len(item.ScopeSummaries) != 3 {
		t.Fatalf("commit diffs leaked into scopes or counts: %+v %+v", item.ChangedFileCount, item.ScopeSummaries)
	}
	plain, err := ingest(t, store, user, observationRequest("source", "plain"))
	if err != nil {
		t.Fatal(err)
	}
	if item, err := store.Context(t.Context(), user, plain.ContextID, time.Now()); err != nil || item.Commits != nil {
		t.Fatalf("observation without commits: %+v %v", item.Commits, err)
	}
	if commits, summary, err := store.ObservationCommits(t.Context(), user, plain.ContextID); err != nil || len(commits) != 0 || summary.Count != 0 {
		t.Fatalf("empty commits: %+v %+v %v", commits, summary, err)
	}
}

func (harness Harness[S]) observationCommitsExpireAndDeleteWithObservation(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "commit-expiry")
	oid := strings.Repeat("b", 40)
	commitDiff := func(contextID string) error {
		t.Helper()
		_, err := store.ObservationCommitSnapshot(t.Context(), user, contextID, oid)
		return err
	}
	request := commitObservationRequest("first")
	request.ContentHash = strings.Repeat("a", 64)
	first, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitDiff(first.ContextID); err != nil {
		t.Fatalf("commit diff not stored: %v", err)
	}
	// A fresh matching submission extends every diff's retention, commits included.
	cutoff := time.Now().Add(time.Minute)
	harness.Expire(t, store, first.ContextID, cutoff)
	fresh := request
	fresh.SubmissionID = "fresh"
	if again, err := ingest(t, store, user, fresh); err != nil || again.ContextID != first.ContextID {
		t.Fatalf("reuse %+v %v", again, err)
	}
	if err := store.PruneExpired(cutoff); err != nil {
		t.Fatal(err)
	}
	if err := commitDiff(first.ContextID); err != nil {
		t.Fatalf("reuse left the commit diff on the old expiry: %v", err)
	}
	for mode := range first.DiffIDs {
		if _, err := store.ObservationSnapshot(t.Context(), user, first.ContextID, mode); err != nil {
			t.Fatalf("reuse left scope %s on the old expiry: %v", mode, err)
		}
	}
	if err := store.PruneExpired(time.Now().Add(retention + time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := commitDiff(first.ContextID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("pruning kept commit diffs: %v", err)
	}
	if _, _, err := store.ObservationCommits(t.Context(), user, first.ContextID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("pruning kept commits: %v", err)
	}
	second, err := ingest(t, store, user, commitObservationRequest("second"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteContext(t.Context(), user, second.ContextID); err != nil {
		t.Fatal(err)
	}
	if err := commitDiff(second.ContextID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("deleting the observation kept commit diffs: %v", err)
	}
	if _, _, err := store.ObservationCommits(t.Context(), user, second.ContextID); !errors.Is(err, reviewdata.ErrNotFound) {
		t.Fatalf("deleting the observation kept commits: %v", err)
	}
}
