package serverapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

func collectionService(t *testing.T) (*contextservice.Service, *reviewstore.Store) {
	t.Helper()
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, err := store.User("collection-test", "collection-test")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.NewScoped(t.Context(), store, user, contextservice.ScopeFor())
	t.Cleanup(func() { _ = service.Close() })
	return service, store
}

func runInitialCollection(t *testing.T, collections *serverapp.Collections) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan error, 1)
	done := make(chan error, 1)
	task := collections.Initial(func(_ serverapp.Published, err error) { completed <- err })
	go func() { done <- task(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("initial task shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("initial collection did not stop")
		}
	})
	return completed
}

func awaitCollection(t *testing.T, completed <-chan error) error {
	t.Helper()
	select {
	case err := <-completed:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("collection timed out")
		return nil
	}
}

func collectionResponse(t *testing.T, response *httptest.ResponseRecorder) []contextservice.Context {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("collection status %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Contexts []contextservice.Context `json:"contexts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Contexts) != 1 {
		t.Fatalf("published contexts: %#v", result.Contexts)
	}
	return result.Contexts
}

func TestInitialCollectionJoinsFirstRequestAndLaterRequestsCollectAgain(t *testing.T) {
	for _, finishedBeforeRequest := range []bool{false, true} {
		name := "request-during-startup"
		if finishedBeforeRequest {
			name = "browser-opens-after-startup"
		}
		t.Run(name, func(t *testing.T) {
			input := collectGit(t)
			service, _ := collectionService(t)
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			collections := serverapp.NewCollections(service, func(ctx context.Context) (serverapp.Collection, error) {
				call := calls.Add(1)
				if call == 1 {
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
						return serverapp.Collection{}, ctx.Err()
					}
				}
				fresh := input
				if call > 1 {
					fresh.SubmissionID = "explicit-sync"
					fresh.Metadata.CollectedAt++
				}
				return serverapp.Collection{Requests: []ingestion.Request{fresh}}, nil
			}, "")
			completed := runInitialCollection(t, collections)
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("initial collection did not start")
			}
			if finishedBeforeRequest {
				close(release)
				if err := awaitCollection(t, completed); err != nil {
					t.Fatal(err)
				}
			}
			first := httptest.NewRecorder()
			requestDone := make(chan struct{})
			go func() {
				collections.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v2/collections?initial=true", nil))
				close(requestDone)
			}()
			if !finishedBeforeRequest {
				close(release)
				if err := awaitCollection(t, completed); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-requestDone:
			case <-time.After(5 * time.Second):
				t.Fatal("first request did not join startup")
			}
			firstContexts := collectionResponse(t, first)
			if got := calls.Load(); got != 1 {
				t.Fatalf("first browser request recollected: %d calls", got)
			}
			second := httptest.NewRecorder()
			collections.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v2/collections", nil))
			secondContexts := collectionResponse(t, second)
			if got := calls.Load(); got != 2 {
				t.Fatalf("explicit sync did not recollect: %d calls", got)
			}
			if firstContexts[0].ID != secondContexts[0].ID {
				t.Fatal("unchanged explicit sync should reuse the observation")
			}
		})
	}
}

