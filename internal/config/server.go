package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// DefaultServerPort keeps diffx-server off the local `diffx [PATH]` range (7981-7990)
// so both modes can run on one machine.
const DefaultServerPort = 7980

// DefaultInternalPort serves the GitHub fetcher protocol; never publish it.
const DefaultInternalPort = 7979

// ServerValues is deployment configuration, independent of personal CLI settings.
type ServerValues struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// DatabaseURL is the Postgres connection string. It holds credentials, so it
	// comes only from DIFFX_SERVER_DATABASE_URL, never a file or flag, and is never echoed.
	DatabaseURL     string `json:"-"`
	PublicURL       string `json:"publicUrl"`
	InsecureCookies bool   `json:"insecureCookies"`
	RetentionDays   int    `json:"retentionDays"`
	// InternalPort listens beside Port, on the same host, when GitHub is enabled.
	InternalPort int    `json:"internalPort"`
	GitHub       GitHub `json:"github"`
	Google       Google `json:"google"`
	// Signup lets people create accounts from the web app without an invite.
	Signup bool `json:"signup"`
	// PasswordSignIn admits password accounts to the web app. Both default to
	// true for development and self-hosting; production turns them off.
	PasswordSignIn bool `json:"passwordSignIn"`
	// AuthRateLimit bounds authentication attempts per client IP across
	// replicas. The IP comes from ClientIPHeader, which a trusted ingress must
	// set; both come only from the environment.
	AuthRateLimit  bool   `json:"-"`
	ClientIPHeader string `json:"-"`
}

