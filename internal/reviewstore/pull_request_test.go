package reviewstore

import (
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

func TestPullRequestProjectionFollowsWinningSubmission(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "pull-requests")
	title := func(id string) (string, string) {
		t.Helper()
		item, err := store.Context(t.Context(), owner.ID, id, time.Now())
		if err != nil || item.Metadata == nil || item.Metadata.PullRequest == nil {
			t.Fatalf("pull request projection: %+v, %v", item, err)
		}
		return item.Metadata.PullRequest.Title, item.Metadata.PullRequest.State
	}
	request := queuedRequest("opened")
	request.ContentHash = strings.Repeat("a", 64)
	request.Metadata.CollectedAt = 100
	request.Metadata.PullRequest = &ingestion.PullRequest{Provider: "github", RepositoryID: 7, FullName: "octo/app", Number: 12, Title: "Draft title", State: "open"}
	original, err := store.Ingest(owner.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	edited := request
	edited.SubmissionID = "edited"
	edited.Metadata.CollectedAt = 200
	pull := *request.Metadata.PullRequest
	pull.Title, pull.State = "Final title", "merged"
	edited.Metadata.PullRequest = &pull
	reused, err := store.Ingest(owner.ID, edited)
	if err != nil || reused.ContextID != original.ContextID {
		t.Fatalf("metadata-only edit must reuse the observation: %+v, %v", reused, err)
	}
	if got, state := title(original.ContextID); got != "Final title" || state != "merged" {
		t.Fatalf("head shows %q %q, want latest submission", got, state)
	}
	// A delayed submission collected earlier never replaces the head's metadata.
	late := request
	late.SubmissionID = "late"
	late.Metadata.CollectedAt = 150
	if _, err := store.Ingest(owner.ID, late); err != nil {
		t.Fatal(err)
	}
	if got, _ := title(original.ContextID); got != "Final title" {
		t.Fatalf("late submission replaced head metadata: %q", got)
	}
	pushed := edited
	pushed.SubmissionID, pushed.ContentHash = "pushed", strings.Repeat("b", 64)
	pushed.Metadata.CollectedAt = 300
	pushedPull := pull
	pushedPull.Title = "After push"
	pushed.Metadata.PullRequest = &pushedPull
	latest, err := store.Ingest(owner.ID, pushed)
	if err != nil || latest.ContextID == original.ContextID {
		t.Fatalf("new content must create a snapshot: %+v, %v", latest, err)
	}
	// Historical snapshots keep their captured metadata.
	if got, _ := title(original.ContextID); got != "Draft title" {
		t.Fatalf("stale snapshot shows %q, want captured title", got)
	}
	if got, _ := title(latest.ContextID); got != "After push" {
		t.Fatalf("latest snapshot shows %q", got)
	}
}
