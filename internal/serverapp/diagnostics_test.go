package serverapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestCollectionFailureProjectsPortableDiagnosticReference(t *testing.T) {
	for _, returnedError := range []bool{false, true} {
		t.Run(map[bool]string{true: "discovery", false: "checkout"}[returnedError], func(t *testing.T) {
			const id = "7166335a-280f-42d8-a595-fb4823d85454"
			collect := func(context.Context, func(ingestion.Request) error) ([]CollectionFailure, error) {
				if returnedError {
					return nil, &review.DiagnosticError{ID: id, Code: "collection_timeout", Stage: "patches (commit)", Cause: context.DeadlineExceeded}
				}
				return []CollectionFailure{{Path: "/checkout", Detail: "Collection timed out during patches (commit)", Code: "collection_timeout", DiagnosticID: id}}, nil
			}
			collections := NewCollections(nil, SelectedCheckout, collect, "")
			response := httptest.NewRecorder()
			collections.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v2/collections", nil))
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			var body struct {
				Code   string `json:"code"`
				ID     string `json:"diagnosticId"`
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.ID != id || body.Code != "collection_timeout" || body.Detail == "" {
				t.Fatalf("problem %+v", body)
			}
		})
	}
}
