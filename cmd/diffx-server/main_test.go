package main

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/remoteserver"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// databaseURL is a well-formed connection string for tests that only parse settings.
const databaseURL = "postgres://diffx:hunter2@db.example.com:5432/diffx"

func withDatabaseURL(t *testing.T) {
	t.Helper()
	t.Setenv("DIFFX_SERVER_DATABASE_URL", databaseURL)
}

func TestRemoteConfigMappingAndPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"host":"::1","port":4123}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	withDatabaseURL(t)
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:7980" || settings.DatabaseURL != databaseURL {
		t.Fatalf("remote default changed: %v", err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Listen != "[::1]:4123" || settings.DatabaseURL != databaseURL {
		t.Fatalf("config mapping: %v", err)
	}
	t.Setenv("DIFFX_SERVER_HOST", "127.0.0.2")
	t.Setenv("DIFFX_SERVER_PORT", "5123")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Listen != "127.0.0.2:5123" || settings.DatabaseURL != databaseURL {
		t.Fatalf("environment mapping: %v", err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path, "--listen", "0.0.0.0:6123"}, io.Discard)
	if err != nil || settings.Listen != "0.0.0.0:6123" || settings.DatabaseURL != databaseURL {
		t.Fatalf("flags mapping: %v", err)
	}
	// The connection string holds credentials, so no flag can carry it.
	for _, flag := range []string{"--state", "--database-url"} {
		if _, _, err := parseSettings([]string{flag, "postgres://diffx:hunter2@db/diffx"}, io.Discard); err == nil {
			t.Fatalf("database flag accepted: %s", flag)
		}
	}
	t.Setenv("DIFFX_SERVER_HOST", "invalid")
	t.Setenv("DIFFX_SERVER_PORT", "65536")
	settings, _, err = parseSettings([]string{"--listen", "127.0.0.1:0"}, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:0" {
		t.Fatalf("overridden listener was validated: %+v %v", settings, err)
	}
}

func TestRemoteConfigDefaultsAndVersion(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	withDatabaseURL(t)
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:7980" {
		t.Fatalf("default server port: %#v %v", settings, err)
	}
	if _, showVersion, err := parseSettings([]string{"--version", "--config-file", "/missing/config.json"}, io.Discard); err != nil || !showVersion {
		t.Fatalf("version resolved config: %v", err)
	}
}

