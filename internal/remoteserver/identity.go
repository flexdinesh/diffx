package remoteserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/packages/api"
)

const sessionCookie = "diffx_session"

type transport struct {
	identity     *identityservice.Service
	applications *registry
	settings     Settings
	assets       fs.FS
	limiter      *authLimiter
	authSlots    chan struct{}
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
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Email    *string `json:"email"`
	Admin    bool    `json:"admin"`
}
type bootstrapJSON struct {
	Mode          string       `json:"mode"`
	GoogleEnabled bool         `json:"googleEnabled"`
	User          *accountJSON `json:"user"`
	CSRFToken     *string      `json:"csrfToken"`
}

func (remote *transport) bootstrap(w http.ResponseWriter, r *http.Request, p principal) {
	result := bootstrapJSON{Mode: "remote", GoogleEnabled: remote.identity.GoogleEnabled()}
	if p.session != nil {
		email, err := remote.identity.GoogleEmail(r.Context(), p.account.ID)
		if err != nil {
			identityError(w, err)
			return
		}
		result.User = &accountJSON{ID: p.account.ID, Username: p.account.Username, Admin: p.account.Admin}
		if email != "" {
			result.User.Email = &email
		}
		result.CSRFToken = &p.session.CSRFToken
	}
	writeJSON(w, 200, result)
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
	cookie, err := r.Cookie(sessionCookie)
	if errors.Is(err, http.ErrNoCookie) {
		return principal{}, nil
	}
	if err != nil {
		return principal{}, identityservice.ErrAuthentication
	}
	session, err := remote.identity.AuthenticateSession(r.Context(), cookie.Value)
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
		problem(w, 403, "Cross-origin access denied")
		return
	}
	// Login remains available with an expired browser cookie. Explicit bearer
	// credentials are always validated; never silently fall back to cookies.
	authForm := r.URL.Path == "/api/v2/auth/login" || r.URL.Path == "/api/v2/setup" || r.URL.Path == "/api/v2/auth/google" ||
		r.URL.Path == "/api/v2/invites/lookup" || r.URL.Path == "/api/v2/invites/google"
	public := r.URL.Path == "/api/v2/bootstrap" || (!strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/mcp") && !strings.HasPrefix(r.URL.Path, "/control/"))
	p, err := remote.authenticate(r)
	if errors.Is(err, identityservice.ErrAuthentication) && r.Header.Get("Authorization") == "" && (public || authForm) {
		remote.cookie(w, "", time.Time{})
		p = principal{}
		err = nil
	}
	if err != nil {
		identityError(w, err)
		return
	}
	if authForm {
		remote.auth(w, r)
		return
	}
	if r.URL.Path == "/api/v2/bootstrap" {
		if r.Method != http.MethodGet {
			problem(w, 405, "Method not allowed")
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
		unauthorized(w)
		return
	}
	if p.session != nil && unsafe(r.Method) && !validCSRF(r, p.session.CSRFToken) {
		problem(w, 403, "CSRF token required")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/github/") {
		if p.session == nil {
			problem(w, 403, "Browser session required")
			return
		}
		remote.githubAccount(w, r, p)
		return
	}
	if r.URL.Path == "/api/v2/push" || strings.HasPrefix(r.URL.Path, "/api/v2/push/") {
		if p.session == nil {
			problem(w, 403, "Browser session required")
			return
		}
		remote.pushRoutes(w, r, p)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/manage/") {
		if p.session == nil || !p.account.Admin {
			problem(w, 403, "Admin browser session required")
			return
		}
		remote.admin(w, r, p)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v2/account/") || r.URL.Path == "/api/v2/auth/logout" {
		if p.session == nil {
			problem(w, 403, "Browser session required")
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
		problem(w, 403, "Collector token cannot access this resource")
		return
	}
	app, err := remote.applications.get(p.account)
	if err != nil {
		identityError(w, err)
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
func unsafe(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
func validCSRF(r *http.Request, expected string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(expected)) == 1
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
				_, err = remote.identity.AuthenticateSession(ctx, p.secret)
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
		problem(w, 405, "Method not allowed")
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

// auth serves the credential forms: admin password login, one-time admin setup,
// Google sign-in starts and invite lookups. All are bounded like login because
// each verifies a secret or starts a sign-in.
func (remote *transport) auth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v2/setup" && r.Method == http.MethodGet {
		required, err := remote.identity.SetupRequired(r.Context())
		if err != nil {
			identityError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			Required bool `json:"required"`
		}{required})
		return
	}
	if r.Method != http.MethodPost {
		problem(w, 405, "Method not allowed")
		return
	}
	var client string
	if remote.limiter != nil {
		var ok bool
		if client, ok = remote.limiter.client(r); !ok {
			problem(w, 400, "Client IP header missing or invalid")
			return
		}
	}
	select {
	case remote.authSlots <- struct{}{}:
		defer func() { <-remote.authSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		problem(w, 429, "Authentication busy")
		return
	}
	if remote.limiter != nil {
		allowed, err := remote.limiter.allow(r.Context(), client)
		if err != nil {
			problem(w, 500, "Server could not process request")
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			problem(w, 429, "Too many authentication attempts")
			return
		}
	}
	switch r.URL.Path {
	case "/api/v2/auth/google", "/api/v2/invites/google":
		remote.googleStart(w, r)
		return
	case "/api/v2/invites/lookup":
		remote.inviteLookup(w, r)
		return
	}
	var session identityservice.Session
	var err error
	if r.URL.Path == "/api/v2/setup" {
		var input struct {
			Token    string `json:"token"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		session, err = remote.identity.CompleteSetup(r.Context(), input.Token, input.Username, input.Password)
		if errors.Is(err, identityservice.ErrNotFound) {
			problem(w, 404, "Setup link is invalid, expired or already used")
			return
		}
	} else {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		session, err = remote.identity.Login(r.Context(), input.Username, input.Password)
	}
	if err != nil {
		identityError(w, err)
		return
	}
	remote.cookie(w, session.Secret, session.ExpiresAt)
	remote.bootstrap(w, r, principal{account: session.Account, session: &session})
}
func (remote *transport) cookie(w http.ResponseWriter, secret string, expiry time.Time) {
	cookie := &http.Cookie{Name: sessionCookie, Value: secret, Path: "/", HttpOnly: true, Secure: !remote.settings.InsecureCookies, SameSite: http.SameSiteLaxMode, Expires: expiry}
	if secret == "" {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, cookie)
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
			problem(w, 405, "Method not allowed")
			return
		}
		if err := remote.identity.Logout(r.Context(), p.secret); err != nil {
			identityError(w, err)
			return
		}
		remote.cookie(w, "", time.Time{})
		w.WriteHeader(204)
	case r.URL.Path == "/api/v2/account/password":
		if r.Method != http.MethodPut {
			problem(w, 405, "Method not allowed")
			return
		}
		var input struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := remote.identity.ChangePassword(r.Context(), p.account.ID, input.CurrentPassword, input.NewPassword); err != nil {
			if errors.Is(err, identityservice.ErrAuthentication) {
				problem(w, 403, "Current password incorrect")
				return
			}
			identityError(w, err)
			return
		}
		remote.cookie(w, "", time.Time{})
		w.WriteHeader(204)
	case r.URL.Path == "/api/v2/account/tokens":
		if r.Method == http.MethodGet {
			tokens, err := remote.identity.Tokens(r.Context(), p.account.ID)
			if err != nil {
				identityError(w, err)
				return
			}
			values := make([]tokenJSON, 0, len(tokens))
			for _, token := range tokens {
				values = append(values, projectToken(token))
			}
			writeJSON(w, 200, struct {
				Tokens []tokenJSON `json:"tokens"`
			}{values})
			return
		}
		if r.Method != http.MethodPost {
			problem(w, 405, "Method not allowed")
			return
		}
		var input struct {
			Name          string                  `json:"name"`
			Purpose       identityservice.Purpose `json:"purpose"`
			ExpiresInDays int                     `json:"expiresInDays"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.ExpiresInDays < 1 || input.ExpiresInDays > 365 {
			problem(w, 400, "Token expiry must be 1–365 days")
			return
		}
		token, err := remote.identity.CreateToken(r.Context(), p.account.ID, input.Name, input.Purpose, time.Now().Add(time.Duration(input.ExpiresInDays)*24*time.Hour))
		if err != nil {
			identityError(w, err)
			return
		}
		writeJSON(w, 201, struct {
			Token  tokenJSON `json:"token"`
			Secret string    `json:"secret"`
		}{projectToken(token.Token), token.Secret})
	case strings.HasPrefix(r.URL.Path, "/api/v2/account/tokens/"):
		if r.Method != http.MethodDelete {
			problem(w, 405, "Method not allowed")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v2/account/tokens/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if err := remote.identity.RevokeToken(r.Context(), p.account.ID, id); err != nil {
			identityError(w, err)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		problem(w, 415, "Expected application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		problem(w, 400, "Malformed request")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		problem(w, 400, "Trailing request data")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="diffx"`)
	problem(w, 401, "Authentication required")
}
func identityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identityservice.ErrAuthentication):
		unauthorized(w)
	case errors.Is(err, identityservice.ErrInvalidInput):
		problem(w, 400, "Invalid account or credential input")
	case errors.Is(err, identityservice.ErrConflict):
		problem(w, 409, "Username unavailable")
	case errors.Is(err, identityservice.ErrNotFound):
		problem(w, 404, "Not found")
	case errors.Is(err, identityservice.ErrForbidden):
		problem(w, 403, "The admin account cannot be changed here")
	default:
		problem(w, 500, "Server could not process request")
	}
}
func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{"about:blank", http.StatusText(status), status, detail})
}
