package controlapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/flexdinesh/diffx/internal/review"
)

type handler struct {
	status   func() (Status, error)
	shutdown func()
	lifetime context.Context
	draining atomic.Bool
	pair     func(string) ([]string, error)
}

// New serves status and shutdown. Mount it only on a transport that already
// admits just the owning user; browser requests are refused regardless.
func New(status func() (Status, error), shutdown func()) http.Handler {
	return NewWithLifetime(context.Background(), status, shutdown)
}

// NewWithLifetime additionally exposes a private stream which closes when this
// instance stops. It carries no application state and performs no periodic work.
func NewWithLifetime(ctx context.Context, status func() (Status, error), shutdown func()) http.Handler {
	return &handler{status: status, shutdown: shutdown, lifetime: ctx}
}

// NewWithPairing mounts invitation rotation on the same owner-only transport.
// A nil pair callback leaves the endpoint unavailable for ordinary local runs.
func NewWithPairing(ctx context.Context, status func() (Status, error), shutdown func(), pair func(string) ([]string, error)) http.Handler {
	return &handler{status: status, shutdown: shutdown, lifetime: ctx, pair: pair}
}
func (handler *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Header.Get("Origin") != "" || request.Header.Get("Sec-Fetch-Site") != "" || request.Header.Get("Sec-Fetch-Mode") != "" {
		writeProblem(response, http.StatusForbidden, "browser_request", "Browser access to service control denied")
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/control/v1/status" {
		value, err := handler.status()
		if err != nil {
			writeError(response, err)
			return
		}
		if handler.draining.Load() {
			value.State = "draining"
		}
		writeJSON(response, http.StatusOK, value)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/control/v1/lifetime" {
		status, err := handler.status()
		if err != nil {
			writeError(response, err)
			return
		}
		if status.InstanceID != request.URL.Query().Get("instanceId") || status.State != "running" || handler.draining.Load() {
			writeProblem(response, http.StatusConflict, "instance_changed", "Local instance stopped or was replaced")
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.WriteHeader(http.StatusOK)
		if flusher, ok := response.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-request.Context().Done():
		case <-handler.lifetime.Done():
		}
		return
	}
	if request.Method != http.MethodPost {
		writeProblem(response, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if request.URL.Path == "/control/v1/shutdown" {
		first := handler.draining.CompareAndSwap(false, true)
		writeJSON(response, http.StatusOK, struct{}{})
		if flusher, ok := response.(http.Flusher); ok {
			flusher.Flush()
		}
		if first {
			go handler.shutdown()
		}
		return
	}
	if request.URL.Path == "/control/v1/pair" && handler.pair != nil && !handler.draining.Load() {
		var input struct {
			Advertise string `json:"advertise"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(struct{})) != io.EOF {
			writeProblem(response, http.StatusBadRequest, "invalid_pair_request", "Invalid pairing request")
			return
		}
		invitations, err := handler.pair(input.Advertise)
		if err != nil {
			writeProblem(response, http.StatusBadRequest, "pair_unavailable", "Cannot create personal server invitation")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Invitations []string `json:"invitations"`
		}{invitations})
		return
	}
	writeProblem(response, 404, "not_found", "Control endpoint not found")
}

func writeJSON(response http.ResponseWriter, status int, value interface{}) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeProblem(response http.ResponseWriter, status int, code, detail string) {
	response.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail})
}

func writeError(response http.ResponseWriter, err error) {
	var limit *http.MaxBytesError
	var requestError *review.RequestError
	switch {
	case errors.As(err, &limit):
		writeProblem(response, http.StatusRequestEntityTooLarge, "input_too_large", "Input exceeds control request limit")
	case errors.As(err, &requestError):
		writeProblem(response, requestError.Status, "request_failed", requestError.Detail)
	default:
		writeProblem(response, http.StatusInternalServerError, "request_failed", err.Error())
	}
}
