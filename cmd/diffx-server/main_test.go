package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
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
	t.Setenv("DIFFX_SERVER_HOST", "invalid")
	t.Setenv("DIFFX_SERVER_PORT", "65536")
	settings, _, err = parseSettings([]string{"--listen", "127.0.0.1:0"}, io.Discard)
	if err != nil || settings.Listen != "127.0.0.1:0" {
		t.Fatalf("overridden listener was validated: %+v %v", settings, err)
	}
}

func TestUserCommandsPasswordLifecycleAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	var stdout, stderr bytes.Buffer
	t.Setenv("TEST_ACCOUNT_PASSWORD", "initial-password-123")
	args := []string{"create", "--username", "alice", "--state", path, "--password-env", "TEST_ACCOUNT_PASSWORD"}
	if err := manageUser(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "password-123") {
		t.Fatal("password printed")
	}
	store, err := reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := identityservice.New(store)
	session, err := identity.Login(t.Context(), "alice", "initial-password-123")
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := manageUser(args, strings.NewReader(""), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("operator bypassed lock: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manageUser(args, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("duplicate account")
	}
	if err := manageUser([]string{"reset-password", "--username", "alice", "--state", path, "--password-stdin"}, strings.NewReader("changed-password-123\n"), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	store, err = reviewstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity = identityservice.New(store)
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
	for _, args := range [][]string{nil, {"delete"}, {"create"}, {"create", "--username", "alice"}, {"create", "--username", "alice", "--password-env", "P", "--password-stdin"}, {"create", "--username", "alice", "--state", ":memory:", "--password-stdin"}} {
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
	t.Setenv("DIFFX_SERVER_PASSWORD", "password-123456")
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"username":"file-admin","signupEnabled":true,"insecureCookies":true}`), 0o400); err != nil {
		t.Fatal(err)
	}
	settings, _, err := parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Username != "file-admin" || !settings.SignupEnabled || !settings.InsecureCookies {
		t.Fatalf("file account: %+v %v", settings, err)
	}
	t.Setenv("DIFFX_SERVER_USERNAME", "env-admin")
	t.Setenv("DIFFX_SERVER_SIGNUP_ENABLED", "false")
	t.Setenv("DIFFX_SERVER_INSECURE_COOKIES", "false")
	settings, _, err = parseSettings([]string{"--config-file", path}, io.Discard)
	if err != nil || settings.Username != "env-admin" || settings.SignupEnabled || settings.InsecureCookies {
		t.Fatalf("environment account: %+v %v", settings, err)
	}
	settings, _, err = parseSettings([]string{"--config-file", path, "--username", "flag-admin", "--signup-enabled", "--insecure-cookies"}, io.Discard)
	if err != nil || settings.Username != "flag-admin" || !settings.SignupEnabled || !settings.InsecureCookies {
		t.Fatalf("flag account: %+v %v", settings, err)
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
		{"DIFFX_SERVER_SIGNUP_ENABLED", "bad"},
		{"DIFFX_SERVER_PASSWORD", "partial-password"},
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