// Google configures Google sign-in. The secret comes only
// from DIFFX_SERVER_GOOGLE_CLIENT_SECRET, never the config file.
type Google struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"-"`
}

// Enabled reports whether any Google setting is present; Validate requires all.
func (google Google) Enabled() bool { return google != Google{} }

// GitHub configures the optional GitHub App integration. Secrets come only from
// DIFFX_SERVER_GITHUB_* environment variables, never the config file.
type GitHub struct {
	AppID          int64  `json:"appId"`
	ClientID       string `json:"clientId"`
	PrivateKeyFile string `json:"privateKeyFile"`
	PublicURL      string `json:"publicUrl"`
	ClientSecret   string `json:"-"`
	WebhookSecret  string `json:"-"`
	TokenKey       string `json:"-"`
	// FetcherSecret authenticates diffx-fetcher on the internal listener.
	FetcherSecret string `json:"-"`
}

// Enabled reports whether any GitHub setting is present; Validate requires all.
func (github GitHub) Enabled() bool {
	return github != GitHub{}
}

// Validate requires the complete integration or none of it. publicUrl is the
// browser-facing origin for OAuth callbacks; plain HTTP is development only.
func (github GitHub) Validate(insecureCookies bool) error {
	if !github.Enabled() {
		return nil
	}
	missing := make([]string, 0)
	for _, field := range []struct {
		name    string
		present bool
	}{
		{"DIFFX_SERVER_GITHUB_APP_ID", github.AppID > 0},
		{"DIFFX_SERVER_GITHUB_CLIENT_ID", github.ClientID != ""},
		{"DIFFX_SERVER_GITHUB_PRIVATE_KEY_FILE", github.PrivateKeyFile != ""},
		{"DIFFX_SERVER_GITHUB_PUBLIC_URL", github.PublicURL != ""},
		{"DIFFX_SERVER_GITHUB_CLIENT_SECRET", github.ClientSecret != ""},
		{"DIFFX_SERVER_GITHUB_WEBHOOK_SECRET", github.WebhookSecret != ""},
		{"DIFFX_SERVER_GITHUB_TOKEN_KEY", github.TokenKey != ""},
		{"DIFFX_SERVER_FETCHER_SECRET", github.FetcherSecret != ""},
	} {
		if !field.present {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("incomplete GitHub integration; also set %s", strings.Join(missing, ", "))
	}
	if !validOrigin(github.PublicURL, insecureCookies) {
		return errors.New("GitHub publicUrl must be an https origin such as https://diffx.example.com (http only with insecure cookies)")
	}
	key, err := base64.StdEncoding.DecodeString(github.TokenKey)
	if err != nil || len(key) != 32 {
		return errors.New("DIFFX_SERVER_GITHUB_TOKEN_KEY must be 32 random bytes in standard base64, such as the output of: openssl rand -base64 32")
	}
	if len(github.FetcherSecret) < 32 {
		return errors.New("DIFFX_SERVER_FETCHER_SECRET must be at least 32 characters, such as the output of: openssl rand -base64 32")
	}
	return nil
}

// validOrigin accepts a browser-facing origin; plain HTTP is development only.
func validOrigin(raw string, insecureCookies bool) bool {
	public, err := url.Parse(raw)
	return err == nil && public.Host != "" && public.User == nil && public.RawQuery == "" && public.Fragment == "" && strings.TrimSuffix(public.Path, "/") == "" &&
		(public.Scheme == "https" || public.Scheme == "http" && insecureCookies)
}

// ValidateIdentity checks the optional public origin, that people have a way
// to sign in, and Google sign-in, which needs both credentials and that origin.
func (values ServerValues) ValidateIdentity() error {
	if values.PublicURL != "" && !validOrigin(values.PublicURL, values.InsecureCookies) {
		return errors.New("publicUrl must be an https origin such as https://diffx.example.com (http only with insecure cookies)")
	}
	if !values.Google.Enabled() {
		if !values.PasswordSignIn {
			return errors.New("no sign-in method: set DIFFX_SERVER_GOOGLE_CLIENT_ID and DIFFX_SERVER_GOOGLE_CLIENT_SECRET, or DIFFX_SERVER_PASSWORD_SIGNIN=true")
		}
		return nil
	}
	if values.Google.ClientID == "" || values.Google.ClientSecret == "" {
		return errors.New("incomplete Google sign-in; set both DIFFX_SERVER_GOOGLE_CLIENT_ID and DIFFX_SERVER_GOOGLE_CLIENT_SECRET")
	}
	if values.PublicURL == "" {
		return errors.New("Google sign-in needs DIFFX_SERVER_PUBLIC_URL for its callback")
	}
	return nil
}

// LoadServerFile resolves defaults, an explicitly selected file, then environment.
// It never discovers personal config, creates files, or acquires a config lock.
// Variables use the DIFFX_SERVER_ prefix so they never configure the local CLI.
func LoadServerFile(path string) (ServerValues, error) {
	values := ServerValues{Host: "127.0.0.1", Port: DefaultServerPort, InternalPort: DefaultInternalPort, RetentionDays: 7, Signup: true, PasswordSignIn: true}
	if path == "" {
		path = os.Getenv("DIFFX_SERVER_CONFIG")
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return values, fmt.Errorf("read server config: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		document := &values
		if err := decoder.Decode(&document); err != nil {
			return values, fmt.Errorf("invalid server config: %w", err)
		}
		if document == nil {
			return values, errors.New("server config must be a JSON object")
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			return values, errors.New("server config must contain one JSON object")
		}
	}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"DIFFX_SERVER_HOST", &values.Host},
		{"DIFFX_SERVER_DATABASE_URL", &values.DatabaseURL},
		{"DIFFX_SERVER_PUBLIC_URL", &values.PublicURL},
		{"DIFFX_SERVER_GOOGLE_CLIENT_ID", &values.Google.ClientID},
		{"DIFFX_SERVER_GOOGLE_CLIENT_SECRET", &values.Google.ClientSecret},
		{"DIFFX_SERVER_GITHUB_CLIENT_ID", &values.GitHub.ClientID},
		{"DIFFX_SERVER_GITHUB_PRIVATE_KEY_FILE", &values.GitHub.PrivateKeyFile},
		{"DIFFX_SERVER_GITHUB_PUBLIC_URL", &values.GitHub.PublicURL},
		{"DIFFX_SERVER_GITHUB_CLIENT_SECRET", &values.GitHub.ClientSecret},
		{"DIFFX_SERVER_GITHUB_WEBHOOK_SECRET", &values.GitHub.WebhookSecret},
		{"DIFFX_SERVER_GITHUB_TOKEN_KEY", &values.GitHub.TokenKey},
		{"DIFFX_SERVER_FETCHER_SECRET", &values.GitHub.FetcherSecret},
		{"DIFFX_SERVER_CLIENT_IP_HEADER", &values.ClientIPHeader},
	} {
		if value, exists := os.LookupEnv(field.name); exists {
			*field.target = value
		}
	}
	for _, field := range []struct {
		name   string
		target *int
	}{
		{"DIFFX_SERVER_PORT", &values.Port},
		{"DIFFX_SERVER_INTERNAL_PORT", &values.InternalPort},
		{"DIFFX_SERVER_RETENTION_DAYS", &values.RetentionDays},
	} {
		if raw, exists := os.LookupEnv(field.name); exists {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return values, fmt.Errorf("invalid %s: %w", field.name, err)
			}
			*field.target = value
		}
	}
	// Compose passes unset integration variables through as empty.
	if raw := os.Getenv("DIFFX_SERVER_GITHUB_APP_ID"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return values, fmt.Errorf("invalid DIFFX_SERVER_GITHUB_APP_ID: %w", err)
		}
		values.GitHub.AppID = value
	}
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"DIFFX_SERVER_INSECURE_COOKIES", &values.InsecureCookies},
		{"DIFFX_SERVER_AUTH_RATE_LIMIT", &values.AuthRateLimit},
		{"DIFFX_SERVER_SIGNUP", &values.Signup},
		{"DIFFX_SERVER_PASSWORD_SIGNIN", &values.PasswordSignIn},
	} {
		if raw, exists := os.LookupEnv(field.name); exists {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return values, fmt.Errorf("invalid %s: %w", field.name, err)
			}
			*field.target = value
		}
	}
	return values, nil
}

// ValidateDatabase requires the Postgres connection string without echoing it.
func (values ServerValues) ValidateDatabase() error {
	if values.DatabaseURL == "" {
		return errors.New("DIFFX_SERVER_DATABASE_URL is required: a Postgres connection string such as postgres://diffx@db.example.com:5432/diffx?sslmode=verify-full")
	}
	return nil
}

// ValidateAuthRateLimit requires a header name when limiting is enabled; the
// server cannot tell a client from its ingress without one.
func (values ServerValues) ValidateAuthRateLimit() error {
	if !values.AuthRateLimit {
		return nil
	}
	if values.ClientIPHeader == "" || strings.IndexFunc(values.ClientIPHeader, func(r rune) bool {
		return r != '-' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z')
	}) >= 0 {
		return errors.New("DIFFX_SERVER_AUTH_RATE_LIMIT needs DIFFX_SERVER_CLIENT_IP_HEADER: the request header name, such as X-Real-IP, that your trusted ingress sets to the single client IP")
	}
	return nil
}

// ListenAddress validates the configured listener when no flag overrides it.
func (values ServerValues) ListenAddress() (string, error) {
	if net.ParseIP(values.Host) == nil {
		return "", errors.New("server host must be an IP address")
	}
	if values.Port < 0 || values.Port > 65535 {
		return "", errors.New("server port must be between 0 and 65535")
	}
	return net.JoinHostPort(values.Host, strconv.Itoa(values.Port)), nil
}

// InternalAddress places the internal listener on the public listener's host.
func (values ServerValues) InternalAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if values.InternalPort <= 0 || values.InternalPort > 65535 || strconv.Itoa(values.InternalPort) == port {
		return "", errors.New("server internal port must be between 1 and 65535 and differ from the server port")
	}
	return net.JoinHostPort(host, strconv.Itoa(values.InternalPort)), nil
}
