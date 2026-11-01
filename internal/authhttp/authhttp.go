// Package authhttp holds the HTTP rules the remote server and the console
// share: problem responses, JSON bodies, identity error mapping, session
// cookies, CSRF, authentication admission and deployment probes.
package authhttp

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
)

// SessionCookie names the browser session cookie on each origin.
const SessionCookie = "diffx_session"

// SetSession writes the session cookie, or clears it for an empty secret.
func SetSession(w http.ResponseWriter, secret string, expiry time.Time, insecure bool) {
	cookie := &http.Cookie{Name: SessionCookie, Value: secret, Path: "/", HttpOnly: true, Secure: !insecure, SameSite: http.SameSiteLaxMode, Expires: expiry}
	if secret == "" {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, cookie)
}

// Unsafe reports a method that browser sessions must protect with CSRF.
func Unsafe(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func ValidCSRF(r *http.Request, expected string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(expected)) == 1
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		Problem(w, 415, "Expected application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		Problem(w, 400, "Malformed request")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		Problem(w, 400, "Trailing request data")
		return false
	}
	return true
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="diffx"`)
	Problem(w, 401, "Authentication required")
}

// IdentityError maps identity outcomes to problem responses.
func IdentityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identityservice.ErrAuthentication):
		Unauthorized(w)
	case errors.Is(err, identityservice.ErrInvalidEmail):
		Problem(w, 400, "Enter a valid email address (up to 254 bytes)")
	case errors.Is(err, identityservice.ErrInviteMismatch):
		Problem(w, 400, "Use the email address this invite was sent to")
	case errors.Is(err, identityservice.ErrInvalidInput):
		Problem(w, 400, "Invalid account or credential input")
	case errors.Is(err, identityservice.ErrConflict):
		Problem(w, 409, "Account already exists")
	case errors.Is(err, identityservice.ErrNotFound), errors.Is(err, identityservice.ErrDisabled):
		Problem(w, 404, "Not found")
	case errors.Is(err, identityservice.ErrForbidden):
		Problem(w, 403, "You cannot change your own account here")
	case errors.Is(err, identityservice.ErrLastAdmin):
		Problem(w, 409, "At least one enabled admin must remain")
	default:
		Problem(w, 500, "Server could not process request")
	}
}

func Problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{"about:blank", http.StatusText(status), status, detail})
}
