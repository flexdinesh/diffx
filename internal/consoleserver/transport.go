package consoleserver

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

const (
	googleCallback = "/api/v1/auth/google/callback"
	// flowCookie binds a Google authorization to the browser that started it.
	flowCookie = "diffx_console_google_flow"
)

type transport struct {
	identity  *identityservice.Service
	settings  Settings
	assets    fs.FS
	admission *authhttp.Admission
}

type adminJSON struct {
	ID     string                 `json:"id"`
	Email  string                 `json:"email"`
	Method identityservice.Method `json:"method"`
}

type settingsJSON struct {
	Signup         bool `json:"signup"`
	PasswordSignup bool `json:"passwordSignup"`
}

type bootstrapJSON struct {
	SetupRequired bool       `json:"setupRequired"`
	GoogleEnabled bool       `json:"googleEnabled"`
	Admin         *adminJSON `json:"admin"`
	CSRFToken     *string    `json:"csrfToken"`
}

type accountJSON struct {
	ID        string                 `json:"id"`
	Email     string                 `json:"email"`
	Method    identityservice.Method `json:"method"`
	Admin     bool                   `json:"admin"`
	Disabled  bool                   `json:"disabled"`
	CreatedAt int64                  `json:"createdAt"`
}

type inviteJSON struct {
	ID        string                       `json:"id"`
	Email     string                       `json:"email"`
	Status    identityservice.InviteStatus `json:"status"`
	CreatedAt int64                        `json:"createdAt"`
	ExpiresAt int64                        `json:"expiresAt"`
	UsedBy    *string                      `json:"usedBy"`
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func projectInvite(invite identityservice.Invite, now time.Time) inviteJSON {
	return inviteJSON{ID: invite.ID, Email: invite.Email, Status: invite.Status(now), CreatedAt: invite.CreatedAt.UnixMilli(),
		ExpiresAt: invite.ExpiresAt.UnixMilli(), UsedBy: optional(invite.UsedBy)}
}

// authForms verify a secret or start a sign-in, so admission bounds them.
var authForms = map[string]bool{
	"/api/v1/setup": true, "/api/v1/setup/google": true, "/api/v1/auth/login": true, "/api/v1/auth/google": true,
	"/api/v1/password-reset/lookup": true, "/api/v1/password-reset": true,
}

func (console *transport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.URL.Path == googleCallback {
		console.googleCallbackRoute(w, r)
		return
	}
	api := strings.HasPrefix(r.URL.Path, "/api/")
	if !serverapp.SameOrigin(r) && !(!api && serverapp.PageNavigation(r)) {
		authhttp.Problem(w, 403, "Cross-origin access denied")
		return
	}
	authForm := authForms[r.URL.Path]
	var session *identityservice.Session
	if cookie, err := r.Cookie(authhttp.SessionCookie); err == nil {
		authenticated, err := console.identity.AuthenticateSession(r.Context(), identityservice.AudienceConsole, cookie.Value)
		switch {
		case err == nil:
			// Stored sessions keep only the secret's hash; logout needs the secret.
			authenticated.Secret = cookie.Value
			session = &authenticated
		case !errors.Is(err, identityservice.ErrAuthentication):
			authhttp.IdentityError(w, err)
			return
		case !api || authForm || r.URL.Path == "/api/v1/bootstrap":
			// Sign-in stays available with an expired or demoted session.
			console.cookie(w, "", time.Time{})
		default:
			authhttp.Unauthorized(w)
			return
		}
	}
	switch {
	case authForm:
		console.auth(w, r)
	case r.URL.Path == "/api/v1/bootstrap":
		if r.Method != http.MethodGet {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		console.bootstrap(w, r, session)
	case !api:
		console.static(w, r)
	case session == nil:
		authhttp.Unauthorized(w)
	case authhttp.Unsafe(r.Method) && !authhttp.ValidCSRF(r, session.CSRFToken):
		authhttp.Problem(w, 403, "CSRF token required")
	case r.URL.Path == "/api/v1/auth/logout" || r.URL.Path == "/api/v1/account/password":
		console.account(w, r, session)
	case r.URL.Path == "/api/v1/accounts" || strings.HasPrefix(r.URL.Path, "/api/v1/accounts/"):
		console.accounts(w, r, session.Account)
	case r.URL.Path == "/api/v1/invites" || strings.HasPrefix(r.URL.Path, "/api/v1/invites/"):
		console.invites(w, r)
	case r.URL.Path == "/api/v1/settings":
		console.appSettings(w, r, session.Account)
	default:
		http.NotFound(w, r)
	}
}

func (console *transport) cookie(w http.ResponseWriter, secret string, expiry time.Time) {
	authhttp.SetSession(w, secret, expiry, console.settings.InsecureCookies)
}

func (console *transport) bootstrap(w http.ResponseWriter, r *http.Request, session *identityservice.Session) {
	required, err := console.identity.SetupRequired(r.Context())
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	result := bootstrapJSON{SetupRequired: required, GoogleEnabled: console.identity.GoogleEnabled()}
	if session != nil {
		result.Admin = &adminJSON{ID: session.Account.ID, Email: session.Account.Email, Method: session.Account.Method}
		result.CSRFToken = &session.CSRFToken
	}
	authhttp.WriteJSON(w, 200, result)
}

// auth serves the credential forms in authForms.
func (console *transport) auth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	release, ok := console.admission.Admit(w, r)
	if !ok {
		return
	}
	defer release()
	switch r.URL.Path {
	case "/api/v1/auth/google", "/api/v1/setup/google":
		console.googleStart(w, r)
		return
	case "/api/v1/password-reset/lookup", "/api/v1/password-reset":
		console.passwordReset(w, r)
		return
	}
	var input struct {
		Token    string `json:"token,omitempty"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	var session identityservice.Session
	var err error
	if r.URL.Path == "/api/v1/setup" {
		session, err = console.identity.CompleteSetup(r.Context(), input.Token, input.Email, input.Password)
		if errors.Is(err, identityservice.ErrNotFound) {
			authhttp.Problem(w, 404, "Setup link is invalid, expired or already used")
			return
		}
	} else {
		session, err = console.identity.Login(r.Context(), identityservice.AudienceConsole, input.Email, input.Password)
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	console.cookie(w, session.Secret, session.ExpiresAt)
	console.bootstrap(w, r, &session)
}

// passwordReset resolves and completes admins' reset links. Secrets come in
// the body so they never appear in request logs.
func (console *transport) passwordReset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password,omitempty"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	if r.URL.Path == "/api/v1/password-reset/lookup" {
		account, err := console.identity.PendingPasswordReset(r.Context(), identityservice.AudienceConsole, input.Token)
		if err != nil {
			resetError(w, err)
			return
		}
		authhttp.WriteJSON(w, 200, struct {
			Email string `json:"email"`
		}{account.Email})
		return
	}
	if err := console.identity.CompletePasswordReset(r.Context(), identityservice.AudienceConsole, input.Token, input.Password); err != nil {
		resetError(w, err)
		return
	}
	w.WriteHeader(204)
}

func resetError(w http.ResponseWriter, err error) {
	if errors.Is(err, identityservice.ErrNotFound) {
		authhttp.Problem(w, 404, "Reset link is invalid, expired or already used")
		return
	}
	authhttp.IdentityError(w, err)
}

// googleStart begins Google sign-in for an existing admin, or Google setup of
// the first admin from the setup link's secret.
func (console *transport) googleStart(w http.ResponseWriter, r *http.Request) {
	var authorize, binding string
	var err error
	if r.URL.Path == "/api/v1/setup/google" {
		var input struct {
			Token string `json:"token"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		authorize, binding, err = console.identity.StartGoogleSetup(r.Context(), input.Token)
		if errors.Is(err, identityservice.ErrNotFound) {
			authhttp.Problem(w, 404, "Setup link is invalid, expired or already used")
			return
		}
	} else {
		if !authhttp.DecodeJSON(w, r, &struct{}{}) {
			return
		}
		authorize, binding, err = console.identity.StartGoogle(r.Context(), "")
	}
	if errors.Is(err, identityservice.ErrGoogleDisabled) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: binding, Path: googleCallback, MaxAge: int(identityservice.GoogleFlowLifetime / time.Second),
		HttpOnly: true, Secure: !console.settings.InsecureCookies, SameSite: http.SameSiteLaxMode})
	authhttp.WriteJSON(w, 200, struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}{authorize})
}

