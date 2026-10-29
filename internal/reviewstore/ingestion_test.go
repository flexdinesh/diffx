package reviewstore

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func observationRequest(source, submission string) ingestion.Request {
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission,
		Metadata: ingestion.Metadata{SourceID: source, Hostname: "agent-host", RepositoryKey: "repo-key", RepositoryName: "my-repo", CheckoutKey: "checkout", Root: "/deleted/checkout", WorktreeName: "sandbox", Branch: "feature", CollectedAt: 123, Trigger: "agent-hook"},
		Scopes: []ingestion.Scope{
			{Snapshot: review.RepositoryDiff{Mode: review.DiffAll, Revision: "all-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "all-fingerprint"}}}, Patches: map[string]review.FilePatch{"file": {Patch: "all-patch", Contents: &review.FileContents{Before: "old", After: "new"}}}},
			{Snapshot: review.RepositoryDiff{Mode: review.DiffStaged, Revision: "staged-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "staged-fingerprint"}}}, Patches: map[string]review.FilePatch{"file": {Patch: "staged-patch"}}},
			{Snapshot: review.RepositoryDiff{Mode: review.DiffUnstaged, Revision: "unstaged-revision", Files: []review.ChangedFile{}}, Patches: map[string]review.FilePatch{}},
		},
	}
}

// The shared contract checks the semantic outcome; rows prove nothing partial persists.
func TestObservationRollbackLeavesNoRows(t *testing.T) {
	store := openTestStore(t)
	user := testUser(t, store, "rollback")
	request := observationRequest("source", "submission")
	request.Scopes[1].Patches = nil
	if _, err := store.Ingest(user.ID, request); err == nil {
		t.Fatal("incomplete scope accepted")
	}
	for _, table := range []string{"observations", "observation_repositories", "observation_scopes", "observation_submissions", "observation_identities", "observation_stream_heads", "contexts", "diffs", "diff_versions", "diff_files", "reviews"} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial commit in %s: %d, %v", table, count, err)
		}
	}
}

// The shared contract checks pruned reads; rows prove nothing is orphaned.
func TestObservationPruningLeavesNoOrphanedRows(t *testing.T) {
	store := openTestStore(t)
	user := testUser(t, store, "expiry")
	request := observationRequest("source", "first")
	request.ContentHash = strings.Repeat("a", 64)
	first, err := store.Ingest(user.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged} {
		snapshot, err := store.ObservationSnapshot(t.Context(), user.ID, first.ContextID, mode)
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
	other := testUser(t, store, "other-user")
	if _, err := store.Observation(other.ID, first.ContextID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user metadata: %v", err)
	}
	if metadata, err := store.Observation(user.ID, first.ContextID); err != nil || metadata.SourceID != "source" {
		t.Fatalf("metadata: %#v %v", metadata, err)
	}
	expire(t, store, first.ContextID, time.Now().Add(-time.Second))
	if _, err := store.Observation(user.ID, first.ContextID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired metadata: %v", err)
	}
	fresh := request
	fresh.SubmissionID = "fresh"
	if _, err := store.Ingest(user.ID, fresh); err != nil {
		t.Fatal(err)
	}
	if err := store.PruneExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"diffs", "diff_versions", "reviews", "comments", "marks", "observation_scopes", "observation_submissions", "observation_identities", "observations", "contexts"} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		expected := 1
		if table == "diffs" || table == "diff_versions" || table == "reviews" || table == "observation_scopes" {
			expected = 3
		}
		if table == "comments" || table == "marks" {
			expected = 2
		}
		if count != expected {
			t.Fatalf("orphaned %s: %d want %d", table, count, expected)
		}
	}
}