func TestInitialCollectionFailureIsReportedThenRetryCollectsAgain(t *testing.T) {
	input := collectGit(t)
	service, _ := collectionService(t)
	var calls atomic.Int32
	collections := serverapp.NewCollections(service, func(context.Context) (serverapp.Collection, error) {
		if calls.Add(1) == 1 {
			return serverapp.Collection{}, errors.New("checkout unavailable")
		}
		return serverapp.Collection{Requests: []ingestion.Request{input}}, nil
	}, "")
	completed := runInitialCollection(t, collections)
	if err := awaitCollection(t, completed); err == nil || !strings.Contains(err.Error(), "checkout unavailable") {
		t.Fatalf("initial failure: %v", err)
	}
	first := httptest.NewRecorder()
	collections.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v2/collections?initial=true", nil))
	if first.Code != http.StatusUnprocessableEntity || !strings.Contains(first.Body.String(), "checkout unavailable") {
		t.Fatalf("first request lost initial failure: %d %s", first.Code, first.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("first request retried startup unexpectedly: %d calls", got)
	}
	retry := httptest.NewRecorder()
	collections.ServeHTTP(retry, httptest.NewRequest(http.MethodPost, "/api/v2/collections", nil))
	collectionResponse(t, retry)
	if got := calls.Load(); got != 2 {
		t.Fatalf("retry collection calls: %d", got)
	}
}

func TestExplicitSyncCollectsFreshBeforeAnyBrowserInitialRequest(t *testing.T) {
	input := collectGit(t)
	service, _ := collectionService(t)
	var calls atomic.Int32
	collections := serverapp.NewCollections(service, func(context.Context) (serverapp.Collection, error) {
		fresh := input
		if calls.Add(1) > 1 {
			fresh.SubmissionID = "explicit-sync"
			fresh.Metadata.CollectedAt++
		}
		return serverapp.Collection{Requests: []ingestion.Request{fresh}}, nil
	}, "")
	completed := runInitialCollection(t, collections)
	if err := awaitCollection(t, completed); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	collections.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v2/collections", nil))
	collectionResponse(t, response)
	if got := calls.Load(); got != 2 {
		t.Fatalf("explicit sync reused stale startup collection: %d calls", got)
	}
}

func TestBrowserCancellationDoesNotCancelInitialCollection(t *testing.T) {
	input := collectGit(t)
	service, _ := collectionService(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	collections := serverapp.NewCollections(service, func(ctx context.Context) (serverapp.Collection, error) {
		if calls.Add(1) > 1 {
			return serverapp.Collection{Requests: []ingestion.Request{input}}, nil
		}
		close(started)
		select {
		case <-release:
			return serverapp.Collection{Requests: []ingestion.Request{input}}, nil
		case <-ctx.Done():
			return serverapp.Collection{}, ctx.Err()
		}
	}, "")
	completed := runInitialCollection(t, collections)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("initial collection did not start")
	}
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	cancelRequest()
	collections.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v2/collections?initial=true", nil).WithContext(requestCtx))
	close(release)
	if err := awaitCollection(t, completed); err != nil {
		t.Fatalf("browser cancellation interrupted startup: %v", err)
	}
	response := httptest.NewRecorder()
	collections.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v2/collections?initial=true", nil))
	collectionResponse(t, response)
	if got := calls.Load(); got != 1 {
		t.Fatalf("canceled browser consumed startup result: %d calls", got)
	}
}

func TestCollectionScopeStartsEmptyAndPublishesOnlySelectedCheckouts(t *testing.T) {
	input := collectGit(t)
	service, store := collectionService(t)
	unscoped := contextservice.New(store, reviewstore.User{ID: service.UserID(), Name: "collection-test"})
	defer unscoped.Close()
	unrelated := input
	unrelated.Metadata.CheckoutKey = "unrelated-checkout"
	unrelated.SubmissionID = "unrelated-submission"
	stored, err := unscoped.Ingest(t.Context(), unrelated)
	if err != nil {
		t.Fatal(err)
	}
	collections := serverapp.NewCollections(service, func(context.Context) (serverapp.Collection, error) {
		return serverapp.Collection{Requests: []ingestion.Request{input}}, nil
	}, "")
	assets := fstest.MapFS{"index.html": {Data: []byte("review UI")}}
	handler := serverapp.Handler(t.Context(), service, store, assets, "", serverapp.Options{Collections: collections})
	list := func() contextservice.Page {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/contexts", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("catalog: %d %s", response.Code, response.Body.String())
		}
		var page contextservice.Page
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	if page := list(); len(page.Contexts) != 0 {
		t.Fatalf("startup exposed prior unrelated reviews: %#v", page.Contexts)
	}
	completed := runInitialCollection(t, collections)
	if err := awaitCollection(t, completed); err != nil {
		t.Fatal(err)
	}
	if page := list(); len(page.Contexts) != 1 || page.Contexts[0].ID == stored.Context.ID {
		t.Fatalf("publication widened beyond selected checkout: %#v", page.Contexts)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/contexts/"+stored.Context.ID, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unrelated review resolved: %d %s", response.Code, response.Body.String())
	}
}