// googleCallbackRoute completes Google sign-in. It is a cross-site top-level
// navigation, so it runs before the same-origin check; the state and the flow
// cookie authenticate it. Every outcome redirects to the console.
func (console *transport) googleCallbackRoute(w http.ResponseWriter, r *http.Request) {
	if !console.identity.GoogleEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: googleCallback, MaxAge: -1, HttpOnly: true, Secure: !console.settings.InsecureCookies, SameSite: http.SameSiteLaxMode})
	query := r.URL.Query()
	if query.Get("error") != "" || query.Get("code") == "" {
		http.Redirect(w, r, "/?signin=cancelled", http.StatusSeeOther)
		return
	}
	var binding string
	if cookie, err := r.Cookie(flowCookie); err == nil {
		binding = cookie.Value
	}
	session, err := console.identity.CompleteGoogle(r.Context(), identityservice.AudienceConsole, query.Get("state"), binding, query.Get("code"))
	target := "/"
	switch {
	case err == nil:
		console.cookie(w, session.Secret, session.ExpiresAt)
	case errors.Is(err, identityservice.ErrNoAccount):
		target = "/?signin=no-account"
	default:
		// A used or expired setup secret, or a taken name, fails like a refusal.
		if !errors.Is(err, identityservice.ErrAuthentication) && !errors.Is(err, identityservice.ErrNotFound) && !errors.Is(err, identityservice.ErrConflict) {
			log.Printf("console Google sign-in: %v", err)
		}
		target = "/?signin=failed"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (console *transport) account(w http.ResponseWriter, r *http.Request, session *identityservice.Session) {
	if r.URL.Path == "/api/v1/auth/logout" {
		if r.Method != http.MethodPost {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		if err := console.identity.Logout(r.Context(), session.Secret); err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		console.cookie(w, "", time.Time{})
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPut {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	if err := console.identity.ChangePassword(r.Context(), session.Account.ID, input.CurrentPassword, input.NewPassword); err != nil {
		if errors.Is(err, identityservice.ErrAuthentication) {
			authhttp.Problem(w, 403, "Current password incorrect")
			return
		}
		authhttp.IdentityError(w, err)
		return
	}
	console.cookie(w, "", time.Time{})
	w.WriteHeader(204)
}

// accounts lists accounts and applies an admin's actions to another account.
func (console *transport) accounts(w http.ResponseWriter, r *http.Request, actor identityservice.Account) {
	if r.URL.Path == "/api/v1/accounts" {
		if r.Method != http.MethodGet {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		summaries, err := console.identity.AccountSummaries(r.Context())
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		accounts := make([]accountJSON, 0, len(summaries))
		for _, account := range summaries {
			accounts = append(accounts, accountJSON{ID: account.ID, Email: account.Email, Method: account.Method,
				Admin: account.Admin, Disabled: account.Disabled, CreatedAt: account.CreatedAt.UnixMilli()})
		}
		authhttp.WriteJSON(w, 200, struct {
			Accounts []accountJSON `json:"accounts"`
		}{accounts})
		return
	}
	id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/"), "/")
	if id == "" || strings.Contains(action, "/") {
		http.NotFound(w, r)
		return
	}
	method := map[string]string{"": http.MethodDelete, "disable": http.MethodPost, "enable": http.MethodPost, "admin": http.MethodPut, "password-reset": http.MethodPost}
	expected, known := method[action]
	if !known {
		http.NotFound(w, r)
		return
	}
	if r.Method != expected {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	var err error
	switch action {
	case "":
		err = console.identity.DeleteAccount(r.Context(), actor.ID, id)
	case "disable":
		err = console.identity.DisableAccount(r.Context(), actor.ID, id)
	case "enable":
		err = console.identity.EnableAccount(r.Context(), actor.ID, id)
	case "admin":
		var input struct {
			Admin *bool `json:"admin"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		if input.Admin == nil {
			authhttp.Problem(w, 400, "Malformed request")
			return
		}
		err = console.identity.SetAdmin(r.Context(), actor.ID, id, *input.Admin)
	case "password-reset":
		account, secret, err := console.identity.ResetPassword(r.Context(), id)
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		authhttp.WriteJSON(w, 201, struct {
			URL       string `json:"url"`
			ExpiresAt int64  `json:"expiresAt"`
		}{resetURL(console.settings, console.origin(r), account, secret), time.Now().Add(identityservice.PasswordResetLifetime).UnixMilli()})
		return
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	w.WriteHeader(204)
}

// origin is the console's browser-facing origin; without PublicURL it is the
// host the admin is browsing, which same-origin checks already bound.
func (console *transport) origin(r *http.Request) string {
	if console.settings.PublicURL != "" {
		return strings.TrimSuffix(console.settings.PublicURL, "/")
	}
	return "http://" + r.Host
}

// resetURL links admins to the console and everyone else to the app.
func resetURL(settings Settings, consoleOrigin string, account identityservice.Account, secret string) string {
	base := strings.TrimSuffix(settings.AppURL, "/")
	if account.Admin {
		base = consoleOrigin
	}
	return base + "/reset-password#t=" + secret
}

// ResetPassword issues a reset link for an account email, for offline recovery.
func ResetPassword(ctx context.Context, store *pgstore.Store, settings Settings, email string) (string, error) {
	account, secret, err := identityservice.New(store).ResetPasswordByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return resetURL(settings, origin(settings, settings.Listen), account, secret), nil
}

// appSettings reads the stored settings and applies an admin's change, which
// every server replica reloads from its notification.
func (console *transport) appSettings(w http.ResponseWriter, r *http.Request, actor identityservice.Account) {
	switch r.Method {
	case http.MethodGet:
		settings, err := console.identity.LoadSettings(r.Context())
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		authhttp.WriteJSON(w, 200, settingsJSON{Signup: settings.Signup, PasswordSignup: settings.PasswordSignup})
	case http.MethodPut:
		var input struct {
			Signup         *bool `json:"signup"`
			PasswordSignup *bool `json:"passwordSignup"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		if input.Signup == nil || input.PasswordSignup == nil {
			authhttp.Problem(w, 400, "Malformed request")
			return
		}
		settings := identityservice.Settings{Signup: *input.Signup, PasswordSignup: *input.PasswordSignup}
		if err := console.identity.PutSettings(r.Context(), actor.ID, settings); err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		authhttp.WriteJSON(w, 200, settingsJSON{Signup: settings.Signup, PasswordSignup: settings.PasswordSignup})
	default:
		authhttp.Problem(w, 405, "Method not allowed")
	}
}

// invites lists, creates and revokes member invites. The link opens the app,
// carries its secret in the fragment and is returned only at creation.
func (console *transport) invites(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/invites" {
		switch r.Method {
		case http.MethodGet:
			invites, err := console.identity.Invites(r.Context())
			if err != nil {
				authhttp.IdentityError(w, err)
				return
			}
			now := time.Now()
			values := make([]inviteJSON, 0, len(invites))
			for _, invite := range invites {
				values = append(values, projectInvite(invite, now))
			}
			authhttp.WriteJSON(w, 200, struct {
				Invites []inviteJSON `json:"invites"`
			}{values})
		case http.MethodPost:
			var input struct {
				Email string `json:"email"`
			}
			if !authhttp.DecodeJSON(w, r, &input) {
				return
			}
			invite, secret, err := console.identity.CreateInvite(r.Context(), input.Email)
			if errors.Is(err, identityservice.ErrConflict) {
				authhttp.Problem(w, 409, "An account already uses this email")
				return
			}
			if err != nil {
				authhttp.IdentityError(w, err)
				return
			}
			authhttp.WriteJSON(w, 201, struct {
				Invite inviteJSON `json:"invite"`
				URL    string     `json:"url"`
			}{projectInvite(invite, invite.CreatedAt), strings.TrimSuffix(console.settings.AppURL, "/") + "/invite#t=" + secret})
		default:
			authhttp.Problem(w, 405, "Method not allowed")
		}
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/invites/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	if err := console.identity.RevokeInvite(r.Context(), id); err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	w.WriteHeader(204)
}

// static serves the console app, falling back to index.html for its routes.
func (console *transport) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if _, err := fs.Stat(console.assets, name); err != nil {
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	raw, err := fs.ReadFile(console.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if path.Ext(name) == ".js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(raw)
	}
}
