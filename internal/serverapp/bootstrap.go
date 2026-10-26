package serverapp

import (
	"encoding/json"
	"net/http"
)

// The shared application defaults to local access. Remote composition supplies
// its authenticated bootstrap before dispatching to an owner's application.
func localBootstrap(capabilities Capabilities) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			problem(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Mode          string  `json:"mode"`
			GoogleEnabled bool    `json:"googleEnabled"`
			User          *string `json:"user"`
			CSRFToken     *string `json:"csrfToken"`
			Collect       bool    `json:"collect"`
			Input         string  `json:"input"`
		}{Mode: "local", Collect: capabilities.Collection, Input: capabilities.Input()})
	}
}
