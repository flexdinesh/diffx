package serverapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

// defaultContext is the review bare /mcp opens. It follows the newest changed
// context of the latest explicit collection so agents see what the user synced.
type defaultContext struct {
	mu sync.Mutex
	id string
}

func (current *defaultContext) get() string {
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.id
}

func (current *defaultContext) set(id string) {
	current.mu.Lock()
	defer current.mu.Unlock()
	current.id = id
}

// Collection is one user-requested collection of the composition's selected
// checkouts. Failures name checkouts that could not be collected.
type Collection struct {
	Requests []ingestion.Request
	Failures []CollectionFailure
}

type CollectionFailure struct {
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// Published is the outcome of ingesting one collection.
type Published struct {
	Submissions []contextservice.Submission
	Failures    []CollectionFailure
}

// Newest is the last published context with changes, else the last one. It
// matches the catalog's newest changed head because ingestion is serial.
func (published Published) Newest() (contextservice.Submission, bool) {
	for index := len(published.Submissions) - 1; index >= 0; index-- {
		if count := published.Submissions[index].Context.ChangedFileCount; count != nil && *count > 0 {
			return published.Submissions[index], true
		}
	}
	if len(published.Submissions) == 0 {
		return contextservice.Submission{}, false
	}
	return published.Submissions[len(published.Submissions)-1], true
}

// Publish selects each collected checkout into the composition's scope, then
// ingests it serially. Each observation commits atomically on its own; a
// rejected request is reported with its checkout and never blocks the others.
func Publish(ctx context.Context, service *contextservice.Service, collection Collection) (Published, error) {
	published := Published{Failures: slices.Clone(collection.Failures)}
	for _, input := range collection.Requests {
		service.Select(input)
		submitted, err := service.Ingest(ctx, input)
		var rejected *review.RequestError
		if errors.As(err, &rejected) {
			published.Failures = append(published.Failures, CollectionFailure{Path: input.Metadata.Root, Detail: rejected.Detail})
			continue
		}
		if err != nil {
			return published, err
		}
		published.Submissions = append(published.Submissions, submitted)
	}
	return published, nil
}

// FailureSummary joins failures for a single diagnostic.
func FailureSummary(failures []CollectionFailure) string {
	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		details = append(details, failure.Path+": "+failure.Detail)
	}
	return strings.Join(details, "; ")
}

// collection serializes user-requested collections of the selected checkouts.
// Failed collection never ingests; each successful checkout ingests a fresh
// observation, which may reuse an unchanged review.
type collection struct {
	mu      sync.Mutex
	service *contextservice.Service
	collect func(context.Context) (Collection, error)
	current *defaultContext
}

func (handler *collection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	collected, err := handler.collect(r.Context())
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "Unable to collect checkouts: "+err.Error())
		return
	}
	published, err := Publish(r.Context(), handler.service, collected)
	if err != nil {
		applicationError(w, err)
		return
	}
	newest, ok := published.Newest()
	if !ok {
		problem(w, http.StatusUnprocessableEntity, "Unable to collect checkouts: "+FailureSummary(published.Failures))
		return
	}
	handler.current.set(newest.Context.ID)
	result := struct {
		Contexts []contextservice.Context `json:"contexts"`
		Failures []CollectionFailure      `json:"failures"`
	}{make([]contextservice.Context, 0, len(published.Submissions)), make([]CollectionFailure, 0, len(published.Failures))}
	for _, submitted := range published.Submissions {
		result.Contexts = append(result.Contexts, submitted.Context)
	}
	result.Failures = append(result.Failures, published.Failures...)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}
