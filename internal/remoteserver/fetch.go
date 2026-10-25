package remoteserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/githubsync"
)

// fetchHandler serves the fetcher's lease protocol on the internal listener
// only. The shared secret authenticates the fetcher; it never carries an account.
func fetchHandler(service *githubsync.Service, secret string) http.Handler {
	want := sha256.Sum256([]byte(secret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		got := sha256.Sum256([]byte(bearer))
		if !ok || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			problem(w, 401, "Fetcher authentication required")
			return
		}
		if r.Method != http.MethodPost {
			problem(w, 405, "Method not allowed")
			return
		}
		switch r.URL.Path {
		case fetchprotocol.LeasePath:
			job, err := service.Lease(r.Context())
			if err != nil {
				log.Printf("GitHub fetch lease: %v", err)
				problem(w, 500, "Fetch lease failed")
				return
			}
			if job == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(w, 200, job)
		case fetchprotocol.ResultPath:
			var result fetchprotocol.Result
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, fetchprotocol.MaxResultBytes)).Decode(&result); err != nil {
				problem(w, 400, "Invalid fetch result")
				return
			}
			err := service.Finish(r.Context(), result)
			switch {
			case err == nil:
				w.WriteHeader(http.StatusNoContent)
			case errors.Is(err, githubsync.ErrLeaseLost):
				problem(w, 409, err.Error())
			case errors.Is(err, githubsync.ErrInvalidResult):
				problem(w, 400, err.Error())
			default:
				log.Printf("GitHub fetch result: %v", err)
				problem(w, 500, "Fetch result not applied")
			}
		default:
			problem(w, 404, "Not found")
		}
	})
}
