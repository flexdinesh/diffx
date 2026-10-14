package serverapp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
)

// defaultContext is the review bare /mcp opens. It follows the newest
// explicit collection so agents see what the user refreshed.
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

// collection serializes user-requested collections of the selected checkout.
// Failed collection never ingests; successful collection ingests a fresh
// observation, which may reuse an unchanged review.
type collection struct {
	mu      sync.Mutex
	service *contextservice.Service
	collect func(context.Context) (ingestion.Request, error)
	current *defaultContext
}

func (handler *collection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	input, err := handler.collect(r.Context())
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "Unable to collect checkout: "+err.Error())
		return
	}
	submitted, err := handler.service.Ingest(r.Context(), input)
	if err != nil {
		applicationError(w, err)
		return
	}
	handler.current.set(submitted.Context.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(submitted.Context)
}