func TestRemoteIdentityConfigPrecedence(t *testing.T) {
	withDatabaseURL(t)
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"publicUrl":"https://file.example.com","insecureCookies":true}`), 0o400); err != nil {
		t.Fatal(err)
	}
	settings, _, err := parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.PublicURL != "https://file.example.com" || !settings.InsecureCookies {
		t.Fatalf("file identity: %+v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_PUBLIC_URL", "https://env.example.com")
	t.Setenv("DIFFX_SERVER_INSECURE_COOKIES", "false")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.PublicURL != "https://env.example.com" || settings.InsecureCookies {
		t.Fatalf("environment identity: %+v %v", settings, err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path, "--public-url", "http://127.0.0.1:7980", "--insecure-cookies"}, io.Discard)
	if err != nil || settings.PublicURL != "http://127.0.0.1:7980" || !settings.InsecureCookies {
		t.Fatalf("flag identity: %+v %v", settings, err)
	}
	for _, document := range []string{`{"username":"admin"}`, `{"signupEnabled":true}`} {
		retired := filepath.Join(t.TempDir(), "retired.json")
		if err := os.WriteFile(retired, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := parseSettings([]string{"--config-file", retired}, io.Discard); err == nil {
			t.Fatalf("retired setting accepted: %s", document)
		}
	}
	for _, flag := range []string{"--username", "--signup-enabled"} {
		if _, _, err := parseSettings([]string{flag, "x"}, io.Discard); err == nil {
			t.Fatalf("retired flag accepted: %s", flag)
		}
	}
}

func TestRemoteGoogleSignInSettings(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	withDatabaseURL(t)
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Google != nil {
		t.Fatalf("Google sign-in without configuration: %+v %v", settings.Google, err)
	}
	t.Setenv("DIFFX_SERVER_GOOGLE_CLIENT_ID", "client")
	t.Setenv("DIFFX_SERVER_GOOGLE_CLIENT_SECRET", "secret")
	if _, _, err := parseSettings(nil, io.Discard); err == nil {
		t.Fatal("Google sign-in accepted without a public URL")
	}
	settings, _, err = parseSettings([]string{"--public-url", "https://diffx.example.com"}, io.Discard)
	if err != nil || settings.Google == nil || settings.Google.ClientID != "client" || settings.Google.ClientSecret != "secret" {
		t.Fatalf("Google sign-in settings: %+v %v", settings.Google, err)
	}
}

func TestRemoteAuthRateLimitSettings(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	withDatabaseURL(t)
	t.Setenv("DIFFX_SERVER_CLIENT_IP_HEADER", "X-Real-IP")
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.ClientIPHeader != "" {
		t.Fatalf("disabled rate limit kept header: %+v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_AUTH_RATE_LIMIT", "true")
	settings, _, err = parseSettings(nil, io.Discard)
	if err != nil || settings.ClientIPHeader != "X-Real-IP" {
		t.Fatalf("enabled rate limit: %+v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_CLIENT_IP_HEADER", "")
	if _, _, err := parseSettings(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "DIFFX_SERVER_CLIENT_IP_HEADER") {
		t.Fatalf("rate limit without client IP header: %v", err)
	}
}

func TestRemoteRejectsInvalidDeploymentSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DIFFX_SERVER_HOST", "localhost"},
		{"DIFFX_SERVER_PORT", "not-a-port"},
		{"DIFFX_SERVER_PORT", "65536"},
		{"DIFFX_SERVER_DATABASE_URL", ""},
		{"DIFFX_SERVER_RETENTION_DAYS", "0"},
		{"DIFFX_SERVER_RETENTION_DAYS", "-1"},
		{"DIFFX_SERVER_INSECURE_COOKIES", "bad"},
		{"DIFFX_SERVER_PUBLIC_URL", "http://diffx.example.com"},
		{"DIFFX_SERVER_PUBLIC_URL", "https://diffx.example.com/path"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("DIFFX_SERVER_CONFIG", "")
			withDatabaseURL(t)
			t.Setenv(tc.key, tc.value)
			if _, _, err := parseSettings(nil, io.Discard); err == nil {
				t.Fatal("accepted invalid deployment settings")
			}
		})
	}
}

func TestServerRequiresDatabaseURLWithoutEchoingIt(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	t.Setenv("DIFFX_SERVER_DATABASE_URL", "")
	if _, _, err := parseSettings(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "DIFFX_SERVER_DATABASE_URL") {
		t.Fatalf("server started without a database: %v", err)
	}
	// A rejected connection string or credential is reported without its password.
	rejected, err := url.Parse(pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	rejected.User = url.UserPassword(rejected.User.Username(), "wrong-hunter2")
	for _, value := range []string{"postgres://diffx:wrong-hunter2@[bad", rejected.String()} {
		t.Setenv("DIFFX_SERVER_DATABASE_URL", value)
		settings, _, err := parseSettings(nil, io.Discard)
		if err != nil || settings.DatabaseURL != value {
			t.Fatalf("database URL from environment: %v", err)
		}
		settings.Listen = "127.0.0.1:0"
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		err = remoteserver.Run(ctx, settings, nil)
		cancel()
		if err == nil || strings.Contains(err.Error(), "wrong-hunter2") {
			t.Fatalf("connection failure must not echo the password: %v", err)
		}
	}
}

func TestRemoteRetentionConfigPrecedence(t *testing.T) {
	withDatabaseURL(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"retentionDays":14}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _, err := parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.RetentionDays != 14 {
		t.Fatalf("retention config: %+v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_RETENTION_DAYS", "21")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.RetentionDays != 21 {
		t.Fatalf("retention environment: %+v %v", settings, err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path, "--retention-days", "3"}, io.Discard)
	if err != nil || settings.RetentionDays != 3 {
		t.Fatalf("retention flag: %+v %v", settings, err)
	}
	for _, value := range []string{"0", "-1", "1000000000"} {
		if _, _, err := parseSettings([]string{"--config-file", path, "--retention-days", value}, io.Discard); err == nil {
			t.Fatalf("invalid retention accepted: %s", value)
		}
	}
}

func TestBootstrapSecretsCannotBeFlags(t *testing.T) {
	withDatabaseURL(t)
	for _, name := range []string{"--password", "--token"} {
		if _, _, err := parseSettings([]string{name, "secret"}, io.Discard); err == nil {
			t.Fatalf("secret flag accepted: %s", name)
		}
	}
}

func TestServerNeverResetsState(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	withDatabaseURL(t)
	if _, _, err := parseSettings([]string{"--reset-state"}, io.Discard); err == nil {
		t.Fatal("server accepted --reset-state")
	}
}

func TestRemovedSignInSettingsRefuseStartup(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	withDatabaseURL(t)
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Google != nil {
		t.Fatalf("server without Google: %+v %v", settings, err)
	}
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"signup":false}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseSettings([]string{"--config-file", path}, io.Discard); err == nil || !strings.Contains(err.Error(), "console's Settings") {
		t.Fatalf("removed config key accepted: %v", err)
	}
	t.Setenv("DIFFX_SERVER_PASSWORD_SIGNIN", "false")
	if _, _, err := parseSettings(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "DIFFX_SERVER_PASSWORD_SIGNIN was removed") {
		t.Fatalf("removed variable accepted: %v", err)
	}
}
