package main

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

func TestRemoteConfigMappingAndPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"host":"::1","port":4123,"state":"file.db"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:7980" || settings.State != "data/state.db" {
		t.Fatalf("remote default changed: %#v %v", settings, err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Listen != "[::1]:4123" || filepath.Base(settings.State) != "file.db" {
		t.Fatalf("config mapping: %#v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_HOST", "127.0.0.2")
	t.Setenv("DIFFX_SERVER_PORT", "5123")
	t.Setenv("DIFFX_SERVER_STATE", "environment.db")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Listen != "127.0.0.2:5123" || filepath.Base(settings.State) != "environment.db" {
		t.Fatalf("environment mapping: %#v %v", settings, err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path, "--listen", "0.0.0.0:6123", "--state", "flags.db"}, io.Discard)
	if err != nil || settings.Listen != "0.0.0.0:6123" || settings.State != "flags.db" {
		t.Fatalf("flags mapping: %#v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_DATABASE_URL", "postgres://diffx:secret@db:5432/diffx")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.DatabaseURL != "postgres://diffx:secret@db:5432/diffx" {
		t.Fatalf("database URL mapping: %#v %v", settings, err)
	}
	if _, _, err := parseSettings([]string{"--config-file", path, "--state", "flags.db"}, io.Discard); err == nil {
		t.Fatal("explicit --state accepted with a database URL")
	}
	t.Setenv("DIFFX_SERVER_DATABASE_URL", "")
	t.Setenv("DIFFX_SERVER_HOST", "invalid")
	t.Setenv("DIFFX_SERVER_PORT", "65536")
	settings, _, err = parseSettings([]string{"--listen", "127.0.0.1:0"}, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:0" {
		t.Fatalf("overridden listener was validated: %+v %v", settings, err)
	}
}

func TestResetPasswordRecoversOnlyTheAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session := testsupport.RemoteAdmin(t, store, "alice", "initial-password-123")
	testsupport.RemoteAccount(t, store, "member")
	var stdout, stderr bytes.Buffer
	args := []string{"reset-password", "--username", "alice", "--state", path, "--password-stdin"}
	if err := manageUser(args, strings.NewReader("changed-password-123\n"), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("operator bypassed lock: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manageUser([]string{"reset-password", "--username", "member", "--state", path, "--password-stdin"}, strings.NewReader("member-password-123\n"), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "no admin account named member") {
		t.Fatalf("member gained a password: %v", err)
	}
	t.Setenv("TEST_ACCOUNT_PASSWORD", "changed-password-123")
	if err := manageUser([]string{"reset-password", "--username", "alice", "--state", path, "--password-env", "TEST_ACCOUNT_PASSWORD"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "password-123") {
		t.Fatal("password printed")
	}
	store, err = reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := identityservice.New(store)
	if _, err := identity.Login(t.Context(), "alice", "initial-password-123"); err == nil {
		t.Fatal("old password accepted")
	}
	if _, err := identity.AuthenticateSession(t.Context(), session.Secret); err == nil {
		t.Fatal("reset did not revoke session")
	}
	if _, err := identity.Login(t.Context(), "alice", "changed-password-123"); err != nil {
		t.Fatal(err)
	}
}
func TestUserCommandValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"delete"}, {"create", "--username", "alice", "--password-stdin"}, {"reset-password"}, {"reset-password", "--username", "alice"}, {"reset-password", "--username", "alice", "--password-env", "P", "--password-stdin"}, {"reset-password", "--username", "alice", "--state", ":memory:", "--password-stdin"}} {
		if err := manageUser(args, strings.NewReader("password-123456"), io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}

func TestRemoteConfigDefaultsAndVersion(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	settings, _, err := parseSettings(nil, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:7980" {
		t.Fatalf("default server port: %#v %v", settings, err)
	}
	if _, showVersion, err := parseSettings([]string{"--version", "--config-file", "/missing/config.json"}, io.Discard); err != nil || !showVersion {
		t.Fatalf("version resolved config: %v", err)
	}
}

func TestRemoteIdentityConfigPrecedence(t *testing.T) {
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

func TestRemoteRejectsInvalidDeploymentSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DIFFX_SERVER_HOST", "localhost"},
		{"DIFFX_SERVER_PORT", "not-a-port"},
		{"DIFFX_SERVER_PORT", "65536"},
		{"DIFFX_SERVER_STATE", ""},
		{"DIFFX_SERVER_RETENTION_DAYS", "0"},
		{"DIFFX_SERVER_RETENTION_DAYS", "-1"},
		{"DIFFX_SERVER_INSECURE_COOKIES", "bad"},
		{"DIFFX_SERVER_PUBLIC_URL", "http://diffx.example.com"},
		{"DIFFX_SERVER_PUBLIC_URL", "https://diffx.example.com/path"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("DIFFX_SERVER_CONFIG", "")
			t.Setenv(tc.key, tc.value)
			if _, _, err := parseSettings(nil, io.Discard); err == nil {
				t.Fatal("accepted invalid deployment settings")
			}
		})
	}
}

func TestRemoteRetentionConfigPrecedence(t *testing.T) {
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
	for _, name := range []string{"--password", "--token"} {
		if _, _, err := parseSettings([]string{name, "secret"}, io.Discard); err == nil {
			t.Fatalf("secret flag accepted: %s", name)
		}
	}
}

func TestServerNeverResetsState(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	if _, _, err := parseSettings([]string{"--reset-state"}, io.Discard); err == nil {
		t.Fatal("server accepted --reset-state")
	}
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE legacy(value TEXT); PRAGMA user_version=10"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	err = manageUser([]string{"reset-password", "--username", "alice", "--state", path, "--password-stdin"}, strings.NewReader("correct horse battery staple"), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "schema 10 is older") || strings.Contains(err.Error(), "--reset-state") {
		t.Fatalf("older state must be refused without a local reset hint: %v", err)
	}
	if matches, _ := filepath.Glob(path + ".schema*"); len(matches) != 0 {
		t.Fatalf("server administration backed up state: %v", matches)
	}
}
