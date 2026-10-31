package remoteserver

import (
	"context"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/packages/api"
)

type transport struct {
	identity     *identityservice.Service
	applications *registry
	settings     Settings
	assets       fs.FS
	admission    *authhttp.Admission
	github       *githubsync.Service
	push         *pushservice.Service
}
type principal struct {
	account identityservice.Account
	session *identityservice.Session
	secret  string
	purpose identityservice.Purpose
}
type accountJSON struct {
	ID       string                 `json:"id"`
	Username string                 `json:"username"`
	Email    *string                `json:"email"`
	Method   identityservice.Method `json:"method"`
}
type bootstrapJSON struct {
	Mode                  string       `json:"mode"`
	GoogleEnabled         bool         `json:"googleEnabled"`
	SignupEnabled         bool         `json:"signupEnabled"`
	PasswordSignupEnabled bool         `json:"passwordSignupEnabled"`
	User                  *accountJSON `json:"user"`
	CSRFToken             *string      `json:"csrfToken"`
}

func (remote *transport) bootstrap(w http.ResponseWriter, r *http.Request, p principal) {
	settings := remote.identity.Settings()
	result := bootstrapJSON{Mode: "remote", GoogleEnabled: remote.identity.GoogleEnabled(), SignupEnabled: settings.Signup, PasswordSignupEnabled: settings.PasswordSignup}
	if p.session != nil {
		email, err := remote.identity.GoogleEmail(r.Context(), p.account.ID)
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		result.User = &accountJSON{ID: p.account.ID, Username: p.account.Username, Method: p.account.Method}
		if email != "" {
			result.User.Email = &email
		}
		result.CSRFToken = &p.session.CSRFToken
	}
	authhttp.WriteJSON(w, 200, result)
}
func (remote *transport) authenticate(r *http.Request) (principal, error) {
	if value := r.Header.Get("Authorization"); value != "" {
		if !strings.HasPrefix(value, "Bearer ") {
			return principal{}, identityservice.ErrAuthentication
		}
		secret := strings.TrimPrefix(value, "Bearer ")
		token, err := remote.identity.AuthenticateToken(r.Context(), secret)
		return principal{account: token.Account, secret: secret, purpose: token.Purpose}, err
	}
	cookie, err := r.Cookie(authhttp.SessionCookie)
	if errors.Is(err, http.ErrNoCookie) {
		return principal{}, nil
	}
	if err != nil {
		return principal{}, identityservice.ErrAuthentication
	}
	session, err := remote.identity.AuthenticateSession(r.Context(), identityservice.AudienceMembers, cookie.Value)
	return principal{account: session.Account, session: &session, secret: cookie.Value}, err
}
func (remote *transport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if remote.githubPublic(w, r) {
		return
	}
	if r.URL.Path == googleCallback {
		remote.googleCallbackRoute(w, r)
		return
	}
	if !serverapp.SameOrigin(r) {
		authhttp.Problem(w, 403, "Cross-origin access denied")
		return
	}
	// Login remains available with an expired browser cookie. Explicit bearer
	// credentials are always validated; never silently fall back to cookies.
	authForm := authForms[r.URL.Path]
	public := r.URL.Path == "/api/v2/bootstrap" || (!strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/mcp") && !strings.HasPrefix(r.URL.Path, "/control/"))
	p, err := remote.authenticate(r)
	if errors.Is(err, identityservice.ErrAuthentication) && r.Header.Get("Authorization") == "" && (public || authForm) {
		remote.cookie(w, "", time.Time{})
		p = principal{}
		err = nil
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	if authForm {
		remote.auth(w, r)
		return
	}
	if r.URL.Path == "/api/v2/bootstrap" {
		if r.Method != http.MethodGet {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		remote.bootstrap(w, r, p)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/mcp") && !strings.HasPrefix(r.URL.Path, "/control/") {
		remote.static(w, r)
		return
	}
	if p.account.ID == "" {
		authhttp.Unauthorized(w)
		return
	}
	if p.session != nil && authhttp.Unsafe(r.Method) && !authhttp.ValidCSRF(r, p.session.CSRFToken) {
		authhttp.Problem(w, 403, "CSRF token required")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/github/") {
		if p.session == nil {
			authhttp.Problem(w, 403, "Browser session required")
			return
		}
		remote.githubAccount(w, r, p)
		return
	}
	if r.URL.Path == "/api/v2/push" || strings.HasPrefix(r.URL.Path, "/api/v2/push/") {
		if p.session == nil {
			authhttp.Problem(w, 403, "Browser session required")
			return
		}
		remote.pushRoutes(w, r, p)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/account/") || r.URL.Path == "/api/v2/auth/logout" {
		if p.session == nil {
			authhttp.Problem(w, 403, "Browser session required")
			return
		}
		remote.account(w, r, p)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/auth/") {
		http.NotFound(w, r)
		return
	}
	if p.session == nil && p.purpose == identityservice.PurposeCollector && !collectorRoute(r) {
		authhttp.Problem(w, 403, "Collector token cannot access this resource")
		return
	}
	app, err := remote.applications.get(p.account)
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	if r.URL.Path == "/api/v2/events" || strings.HasPrefix(r.URL.Path, "/mcp") {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go remote.watch(ctx, cancel, p)
		r = r.WithContext(ctx)
	}
	app.handler.ServeHTTP(w, r)
}

// authForms are the credential forms: password sign-in and sign-up, Google
// sign-in starts, invite lookups and acceptance, and password reset links. All
// are bounded like sign-in because each verifies a secret or starts a sign-in.
var authForms = map[string]bool{
	"/api/v2/auth/login": true, "/api/v2/auth/signup": true, "/api/v2/auth/google": true,
	"/api/v2/invites/lookup": true, "/api/v2/invites/google": true, "/api/v2/invites/password": true,
	"/api/v2/password-reset/lookup": true, "/api/v2/password-reset": true,
}

func collectorRoute(r *http.Request) bool {
	p := r.URL.Path
	if p == "/api/v2/ingestion-jobs" {
		return r.Method == http.MethodPost
	}
	if r.Method != http.MethodGet {
		return false
	}
	if p == "/api/v2/health" || p == "/api/v2/contexts" {
		return true
	}
	if strings.HasPrefix(p, "/api/v2/ingestion-jobs/") {
		return !strings.Contains(strings.TrimPrefix(p, "/api/v2/ingestion-jobs/"), "/")
	}
	if strings.HasPrefix(p, "/api/v2/contexts/") {
		return !strings.Contains(strings.TrimPrefix(p, "/api/v2/contexts/"), "/")
	}
	return false
}
func (remote *transport) watch(ctx context.Context, cancel context.CancelFunc, p principal) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-remote.applications.ctx.Done():
			cancel()
			return
		case <-ticker.C:
			var err error
			if p.session != nil {
				_, err = remote.identity.AuthenticateSession(ctx, identityservice.AudienceMembers, p.secret)
			} else {
				_, err = remote.identity.AuthenticateToken(ctx, p.secret)
			}
			if err != nil {
				cancel()
				return
			}
		}
	}
}
func (remote *transport) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	if r.URL.Path == "/openapi.yaml" {
		w.Header().Set("Content-Type", "application/yaml")
		if r.Method == http.MethodGet {
			_, _ = w.Write(api.OpenAPI)
		}
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if _, err := fs.Stat(remote.assets, name); err != nil {
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	raw, err := fs.ReadFile(remote.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := assetType(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// The service worker must update promptly; browsers revalidate it.
	if name == "sw.js" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(raw)
	}
}

// assetType names types the platform MIME table may lack. Browsers require a
// JavaScript type for service workers and accept a manifest only as JSON.
func assetType(extension string) string {
	switch extension {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	}
	return mime.TypeByExtension(extension)
}

// auth serves the credential forms in authForms. Signup forms the settings
// turned off answer 404, like unknown routes; password sign-in and reset links
// always serve existing password accounts.
func (remote *transport) auth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	release, ok := remote.admission.Admit(w, r)
	if !ok {
		return
	}
	defer release()
	switch r.URL.Path {
	case "/api/v2/auth/google", "/api/v2/invites/google":
		remote.googleStart(w, r)
		return
	case "/api/v2/invites/lookup":
		remote.inviteLookup(w, r)
		return
	case "/api/v2/password-reset/lookup", "/api/v2/password-reset":
		remote.passwordReset(w, r)
		return
	}
	var input struct {
		Token    string `json:"token,omitempty"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	var session identityservice.Session
	var err error
	switch r.URL.Path {
	case "/api/v2/auth/signup":
		session, err = remote.identity.SignUp(r.Context(), input.Username, input.Password)
	case "/api/v2/invites/password":
		session, err = remote.identity.AcceptPasswordInvite(r.Context(), input.Token, input.Username, input.Password)
		if errors.Is(err, identityservice.ErrNotFound) {
			authhttp.Problem(w, 404, "Invite link is invalid, expired or already used")
			return
		}
	default:
		session, err = remote.identity.Login(r.Context(), identityservice.AudienceMembers, input.Username, input.Password)
	}
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	remote.cookie(w, session.Secret, session.ExpiresAt)
	remote.bootstrap(w, r, principal{account: session.Account, session: &session})
}

// passwordReset resolves and completes password reset links. Secrets come in
// the body so they never appear in request logs.
func (remote *transport) passwordReset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password,omitempty"`
	}
	if !authhttp.DecodeJSON(w, r, &input) {
		return
	}
	if r.URL.Path == "/api/v2/password-reset/lookup" {
		account, err := remote.identity.PendingPasswordReset(r.Context(), identityservice.AudienceMembers, input.Token)
		if err != nil {
			resetError(w, err)
			return
		}
		authhttp.WriteJSON(w, 200, struct {
			Username string `json:"username"`
		}{account.Username})
		return
	}
	if err := remote.identity.CompletePasswordReset(r.Context(), identityservice.AudienceMembers, input.Token, input.Password); err != nil {
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
func (remote *transport) cookie(w http.ResponseWriter, secret string, expiry time.Time) {
	authhttp.SetSession(w, secret, expiry, remote.settings.InsecureCookies)
}

type tokenJSON struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Purpose    identityservice.Purpose `json:"purpose"`
	CreatedAt  int64                   `json:"createdAt"`
	LastUsedAt *int64                  `json:"lastUsedAt"`
	ExpiresAt  int64                   `json:"expiresAt"`
}

func projectToken(token identityservice.Token) tokenJSON {
	result := tokenJSON{ID: token.ID, Name: token.Name, Purpose: token.Purpose, CreatedAt: token.CreatedAt.UnixMilli(), ExpiresAt: token.ExpiresAt.UnixMilli()}
	if token.LastUsedAt != nil {
		value := token.LastUsedAt.UnixMilli()
		result.LastUsedAt = &value
	}
	return result
}
func (remote *transport) account(w http.ResponseWriter, r *http.Request, p principal) {
	switch {
	case r.URL.Path == "/api/v2/auth/logout":
		if r.Method != http.MethodPost {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		if err := remote.identity.Logout(r.Context(), p.secret); err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		remote.cookie(w, "", time.Time{})
		w.WriteHeader(204)
	case r.URL.Path == "/api/v2/account/password":
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
		if err := remote.identity.ChangePassword(r.Context(), p.account.ID, input.CurrentPassword, input.NewPassword); err != nil {
			if errors.Is(err, identityservice.ErrAuthentication) {
				authhttp.Problem(w, 403, "Current password incorrect")
				return
			}
			authhttp.IdentityError(w, err)
			return
		}
		remote.cookie(w, "", time.Time{})
		w.WriteHeader(204)
	case r.URL.Path == "/api/v2/account/tokens":
		if r.Method == http.MethodGet {
			tokens, err := remote.identity.Tokens(r.Context(), p.account.ID)
			if err != nil {
				authhttp.IdentityError(w, err)
				return
			}
			values := make([]tokenJSON, 0, len(tokens))
			for _, token := range tokens {
				values = append(values, projectToken(token))
			}
			authhttp.WriteJSON(w, 200, struct {
				Tokens []tokenJSON `json:"tokens"`
			}{values})
			return
		}
		if r.Method != http.MethodPost {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		var input struct {
			Name          string                  `json:"name"`
			Purpose       identityservice.Purpose `json:"purpose"`
			ExpiresInDays int                     `json:"expiresInDays"`
		}
		if !authhttp.DecodeJSON(w, r, &input) {
			return
		}
		if input.ExpiresInDays < 1 || input.ExpiresInDays > 365 {
			authhttp.Problem(w, 400, "Token expiry must be 1–365 days")
			return
		}
		token, err := remote.identity.CreateToken(r.Context(), p.account.ID, input.Name, input.Purpose, time.Now().Add(time.Duration(input.ExpiresInDays)*24*time.Hour))
		if err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		authhttp.WriteJSON(w, 201, struct {
			Token  tokenJSON `json:"token"`
			Secret string    `json:"secret"`
		}{projectToken(token.Token), token.Secret})
	case strings.HasPrefix(r.URL.Path, "/api/v2/account/tokens/"):
		if r.Method != http.MethodDelete {
			authhttp.Problem(w, 405, "Method not allowed")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v2/account/tokens/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if err := remote.identity.RevokeToken(r.Context(), p.account.ID, id); err != nil {
			authhttp.IdentityError(w, err)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
