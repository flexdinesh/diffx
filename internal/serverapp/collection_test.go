package serverapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

func TestPublishedNewestUsesActivityThenStableStreamIdentity(t *testing.T) {
	changed, clean := 1, 0
	streamA, streamB := "stream-a", "stream-b"
	contexts := []contextservice.Context{
		{ID: "older", StreamID: &streamB, LastChangedAt: 100, ChangedFileCount: &changed, Availability: "available"},
		{ID: "newest", StreamID: &streamA, LastChangedAt: 200, ChangedFileCount: &changed, Availability: "available"},
		{ID: "tie", StreamID: &streamB, LastChangedAt: 200, ChangedFileCount: &changed, Availability: "available"},
		{ID: "clean", LastChangedAt: 900, ChangedFileCount: &clean, Availability: "available"},
		{ID: "unknown", LastChangedAt: 999, Availability: "available"},
		{ID: "stale", LastChangedAt: 1000, ChangedFileCount: &changed, Availability: "available", Stale: true},
		{ID: "unavailable", LastChangedAt: 1001, ChangedFileCount: &changed, Availability: "unavailable"},
	}
	for _, reverse := range []bool{false, true} {
		published := Published{}
		for index := range contexts {
			if reverse {
				index = len(contexts) - 1 - index
			}
			published.Submissions = append(published.Submissions, contextservice.Submission{Context: contexts[index]})
		}
		newest, ok := published.Newest()
		if !ok || newest.Context.ID != "newest" {
			t.Fatalf("activity selection (reverse %v): %+v, %v", reverse, newest, ok)
		}
	}
}

func TestOlderCollectionRemainsSuccessfulWithoutRegressingStreamHead(t *testing.T) {
	store, err := reviewstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner, err := store.User("local", "Local")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.New(store, owner)
	defer service.Close()
	request := ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: "later-clock", ContentHash: strings.Repeat("a", 64),
		Metadata: ingestion.Metadata{SourceID: "source", RepositoryKey: "repository", CheckoutKey: "checkout", Branch: "main", Root: "/gone", Trigger: "manual", CollectedAt: 200, ActivityAt: 190},
		Scopes:   []ingestion.Scope{{Snapshot: review.RepositoryDiff{Source: "local", Root: "/gone", Branch: "main", Mode: review.DiffAll, Revision: "later", Files: []review.ChangedFile{}}, Patches: map[string]review.FilePatch{}}},
	}
	later, err := service.Ingest(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.SubmissionID, request.ContentHash = "earlier-clock", strings.Repeat("b", 64)
	request.Metadata.CollectedAt, request.Metadata.ActivityAt = 100, 90
	published, err := Publish(t.Context(), service, Collection{Requests: []ingestion.Request{request}})
	if err != nil || len(published.Submissions) != 1 || !published.Submissions[0].Context.Stale {
		t.Fatalf("older publication: %+v, %v", published, err)
	}
	fallback, ok := published.Newest()
	if !ok || fallback.Context.ID != published.Submissions[0].Context.ID {
		t.Fatalf("successful older collection lost its default: %+v, %v", fallback, ok)
	}
	handler := NewCollections(service, func(ctx context.Context) (Collection, error) {
		request.SubmissionID = "earlier-clock-again"
		return Collection{Requests: []ingestion.Request{request}}, nil
	}, later.Context.ID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v2/collections", nil))
	if response.Code != http.StatusOK || handler.current.get() != fallback.Context.ID {
		t.Fatalf("successful older collection response/default: %d %s %s", response.Code, response.Body.String(), handler.current.get())
	}
	head, err := service.Get(t.Context(), later.Context.ID)
	if err != nil || head.Stale || head.LatestContextID == nil || *head.LatestContextID != later.Context.ID {
		t.Fatalf("activity default changed stream freshness: %+v, %v", head, err)
	}
}

func TestPublishedNewestPrefersCurrentCleanBeforeStaleChanged(t *testing.T) {
	changed, clean := 1, 0
	published := Published{Submissions: []contextservice.Submission{
		{Context: contextservice.Context{ID: "stale", Stale: true, Availability: "available", LastChangedAt: 900, ChangedFileCount: &changed}},
		{Context: contextservice.Context{ID: "current", Availability: "available", LastChangedAt: 100, ChangedFileCount: &clean}},
	}}
	newest, ok := published.Newest()
	if !ok || newest.Context.ID != "current" {
		t.Fatalf("stale changed context displaced current clean default: %+v, %v", newest, ok)
	}
	published.Submissions = append(published.Submissions[:1], contextservice.Submission{Context: contextservice.Context{ID: "stale-more-active", Stale: true, Availability: "available", LastChangedAt: 1000}})
	newest, ok = published.Newest()
	if !ok || newest.Context.ID != "stale-more-active" {
		t.Fatalf("all-stale fallback did not follow activity: %+v, %v", newest, ok)
	}
}

func TestPublishedNewestKeepsCleanAndPipedFallbacks(t *testing.T) {
	clean := 0
	for _, source := range []string{"local", "stdin"} {
		published := Published{Submissions: []contextservice.Submission{
			{Context: contextservice.Context{ID: "first", Source: source, Observation: &ingestion.Metadata{CollectedAt: 200}, ChangedFileCount: &clean, Availability: "available"}},
			{Context: contextservice.Context{ID: "last", Source: source, Observation: &ingestion.Metadata{CollectedAt: 100}, ChangedFileCount: &clean, Availability: "available"}},
		}}
		newest, ok := published.Newest()
		if !ok || newest.Context.ID != "first" {
			t.Fatalf("%s fallback: %+v, %v", source, newest, ok)
		}
	}
	if _, ok := (Published{}).Newest(); ok {
		t.Fatal("empty collection had a default")
	}
}
