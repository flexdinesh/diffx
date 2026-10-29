package storetest

import (
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func (harness Harness[S]) sessionAssociationsReuseReviewAndFilterEveryCollection(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "sessions")
	first := observationRequest("source", "a")
	first.ContentHash = strings.Repeat("a", 64)
	first.Metadata.Agent, first.Metadata.RunID = "codex", "session-a"
	first.Metadata.CollectedAt = 100
	binding, err := ingest(t, store, user, first)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutComment(t.Context(), binding.ContextID, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "keep", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutMark(t.Context(), binding.ContextID, review.ReviewMark{Scope: review.DiffAll, FileID: "file", FileVersion: "all-fingerprint"}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.SubmissionID = "b"
	second.Metadata.Agent, second.Metadata.RunID = "claude", "session-b"
	second.Metadata.AgentSession = &ingestion.AgentSession{Harness: "claude", ID: "session-b", Name: "Review boundaries"}
	second.Metadata.TriggerRoot = "/originating/worktree"
	second.Metadata.CollectedAt = 200
	reused, err := ingest(t, store, user, second)
	if err != nil || reused.ContextID != binding.ContextID {
		t.Fatalf("session duplicated snapshot: %#v %v", reused, err)
	}
	for _, filter := range []ingestion.Filter{
		{RunID: "session-a"}, {RunID: "session-b"}, {Harness: "codex", SessionID: "session-a"},
		{Harness: "claude", SessionID: "session-b", SessionName: "Review boundaries"},
		{Query: "boundaries"}, {Query: "originating/worktree"},
	} {
		items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", filter)
		if err != nil || len(items) != 1 || items[0].ID != binding.ContextID {
			t.Fatalf("filter %#v: %#v %v", filter, items, err)
		}
	}
	items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{Harness: "codex", SessionID: "session-b"})
	if err != nil || len(items) != 0 {
		t.Fatalf("mixed unrelated session facts: %#v %v", items, err)
	}
	context, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || len(context.Sessions) != 2 || context.Metadata.RunID != "session-a" || context.Metadata.AgentSession != nil {
		t.Fatalf("provenance: %#v %v", context, err)
	}
	comments, err := store.Comments(t.Context(), binding.ContextID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments: %#v %v", comments, err)
	}
	marks, err := store.Marks(t.Context(), binding.ContextID, review.DiffAll)
	if err != nil || len(marks) != 1 {
		t.Fatalf("marks: %#v %v", marks, err)
	}
	other := harness.Owner(t, store, "other-session-user")
	items, err = store.ObservationContexts(t.Context(), other, 100, 0, "", ingestion.Filter{SessionID: "session-a"})
	if err != nil || len(items) != 0 {
		t.Fatalf("cross-user session: %#v %v", items, err)
	}
}

func (harness Harness[S]) sessionNameUpdatesByCollectionOrderAndSource(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "session-names")
	request := observationRequest("source-a", "initial")
	request.ContentHash = strings.Repeat("a", 64)
	request.Metadata.AgentSession = &ingestion.AgentSession{Harness: "codex", ID: "native-id", Name: "Initial"}
	request.Metadata.CollectedAt = 100
	binding, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		id, name string
		time     int64
	}{
		{"renamed", "Current", 300}, {"delayed", "Old", 200}, {"unnamed", "", 400},
	} {
		fresh := request
		fresh.SubmissionID = step.id
		fresh.Metadata.AgentSession = &ingestion.AgentSession{Harness: "codex", ID: "native-id", Name: step.name}
		fresh.Metadata.CollectedAt = step.time
		if _, err := ingest(t, store, user, fresh); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingest(t, store, user, request); err != nil {
		t.Fatal(err)
	}
	otherSource := request
	otherSource.SubmissionID = "other-source"
	otherSource.Metadata.SourceID = "source-b"
	otherSource.Metadata.AgentSession = &ingestion.AgentSession{Harness: "codex", ID: "native-id", Name: "Other installation"}
	otherSource.Metadata.CollectedAt = 500
	if _, err := ingest(t, store, user, otherSource); err != nil {
		t.Fatal(err)
	}
	context, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || len(context.Sessions) != 1 || context.Sessions[0].SourceID != "source-a" || context.Sessions[0].Name != "Current" || context.Sessions[0].FirstObservedAt != 100 || context.Sessions[0].LastObservedAt != 400 {
		t.Fatalf("mutable session: %#v %v", context, err)
	}
	for _, filter := range []ingestion.Filter{{SourceID: "source-a", SessionName: "Current"}, {Query: "Current"}} {
		items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", filter)
		if err != nil || len(items) != 1 || items[0].ID != binding.ContextID {
			t.Fatalf("updated name filter: %#v %v", items, err)
		}
	}
}

