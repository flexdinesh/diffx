package serverapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

// defaultContext is the review bare /mcp opens. It follows the default of the
// latest explicit collection so agents see what the user synced.
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
	Path         string `json:"path"`
	Detail       string `json:"detail"`
	Code         string `json:"code,omitempty"`
	DiagnosticID string `json:"diagnosticId,omitempty"`
}

// Collect publishes completed checkout requests serially before returning the
// remaining checkout failures. It joins its workers before returning, including
// after cancellation or a publication error.
type Collect func(context.Context, func(ingestion.Request) error) ([]CollectionFailure, error)

// Published is the outcome of ingesting one collection.
type Published struct {
	Publications []contextservice.Publication
	Failures     []CollectionFailure
	// launched is the context of the deepest checkout containing its request's
	// input path. Reused observations keep their first run's metadata.
	launched      string
	launchedDepth int
}

// Newest prefers current contexts with changes, then current clean contexts.
// Among equals, the checkout containing the collection's input path wins, so
// `diffx PATH` inside a worktree opens that worktree; then recent activity.
// A successful collection of older observations still has an available default.
func (published Published) Newest() (contextservice.Publication, bool) {
	var newest, fallback contextservice.Publication
	found, available := false, false
	changed := false
	for _, submitted := range published.Publications {
		if submitted.Context.Availability != "available" {
			continue
		}
		if !available || contextservice.CompareActivity(submitted.Context, fallback.Context) < 0 {
			fallback, available = submitted, true
		}
		if submitted.Context.Stale {
			continue
		}
		hasChanges := submitted.Context.ChangedFileCount != nil && *submitted.Context.ChangedFileCount > 0
		if !found || hasChanges && !changed || hasChanges == changed && published.precedes(submitted.Context, newest.Context) {
			newest, found, changed = submitted, true, hasChanges
		}
	}
	if found {
		return newest, true
	}
	return fallback, available
}

func (published Published) precedes(left, right contextservice.Context) bool {
	if launched := published.launched; launched != "" && (left.ID == launched) != (right.ID == launched) {
		return left.ID == launched
	}
	return contextservice.CompareActivity(left, right) < 0
}

// launchDepth ranks a checkout whose root contains its trigger root, the input
// path of local collection. Nested worktrees prefer the deepest root.
func launchDepth(metadata ingestion.Metadata) int {
	if metadata.Root == "" || metadata.TriggerRoot == "" {
		return 0
	}
	relative, err := filepath.Rel(metadata.Root, metadata.TriggerRoot)
	if err != nil || !filepath.IsLocal(relative) {
		return 0
	}
	return len(metadata.Root)
}

// Publish selects each collected checkout into the composition's scope, then
// ingests it serially. Each observation commits atomically on its own; a
// rejected request is reported with its checkout and never blocks the others.
func Publish(ctx context.Context, service *contextservice.Service, collection Collection) (Published, error) {
	published := Published{Failures: slices.Clone(collection.Failures)}
	for _, input := range collection.Requests {
		if err := published.ingest(ctx, service, input); err != nil {
			return published, err
		}
	}
	return published, nil
}

func (published *Published) ingest(ctx context.Context, service *contextservice.Service, input ingestion.Request) error {
	service.Select(input)
	submitted, err := service.Ingest(ctx, input)
	var rejected *review.RequestError
	if errors.As(err, &rejected) {
		published.Failures = append(published.Failures, CollectionFailure{Path: input.Metadata.Root, Detail: rejected.Detail})
		return nil
	}
	if err != nil {
		return err
	}
	published.Publications = append(published.Publications, submitted)
	if depth := launchDepth(input.Metadata); depth > published.launchedDepth {
		published.launched, published.launchedDepth = submitted.Context.ID, depth
	}
	return nil
}

// FailureSummary joins failures for a single diagnostic.
func FailureSummary(failures []CollectionFailure) string {
	details := make([]string, 0, len(failures))
	for _, failure := range failures {
		details = append(details, failure.Path+": "+failure.Detail)
	}
	return strings.Join(details, "; ")
}

// Selection names which checkouts a local process collects.
type Selection string

const (
	// SelectedCheckout collects only the checkout the process was started for.
	SelectedCheckout Selection = "checkout"
	// DiscoveredCheckouts collects every checkout discovered under the paths.
	DiscoveredCheckouts Selection = "checkouts"
)

