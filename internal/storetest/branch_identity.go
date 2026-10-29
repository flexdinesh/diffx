package storetest

import (
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

func branchIdentityRequest(submission, label, id, content string, collected int64) ingestion.Request {
	request := observationRequest("source", submission)
	request.Metadata.Branch, request.Metadata.BranchID = label, id
	request.Metadata.CollectedAt = collected
	request.ContentHash = strings.Repeat(content, 64)
	return request
}

func (harness Harness[S]) reusedBranchNameKeepsIndependentReviewAndFreshness(t *testing.T) {
	store := harness.open(t)
	user := harness.Owner(t, store, "branch-reuse")
	first, err := ingest(t, store, user, branchIdentityRequest("original", "feature", "original-id", "a", 100))
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := ingest(t, store, user, branchIdentityRequest("renamed", "renamed", "original-id", "b", 200))
	if err != nil {
		t.Fatal(err)
	}
	reused, err := ingest(t, store, user, branchIdentityRequest("reused", "feature", "fresh-id", "a", 300))
	if err != nil || reused.ContextID == first.ContextID {
		t.Fatalf("reused branch inherited old review: %#v, %v", reused, err)
	}
	for _, check := range []struct {
		id    string
		stale bool
	}{{first.ContextID, true}, {renamed.ContextID, false}, {reused.ContextID, false}} {
		context, err := store.Context(t.Context(), user, check.id, time.Now())
		if err != nil || context.Stale != check.stale {
			t.Fatalf("independent branch freshness %s: %#v, %v", check.id, context, err)
		}
	}
}
