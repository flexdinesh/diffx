package remoteserver

import (
	"net/http"
	"strings"

	"github.com/flexdinesh/diffx/internal/identityservice"
)

type adminUserJSON struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	Email     *string `json:"email"`
	Admin     bool    `json:"admin"`
	Disabled  bool    `json:"disabled"`
	CreatedAt int64   `json:"createdAt"`
}

func projectAdminUser(account identityservice.AccountSummary) adminUserJSON {
	result := adminUserJSON{ID: account.ID, Username: account.Username, Admin: account.Admin, Disabled: account.Disabled, CreatedAt: account.CreatedAt.UnixMilli()}
	if account.Email != "" {
		result.Email = &account.Email
	}
	return result
}

// admin serves account administration to the admin's browser session. Disabling
// or deleting an account stops its active services; its credentials are already gone.
func (remote *transport) admin(w http.ResponseWriter, r *http.Request, p principal) {
	if r.URL.Path == "/api/v2/admin/users" {
		if r.Method != http.MethodGet {
			problem(w, 405, "Method not allowed")
			return
		}
		accounts, err := remote.identity.AccountSummaries(r.Context())
		if err != nil {
			identityError(w, err)
			return
		}
		users := make([]adminUserJSON, 0, len(accounts))
		for _, account := range accounts {
			users = append(users, projectAdminUser(account))
		}
		writeJSON(w, 200, struct {
			Users []adminUserJSON `json:"users"`
		}{users})
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v2/admin/users/")
	if !ok || rest == "" {
		http.NotFound(w, r)
		return
	}
	id, action, _ := strings.Cut(rest, "/")
	var err error
	switch {
	case action == "" && r.Method == http.MethodDelete:
		err = remote.identity.DeleteAccount(r.Context(), id)
	case (action == "disable" || action == "enable") && r.Method == http.MethodPost:
		if action == "disable" {
			err = remote.identity.DisableAccount(r.Context(), id)
		} else {
			err = remote.identity.EnableAccount(r.Context(), id)
		}
	case action == "" || action == "disable" || action == "enable":
		problem(w, 405, "Method not allowed")
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err == nil {
		err = remote.applications.remove(id)
	}
	if err != nil {
		identityError(w, err)
		return
	}
	w.WriteHeader(204)
}