// Collections serializes checkout collection and publication. The first UI
// load request joins startup, including when startup finished before the browser opened.
// Later requests collect fresh snapshots. A disconnected browser does not cancel
// startup, whose lifetime belongs to the foreground process.
type Collections struct {
	mu        sync.Mutex
	pending   *collectionResult
	gate      chan struct{}
	service   *contextservice.Service
	selection Selection
	collect   Collect
	current   *defaultContext
}

type collectionResult struct {
	done      chan struct{}
	published Published
	err       error
}

func NewCollections(service *contextservice.Service, selection Selection, collect Collect, defaultID string) *Collections {
	return &Collections{service: service, selection: selection, collect: collect, current: &defaultContext{id: defaultID}, gate: make(chan struct{}, 1)}
}

// Initial reserves the startup result before serving requests and returns the
// task that collects it. Call once, before Run. Collection errors are reported
// to the UI and callback; the server stays available for retry.
func (handler *Collections) Initial(completed func(Published, error)) Task {
	initial := &collectionResult{done: make(chan struct{})}
	handler.pending = initial
	return func(ctx context.Context) error {
		initial.published, initial.err = handler.run(ctx)
		close(initial.done)
		if completed != nil && ctx.Err() == nil {
			completed(initial.published, initial.err)
		}
		<-ctx.Done()
		return nil
	}
}

func (handler *Collections) run(ctx context.Context) (Published, error) {
	select {
	case handler.gate <- struct{}{}:
		defer func() { <-handler.gate }()
	case <-ctx.Done():
		return Published{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Published{}, err
	}
	published := Published{}
	var publishError error
	failures, err := handler.collect(ctx, func(input ingestion.Request) error {
		if publishError = published.ingest(ctx, handler.service, input); publishError != nil {
			return publishError
		}
		if newest, ok := published.Newest(); ok {
			handler.current.set(newest.Context.ID)
		}
		return nil
	})
	published.Failures = append(published.Failures, failures...)
	if publishError != nil {
		return published, publishError
	}
	if err := ctx.Err(); err != nil {
		return published, err
	}
	if err != nil {
		failure := &review.RequestError{Status: http.StatusUnprocessableEntity, Detail: "Unable to collect checkouts: " + err.Error(), Code: "collection_failed"}
		var diagnostic *review.DiagnosticError
		if errors.As(err, &diagnostic) {
			failure.Code, failure.DiagnosticID = diagnostic.Code, diagnostic.ID
		}
		return published, failure
	}
	newest, ok := published.Newest()
	if !ok {
		failure := &review.RequestError{Status: http.StatusUnprocessableEntity, Detail: "Unable to collect checkouts: " + FailureSummary(published.Failures), Code: "collection_failed"}
		for _, checkout := range published.Failures {
			if checkout.DiagnosticID != "" {
				failure.Code, failure.DiagnosticID = checkout.Code, checkout.DiagnosticID
				break
			}
		}
		return published, failure
	}
	handler.current.set(newest.Context.ID)
	return published, nil
}

func (handler *Collections) result(ctx context.Context, joinInitial bool) (Published, error) {
	handler.mu.Lock()
	initial := handler.pending
	if !joinInitial {
		handler.pending = nil
		initial = nil
	}
	handler.mu.Unlock()
	if initial == nil {
		return handler.run(ctx)
	}
	select {
	case <-ctx.Done():
		return Published{}, ctx.Err()
	case <-initial.done:
		handler.mu.Lock()
		if handler.pending == initial {
			handler.pending = nil
		}
		handler.mu.Unlock()
		return initial.published, initial.err
	}
}

func (handler *Collections) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	published, err := handler.result(r.Context(), r.URL.Query().Get("initial") == "true")
	if err != nil {
		applicationError(w, err)
		return
	}
	newest, _ := published.Newest()
	result := struct {
		Contexts         []contextservice.Context `json:"contexts"`
		Failures         []CollectionFailure      `json:"failures"`
		DefaultContextID string                   `json:"defaultContextId"`
	}{make([]contextservice.Context, 0, len(published.Publications)), make([]CollectionFailure, 0, len(published.Failures)), newest.Context.ID}
	for _, submitted := range published.Publications {
		result.Contexts = append(result.Contexts, submitted.Context)
	}
	result.Failures = append(result.Failures, published.Failures...)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}