func (harness Harness[S]) comparisonPoliciesHaveSeparateReviewsAndDurableHeads(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "policies")
	legacy := observationRequest("source", "legacy")
	legacy.ContentHash = strings.Repeat("a", 64)
	first, err := ingest(t, store, user, legacy)
	if err != nil {
		t.Fatal(err)
	}
	head := legacy
	head.SubmissionID = "explicit-head"
	head.Metadata.Comparison = &ingestion.Comparison{Kind: "working-tree", BaseRef: "HEAD", BaseCommit: "head"}
	headBinding, err := ingest(t, store, user, head)
	if err != nil || headBinding.ContextID != first.ContextID {
		t.Fatalf("legacy HEAD identity: %#v %v", headBinding, err)
	}
	branch := legacy
	branch.SubmissionID = "branch-main"
	branch.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: "base-a", MergeBase: "merge-a"}
	second, err := ingest(t, store, user, branch)
	if err != nil || second.ContextID == first.ContextID {
		t.Fatalf("branch identity: %#v %v", second, err)
	}
	other := branch
	other.SubmissionID = "branch-other"
	other.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/other"}
	third, err := ingest(t, store, user, other)
	if err != nil || third.ContextID == second.ContextID {
		t.Fatalf("base policy identity: %#v %v", third, err)
	}
	updated := branch
	updated.SubmissionID = "base-advanced"
	updated.Metadata.Comparison = &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: "base-b", MergeBase: "merge-b"}
	updated.ContentHash = strings.Repeat("b", 64)
	updated.Metadata.CollectedAt++
	latest, err := ingest(t, store, user, updated)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		id    string
		stale bool
	}{{first.ContextID, false}, {second.ContextID, true}, {third.ContextID, false}, {latest.ContextID, false}} {
		context, err := store.Context(t.Context(), user, check.id, time.Now())
		if err != nil || context.Stale != check.stale {
			t.Fatalf("policy freshness: %#v %v", context, err)
		}
	}
	if err := store.DeleteContext(t.Context(), user, latest.ContextID); err != nil {
		t.Fatal(err)
	}
	context, err := store.Context(t.Context(), user, second.ContextID, time.Now())
	if err != nil || !context.Stale {
		t.Fatalf("deleted latest regressed head: %#v %v", context, err)
	}
}

func (harness Harness[S]) retentionConfigurationPreservesExistingExpiryAndReplay(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "retention")
	request := observationRequest("source", "original")
	request.ProtocolVersion = ingestion.ProtocolVersion
	request.ContentHash = strings.Repeat("a", 64)
	binding, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	duration := 2 * 24 * time.Hour
	store = harness.Reopen(t, store, duration)
	if _, err := ingest(t, store, user, request); err != nil {
		t.Fatal(err)
	}
	replay, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || *replay.ExpiresAt != *original.ExpiresAt {
		t.Fatalf("changed existing expiry: %#v %v", replay, err)
	}
	request.SubmissionID = "fresh"
	if _, err := ingest(t, store, user, request); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || *refreshed.ExpiresAt-refreshed.LastSubmittedAt != duration.Milliseconds() {
		t.Fatalf("fresh retention: %#v %v", refreshed, err)
	}
}

// A container execution ID is provenance, not an agent session without a harness.
func (harness Harness[S]) containerRunWithoutHarnessRemainsSearchableWithoutSession(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "container-run")
	request := observationRequest("source", "container")
	request.Metadata.RunID = "container-execution"
	binding, err := ingest(t, store, user, request)
	if err != nil {
		t.Fatal(err)
	}
	context, err := store.Context(t.Context(), user, binding.ContextID, time.Now())
	if err != nil || len(context.Sessions) != 0 {
		t.Fatalf("container mistaken for session: %#v %v", context, err)
	}
	for _, filter := range []ingestion.Filter{{RunID: "container-execution"}, {Query: "container-execution"}} {
		items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", filter)
		if err != nil || len(items) != 1 || items[0].ID != binding.ContextID {
			t.Fatalf("container provenance lost: %#v %v", items, err)
		}
	}
	items, err := store.ObservationContexts(t.Context(), user, 100, 0, "", ingestion.Filter{SessionID: "container-execution"})
	if err != nil || len(items) != 0 {
		t.Fatalf("container filter mistaken for session: %#v %v", items, err)
	}
}
