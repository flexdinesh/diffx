package remoteserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

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

type inviteJSON struct {
	ID        string                       `json:"id"`
	Email     string                       `json:"email"`
	Status    identityservice.InviteStatus `json:"status"`
	CreatedAt int64                        `json:"createdAt"`
	ExpiresAt int64                        `json:"expiresAt"`
	UsedBy    *string                      `json:"usedBy"`
}

func projectInvite(invite identityservice.Invite, now time.Time) inviteJSON {
	result := inviteJSON{ID: invite.ID, Email: invite.Email, Status: invite.Status(now), CreatedAt: invite.CreatedAt.UnixMilli(), ExpiresAt: invite.ExpiresAt.UnixMilli()}
	if invite.UsedBy != "" {
		result.UsedBy = &invite.UsedBy
	}
	return result
}

// invites lists, creates and revokes invites. The link carries its secret in
// the fragment and is returned only at creation.
func (remote *transport) invites(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v2/manage/invites" {
		switch r.Method {
		case http.MethodGet:
			invites, err := remote.identity.Invites(r.Context())
			if err != nil {
				identityError(w, err)
				return
			}
			now := time.Now()
			values := make([]inviteJSON, 0, len(invites))
			for _, invite := range invites {
				values = append(values, projectInvite(invite, now))
			}
			writeJSON(w, 200, struct {
				Invites []inviteJSON `json:"invites"`
			}{values})
		case http.MethodPost:
			var input struct {
				Email string `json:"email"`
			}
			if !decodeJSON(w, r, &input) {
				return
			}
			invite, secret, err := remote.identity.CreateInvite(r.Context(), input.Email)
			switch {
			case errors.Is(err, identityservice.ErrGoogleDisabled):
				problem(w, 409, "Google sign-in is not configured; invites need it")
				return
			case errors.Is(err, identityservice.ErrConflict):
				problem(w, 409, "An account already uses this email")
				return
			case err != nil:
				identityError(w, err)
				return
			}
			writeJSON(w, 201, struct {
				Invite inviteJSON `json:"invite"`
				URL    string     `json:"url"`
			}{projectInvite(invite, invite.CreatedAt), strings.TrimSuffix(remote.settings.PublicURL, "/") + "/invite#t=" + secret})
		default:
			problem(w, 405, "Method not allowed")
		}
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v2/manage/invites/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		problem(w, 405, "Method not allowed")
		return
	}
	if err := remote.identity.RevokeInvite(r.Context(), id); err != nil {
		identityError(w, err)
		return
	}
	w.WriteHeader(204)
}

// admin serves account administration to the admin's browser session. Disabling
// or deleting an account stops its active services; its credentials are already gone.
func (remote *transport) admin(w http.ResponseWriter, r *http.Request, p principal) {
	if strings.HasPrefix(r.URL.Path, "/api/v2/manage/invites") {
		remote.invites(w, r)
		return
	}
	if r.URL.Path == "/api/v2/manage/users" {
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
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v2/manage/users/")
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
