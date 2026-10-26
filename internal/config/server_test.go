package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerConfigIgnoresPersonalSettingsAndDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	runtime := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DIFFX_RUNTIME_DIR", runtime)
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	path := filepath.Join(home, ".config", "diffx", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"state":"personal.db","port":9000}`), 0o400); err != nil {
		t.Fatal(err)
	}
	// CLI environment must not affect or invalidate server startup.
	t.Setenv("DIFFX_CONFIG_PATH", path)
	t.Setenv("DIFFX_REMOTE_URL", "not-a-url")
	t.Setenv("DIFFX_LOCAL_PORT", "9000")
	t.Setenv("DIFFX_LOCAL_WEB_DIR", "/missing/assets")
	values, err := LoadServerFile("")
	if err != nil || values.State != "data/state.db" || values.Port != DefaultServerPort {
		t.Fatalf("personal config affected server: %+v %v", values, err)
	}
	for _, directory := range []string{runtime, filepath.Dir(path)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if directory == runtime || entry.Name() != "config.json" {
				t.Fatalf("server created config artifact %s", filepath.Join(directory, entry.Name()))
			}
		}
	}
}

func TestServerConfigReadsOnlyExplicitFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	t.Setenv("DIFFX_SERVER_CONFIG", path)
	if _, err := LoadServerFile(""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing config did not fail: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created missing config: %v", err)
	}
	raw := []byte(`{"host":"::1","port":4123,"state":"configured.db","retentionDays":14}`)
	if err := os.WriteFile(path, raw, 0o400); err != nil {
		t.Fatal(err)
	}
	values, err := LoadServerFile("")
	if err != nil || values.Host != "::1" || values.Port != 4123 || values.State != "configured.db" || values.RetentionDays != 14 {
		t.Fatalf("explicit config: %+v %v", values, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("read-only config created artifacts: %v %v", entries, err)
	}
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(`{"port":5000}`), 0o400); err != nil {
		t.Fatal(err)
	}
	values, err = LoadServerFile(other)
	if err != nil || values.Port != 5000 {
		t.Fatalf("explicit path did not override env path: %+v %v", values, err)
	}
}

func TestServerConfigRejectsMalformedFiles(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"port":"7981"}`, `{"unknown":true}`, `{"password":"secret"}`, `{"token":"secret"}`, `{"account":"admin"}`, `{"server":"https://example.com"}`, `{} {}`, `{} trailing`} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server.json")
			if err := os.WriteFile(path, []byte(raw), 0o400); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadServerFile(path); err == nil {
				t.Fatal("accepted invalid server config")
			}
		})
	}
}

func TestGitHubConfigIsAllOrNothingWithSecretsFromEnvironment(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	complete := map[string]string{
		"DIFFX_SERVER_GITHUB_APP_ID": "12", "DIFFX_SERVER_GITHUB_CLIENT_ID": "client", "DIFFX_SERVER_GITHUB_PRIVATE_KEY_FILE": "/keys/app.pem",
		"DIFFX_SERVER_GITHUB_PUBLIC_URL": "https://diffx.example.com", "DIFFX_SERVER_GITHUB_CLIENT_SECRET": "secret",
		"DIFFX_SERVER_GITHUB_WEBHOOK_SECRET": "hook", "DIFFX_SERVER_GITHUB_TOKEN_KEY": key,
		"DIFFX_SERVER_FETCHER_SECRET": strings.Repeat("f", 32),
	}
	for name, value := range complete {
		t.Setenv(name, value)
	}
	values, err := LoadServerFile("")
	if err != nil || values.GitHub.AppID != 12 || values.GitHub.TokenKey != key || values.GitHub.Validate(false) != nil {
		t.Fatalf("complete integration: %+v %v %v", values.GitHub, err, values.GitHub.Validate(false))
	}
	for _, tc := range []struct{ name, value, want string }{
		{"DIFFX_SERVER_GITHUB_WEBHOOK_SECRET", "", "DIFFX_SERVER_GITHUB_WEBHOOK_SECRET"},
		{"DIFFX_SERVER_GITHUB_PUBLIC_URL", "http://diffx.example.com", "https origin"},
		{"DIFFX_SERVER_GITHUB_PUBLIC_URL", "https://diffx.example.com/app", "https origin"},
		{"DIFFX_SERVER_GITHUB_TOKEN_KEY", "short", "32 random bytes"},
		{"DIFFX_SERVER_FETCHER_SECRET", "", "DIFFX_SERVER_FETCHER_SECRET"},
		{"DIFFX_SERVER_FETCHER_SECRET", "short", "at least 32 characters"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			values, err := LoadServerFile("")
			if err != nil {
				t.Fatal(err)
			}
			if err := values.GitHub.Validate(false); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation %v, want %q", err, tc.want)
			}
		})
	}
	t.Setenv("DIFFX_SERVER_GITHUB_PUBLIC_URL", "http://127.0.0.1:7980")
	values, _ = LoadServerFile("")
	if err := values.GitHub.Validate(true); err != nil {
		t.Fatalf("development http public url with insecure cookies: %v", err)
	}
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"github":{"appId":1,"clientSecret":"in-file"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServerFile(path); err == nil || !strings.Contains(err.Error(), "clientSecret") {
		t.Fatalf("secret accepted from config file: %v", err)
	}
	if (GitHub{}).Validate(false) != nil || (GitHub{}).Enabled() {
		t.Fatal("absent integration must be valid and disabled")
	}
}

