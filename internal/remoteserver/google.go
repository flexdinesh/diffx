package remoteserver

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"

	"github.com/flexdinesh/diffx/internal/identityservice"
)

const (
	googleCallback = "/api/v2/auth/google/callback"
	// flowCookie binds a Google authorization to the browser that started it,
	// so a forged callback cannot sign a victim into another account.
	flowCookie = "diffx_google_flow"
)

// googleStart begins Google sign-in, optionally from an invite link. It is an
// authentication form: the caller already bounded admission.
func (remote *transport) googleStart(w http.ResponseWriter, r *http.Request) {
	var invite string
	if r.URL.Path == "/api/v2/invites/google" {
		var input struct {
			Token string `json:"token"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		invite = input.Token
	} else if !authhttp.DecodeJSON(w, r, &struct{}{}) {
		return
	}
	authorize, binding, err := remote.identity.StartGoogle(r.Context(), invite)
	if err != nil {
		googleError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: binding, Path: googleCallback, MaxAge: int(identityservice.GoogleFlowLifetime / time.Second),
		HttpOnly: true, Secure: !remote.settings.InsecureCookies, SameSite: http.SameSiteLaxMode})
	authhttp.WriteJSON(w, 200, struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}{authorize})
}

// inviteLookup resolves an invite secret for the invite page, which offers the
// deployment's sign-in methods. The secret comes
// in the body so it never appears in request logs.
func (remote *transport) inviteLookup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	invite, err := remote.identity.PendingInvite(r.Context(), input.Token)
	if err != nil {
		googleError(w, r, err)
		return
	}
	authhttp.WriteJSON(w, 200, struct {
		Email     string `json:"email"`
		Admin     bool   `json:"admin"`
		ExpiresAt int64  `json:"expiresAt"`
	}{invite.Email, invite.Admin, invite.ExpiresAt.UnixMilli()})
}

func googleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identityservice.ErrGoogleDisabled):
		http.NotFound(w, r)
	case errors.Is(err, identityservice.ErrNotFound):
		authhttp.Problem(w, 404, "Invite link is invalid, expired or already used")
	default:
		authhttp.IdentityError(w, err)
	}
}

// googleCallbackRoute completes Google sign-in. It is a cross-site top-level
// navigation, so it runs before the same-origin check; the state and the flow
// cookie authenticate it. Every outcome redirects to the app.
func (remote *transport) googleCallbackRoute(w http.ResponseWriter, r *http.Request) {
	if !remote.identity.GoogleEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: googleCallback, MaxAge: -1, HttpOnly: true, Secure: !remote.settings.InsecureCookies, SameSite: http.SameSiteLaxMode})
	query := r.URL.Query()
	if query.Get("error") != "" || query.Get("code") == "" {
		http.Redirect(w, r, "/?signin=cancelled", http.StatusSeeOther)
		return
	}
	var binding string
	if cookie, err := r.Cookie(flowCookie); err == nil {
		binding = cookie.Value
	}
	session, err := remote.identity.CompleteGoogle(r.Context(), identityservice.AudienceMembers, query.Get("state"), binding, query.Get("code"))
	outcome := ""
	switch {
	case err == nil:
		remote.cookie(w, session.Secret, session.ExpiresAt)
	case errors.Is(err, identityservice.ErrNoAccount):
		outcome = "no-account"
	case errors.Is(err, identityservice.ErrInviteMismatch):
		outcome = "invite-mismatch"
	case errors.Is(err, identityservice.ErrNotFound):
		outcome = "invite-invalid"
	default:
		if !errors.Is(err, identityservice.ErrAuthentication) {
			log.Printf("Google sign-in: %v", err)
		}
		outcome = "failed"
	}
	target := "/"
	if outcome != "" {
		target = "/?signin=" + outcome
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
