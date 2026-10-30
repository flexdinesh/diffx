package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// DefaultConsolePort keeps diffx-console beside diffx-server (7980) and off the
// local `diffx [PATH]` range (7981-7990).
const DefaultConsolePort = 7970

// ConsoleValues configures diffx-console, the admin console over the server's
// database. It comes only from DIFFX_CONSOLE_* environment variables.
type ConsoleValues struct {
	Host string
	Port int
	// DatabaseURL is the server's Postgres connection string; never echoed.
	DatabaseURL string
	// PublicURL is the console's browser-facing origin; AppURL is the app's,
	// used in invite and member reset links.
	PublicURL       string
	AppURL          string
	InsecureCookies bool
	// Google is the console's own OAuth client, with its callback on PublicURL.
	Google         Google
	AuthRateLimit  bool
	ClientIPHeader string
}

// LoadConsole resolves defaults, then the environment.
func LoadConsole() (ConsoleValues, error) {
	values := ConsoleValues{Host: "127.0.0.1", Port: DefaultConsolePort}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"DIFFX_CONSOLE_HOST", &values.Host},
		{"DIFFX_CONSOLE_DATABASE_URL", &values.DatabaseURL},
		{"DIFFX_CONSOLE_PUBLIC_URL", &values.PublicURL},
		{"DIFFX_CONSOLE_APP_URL", &values.AppURL},
		{"DIFFX_CONSOLE_GOOGLE_CLIENT_ID", &values.Google.ClientID},
		{"DIFFX_CONSOLE_GOOGLE_CLIENT_SECRET", &values.Google.ClientSecret},
		{"DIFFX_CONSOLE_CLIENT_IP_HEADER", &values.ClientIPHeader},
	} {
		if value, exists := os.LookupEnv(field.name); exists {
			*field.target = value
		}
	}
	if raw, exists := os.LookupEnv("DIFFX_CONSOLE_PORT"); exists {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return values, fmt.Errorf("invalid DIFFX_CONSOLE_PORT: %w", err)
		}
		values.Port = value
	}
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"DIFFX_CONSOLE_INSECURE_COOKIES", &values.InsecureCookies},
		{"DIFFX_CONSOLE_AUTH_RATE_LIMIT", &values.AuthRateLimit},
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
func (values ConsoleValues) ValidateDatabase() error {
	if values.DatabaseURL == "" {
		return errors.New("DIFFX_CONSOLE_DATABASE_URL is required: the diffx-server Postgres connection string")
	}
	return nil
}

// Validate checks everything Run needs: the app origin for invite and reset
// links, the optional console origin, complete Google sign-in and rate limiting.
func (values ConsoleValues) Validate() error {
	if err := values.ValidateDatabase(); err != nil {
		return err
	}
	if !validOrigin(values.AppURL, values.InsecureCookies) {
		return errors.New("DIFFX_CONSOLE_APP_URL is required: the app's https origin, such as https://diffx.example.com (http only with insecure cookies)")
	}
	if values.PublicURL != "" && !validOrigin(values.PublicURL, values.InsecureCookies) {
		return errors.New("DIFFX_CONSOLE_PUBLIC_URL must be an https origin such as https://console.diffx.example.com (http only with insecure cookies)")
	}
	if values.Google.Enabled() {
		if values.Google.ClientID == "" || values.Google.ClientSecret == "" {
			return errors.New("incomplete Google sign-in; set both DIFFX_CONSOLE_GOOGLE_CLIENT_ID and DIFFX_CONSOLE_GOOGLE_CLIENT_SECRET")
		}
		if values.PublicURL == "" {
			return errors.New("Google sign-in needs DIFFX_CONSOLE_PUBLIC_URL for its callback")
		}
	}
	if values.AuthRateLimit && !validHeaderName(values.ClientIPHeader) {
		return errors.New("DIFFX_CONSOLE_AUTH_RATE_LIMIT needs DIFFX_CONSOLE_CLIENT_IP_HEADER: the request header name, such as X-Real-IP, that your trusted ingress sets to the single client IP")
	}
	return nil
}

func (values ConsoleValues) ListenAddress() (string, error) {
	return listenAddress("console", values.Host, values.Port)
}