func TestGoogleSignInNeedsBothCredentialsAndPublicURL(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	t.Setenv("DIFFX_SERVER_GOOGLE_CLIENT_ID", "client")
	t.Setenv("DIFFX_SERVER_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("DIFFX_SERVER_PUBLIC_URL", "https://diffx.example.com")
	values, err := LoadServerFile("")
	if err != nil || !values.Google.Enabled() || values.ValidateIdentity() != nil {
		t.Fatalf("complete Google sign-in: %+v %v", values.Google, err)
	}
	for _, tc := range []struct{ name, want string }{
		{"DIFFX_SERVER_GOOGLE_CLIENT_SECRET", "DIFFX_SERVER_GOOGLE_CLIENT_SECRET"},
		{"DIFFX_SERVER_GOOGLE_CLIENT_ID", "DIFFX_SERVER_GOOGLE_CLIENT_ID"},
		{"DIFFX_SERVER_PUBLIC_URL", "DIFFX_SERVER_PUBLIC_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.name, "")
			values, err := LoadServerFile("")
			if err != nil {
				t.Fatal(err)
			}
			if err := values.ValidateIdentity(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation %v, want %q", err, tc.want)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"google":{"clientId":"client","clientSecret":"in-file"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServerFile(path); err == nil || !strings.Contains(err.Error(), "clientSecret") {
		t.Fatalf("secret accepted from config file: %v", err)
	}
	if (Google{}).Enabled() || (ServerValues{}).ValidateIdentity() != nil {
		t.Fatal("absent Google sign-in must be valid and disabled")
	}
}

func TestInternalListenerSharesHostOnSeparatePort(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	values, err := LoadServerFile("")
	if err != nil || values.InternalPort != DefaultInternalPort {
		t.Fatalf("default internal port: %+v %v", values, err)
	}
	if address, err := values.InternalAddress("0.0.0.0:7980"); err != nil || address != "0.0.0.0:7979" {
		t.Fatalf("internal address %q %v", address, err)
	}
	t.Setenv("DIFFX_SERVER_INTERNAL_PORT", "7980")
	values, err = LoadServerFile("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := values.InternalAddress("0.0.0.0:7980"); err == nil {
		t.Fatal("internal listener accepted the public port")
	}
}

func TestEmptyGitHubEnvironmentDisablesIntegration(t *testing.T) {
	t.Setenv("DIFFX_SERVER_CONFIG", "")
	for _, name := range []string{"APP_ID", "CLIENT_ID", "PRIVATE_KEY_FILE", "PUBLIC_URL", "CLIENT_SECRET", "WEBHOOK_SECRET", "TOKEN_KEY"} {
		t.Setenv("DIFFX_SERVER_GITHUB_"+name, "")
	}
	t.Setenv("DIFFX_SERVER_FETCHER_SECRET", "")
	values, err := LoadServerFile("")
	if err != nil || values.GitHub.Enabled() || values.GitHub.Validate(false) != nil {
		t.Fatalf("empty passthrough must disable GitHub: %+v %v", values.GitHub, err)
	}
}
