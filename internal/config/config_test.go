package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFirstUseOverridesAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("DIFFX_CONFIG_PATH", path)
	defaults, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	host := "0.0.0.0"
	if err := Edit("local.host", &host); err != nil {
		t.Fatal(err)
	}
	saved, err := Load()
	if err != nil || saved.Local.Host != host {
		t.Fatalf("saved config: %#v, %v", saved, err)
	}
	override, err := Merge(saved, `{"local":{"host":"127.0.0.1","port":4000,"state":"memory","webDir":"/assets"}}`)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := override.Settings()
	if err != nil || settings.Host != "127.0.0.1" || settings.Port != 4000 || settings.State != "memory" || settings.WebDir != "/assets" {
		t.Fatalf("override: %#v, %v", settings, err)
	}
	unchanged, err := Load()
	if err != nil || unchanged.Local.Host != host || unchanged.Local.Port != nil {
		t.Fatalf("override persisted: %#v, %v", unchanged, err)
	}
	if err := Edit("local.host", nil); err != nil {
		t.Fatal(err)
	}
	restored, err := Load()
	if err != nil || restored.Local.Host != defaults.Local.Host {
		t.Fatalf("remove: %#v, %v", restored, err)
	}
}

func TestRuntimeEnvironmentOverridesFileWithoutPersisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "config.json")
	t.Setenv("DIFFX_CONFIG_PATH", filepath.Join(t.TempDir(), "unused.json"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"local":{"host":"127.0.0.2","port":4123,"state":"file.db","webDir":"file-assets"}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_LOCAL_HOST", "::1")
	t.Setenv("DIFFX_LOCAL_PORT", "5123")
	t.Setenv("DIFFX_LOCAL_STATE", "memory")
	t.Setenv("DIFFX_LOCAL_WEB_DIR", "")
	values, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	local := values.Local
	if local.Host != "::1" || local.Port == nil || *local.Port != 5123 || local.State != "memory" || local.WebDir != "" {
		t.Fatalf("environment precedence: %#v", values)
	}
	if err := EditFile(path, "local.host", nil); err != nil {
		t.Fatal(err)
	}
	saved, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	local = saved.Local
	if local.Host != "127.0.0.1" || local.Port == nil || *local.Port != 4123 || local.State != "file.db" || local.WebDir != "file-assets" {
		t.Fatalf("environment persisted during edit: %#v", saved)
	}
	if _, err := os.Stat(os.Getenv("DIFFX_CONFIG_PATH")); !os.IsNotExist(err) {
		t.Fatalf("explicit file touched default: %v", err)
	}
}

// diffx-server deployment variables share a machine with the local CLI and
// must never select its listener or database.
func TestServerEnvironmentNeverConfiguresLocalMode(t *testing.T) {
	for _, name := range []string{"DIFFX_SERVER_HOST", "DIFFX_SERVER_PORT", "DIFFX_SERVER_STATE", "DIFFX_SERVER_TOKEN", "DIFFX_SERVER_RETENTION_DAYS", "DIFFX_HOST", "DIFFX_PORT", "DIFFX_STATE", "DIFFX_TOKEN", "DIFFX_SERVER_URL"} {
		t.Setenv(name, "1")
	}
	values, err := LoadFile(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if values.Local.Host != "127.0.0.1" || values.Local.Port != nil || values.Local.RetentionDays != 7 || values.Remote != (RemoteValues{}) {
		t.Fatalf("server or retired environment configured CLI: %#v", values)
	}
}

func TestMissingRuntimeFileCreatesPrivateDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "nested", "config.json")
	values, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values.Local.Host != "127.0.0.1" || values.Local.Port != nil || values.Local.State == "" || values.Local.WebDir != "" || values.RemoteConfigured() {
		t.Fatalf("defaults: %#v", values)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{path, filepath.Dir(path)} {
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("non-private config path %s: %v", name, err)
			}
		}
	}
}

func TestInvalidEnvironmentAndFileFail(t *testing.T) {
	for _, field := range []struct{ name, value string }{
		{"DIFFX_LOCAL_HOST", "localhost"},
		{"DIFFX_LOCAL_PORT", ""},
		{"DIFFX_LOCAL_PORT", "65536"},
		{"DIFFX_LOCAL_PORT", "-1"},
		{"DIFFX_LOCAL_STATE", ""},
		{"DIFFX_REMOTE_URL", "ftp://example.com"},
		{"DIFFX_REMOTE_URL", "https://example.com/api"},
		{"DIFFX_LOCAL_RETENTION_DAYS", ""},
		{"DIFFX_LOCAL_RETENTION_DAYS", "0"},
		{"DIFFX_LOCAL_RETENTION_DAYS", "-1"},
		{"DIFFX_LOCAL_RETENTION_DAYS", "106752"},
	} {
		t.Run(field.name+"/"+field.value, func(t *testing.T) {
			t.Setenv(field.name, field.value)
			if _, err := LoadFile(filepath.Join(t.TempDir(), "config.json")); err == nil {
				t.Fatal("accepted invalid environment")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"unknown":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("accepted invalid file")
	}
}

func TestDestinationAndRetentionPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for key, value := range map[string]string{"remote.url": "https://reviews.example.com", "remote.token": "saved-token", "local.retentionDays": "14"} {
		if err := EditFile(path, key, &value); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DIFFX_REMOTE_URL", "http://127.0.0.1:9000")
	t.Setenv("DIFFX_REMOTE_TOKEN", "env-token")
	t.Setenv("DIFFX_LOCAL_RETENTION_DAYS", "3")
	values, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if values.Remote.URL != "http://127.0.0.1:9000" || values.Remote.Token != "env-token" || values.Local.RetentionDays != 3 || !values.RemoteConfigured() {
		t.Fatalf("environment precedence: %#v", values)
	}
	settings, err := values.Settings()
	if err != nil || settings.RetentionDays != 3 {
		t.Fatalf("server retention: %#v %v", settings, err)
	}
	saved, err := ReadFile(path)
	if err != nil || saved.Remote.URL != "https://reviews.example.com" || saved.Remote.Token != "saved-token" || saved.Local.RetentionDays != 14 {
		t.Fatalf("environment persisted: %#v %v", saved, err)
	}
	for _, key := range []string{"remote.url", "remote.token", "local.retentionDays"} {
		if err := EditFile(path, key, nil); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := ReadFile(path)
	if err != nil || restored.Remote != (RemoteValues{}) || restored.Local.RetentionDays != 7 {
		t.Fatalf("remove restores defaults: %#v %v", restored, err)
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("token config is not private: %v", err)
	}
}

func TestRetiredFlatKeysNameTheirReplacement(t *testing.T) {
	defaults, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for raw, replacement := range map[string]string{`{"server":"https://example.com"}`: "remote.url", `{"token":"x"}`: "remote.token", `{"port":4000}`: "local.port"} {
		if _, err := Merge(defaults, raw); err == nil || !strings.Contains(err.Error(), replacement) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	value := "x"
	if err := EditFile(filepath.Join(t.TempDir(), "config.json"), "token", &value); err == nil || !strings.Contains(err.Error(), "remote.token") {
		t.Fatalf("flat key edit: %v", err)
	}
}

func TestInvalidCollectorSettings(t *testing.T) {
	defaults, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	partial, err := Merge(defaults, `{"local":{"host":"127.0.0.1","state":"memory"}}`)
	if err != nil || partial.Local.RetentionDays != 7 {
		t.Fatalf("partial retention: %#v %v", partial, err)
	}
	for _, raw := range []string{
		`{"remote":{"url":null}}`, `{"remote":{"token":null}}`, `{"local":{"retentionDays":null}}`, `{"remote":null}`,
		`{"remote":{"url":"https://user:pass@example.com"}}`, `{"remote":{"url":"https://example.com?query"}}`,
		`{"remote":{"url":"https://example.com#fragment"}}`, `{"local":{"retentionDays":0}}`, `{"local":{"retentionDays":1.5}}`,
		`{"remote":{"server":"https://example.com"}}`,
	} {
		if _, err := Merge(defaults, raw); err == nil {
			t.Fatalf("accepted invalid settings: %s", raw)
		} else if strings.Contains(err.Error(), "user:pass") {
			t.Fatalf("error exposes credentials: %v", err)
		}
	}
}

func TestInvalidConfigNeverChangesSavedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("DIFFX_CONFIG_PATH", path)
	values, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `[]`, `{"unknown":1}`, `{"local":{"host":null}}`, `{"local":{"state":""}}`, `{"local":{"host":"example.com"}}`, `{"local":{"port":-1}}`, `{"local":{"port":65536}}`, `{"local":{"port":"4000"}}`, `{} {}`} {
		if _, err := Merge(values, raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := "-1"
	if err := Edit("local.port", &invalid); err == nil {
		t.Fatal("saved invalid port")
	}
	after, _ := os.ReadFile(path)
	if string(raw) != string(after) {
		t.Fatal("invalid edit altered file")
	}
}

func TestMalformedConfigFileErrorsNameFileAndFix(t *testing.T) {
	for raw, want := range map[string][]string{
		`{"host":"127.0.0.1","port":null,"server":"","token":""}`: {"retired flat format", "host → local.host", "port → local.port", "server → remote.url", "token → remote.token"},
		`{"local":{"server":"https://example.com"}}`:              {`unknown config key "local.server"`, `move its value to "remote.url"`, `{"remote": {"url": "https://reviews.example.com"}}`},
		`{"remote":{"server":"https://example.com"}}`:             {`move its value to "remote.url"`},
		`{"local":{"token":"secret-token"}}`:                      {`move its value to "remote.token"`, `{"remote": {"token": "<existing token>"}}`},
		`{"local":{"url":"https://example.com"}}`:                 {`move its value to "remote.url"`},
		`{"remote":{"port":4000}}`:                                {`move its value to "local.port"`},
		`{"local":{"prot":4000}}`:                                 {`unknown config key "local.prot"`, "local.port"},
		`{"local":{"port":"4000"}}`:                               {"local.port must be a whole number or null", `{"local": {"port": null}}`},
		`{"local":{"port":65536}}`:                                {"use 0 for an ephemeral port"},
		`{"local":{"host":null}}`:                                 {"local.host cannot be null", "omit it to use the default", `{"local": {"host": "127.0.0.1"}}`},
		`{"local":{"host":"localhost"}}`:                          {"local.host must be an IP address", `use "127.0.0.1"`},
		`{"local":{"state":""}}`:                                  {"omit it to use the default persistent database"},
		`{"local":{"retentionDays":0}}`:                           {"use 7 for one week"},
		`{"remote":{"url":"https://user:password@example.com"}}`:  {`use "https://reviews.example.com"`, "remote.token"},
		`{"remote":"https://example.com"}`:                        {"remote must be a JSON object"},
		`{"servers":{}}`:                                          {`unknown config section "servers"`},
		`{"local":`:                                               {"invalid config JSON"},
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFile(path)
		if err == nil {
			t.Fatalf("accepted %s", raw)
		}
		for _, text := range append(want, path, "Edit this file, then retry") {
			if !strings.Contains(err.Error(), text) {
				t.Fatalf("%s: error %q lacks %q", raw, err, text)
			}
		}
		for _, secret := range []string{"secret-token", "user:password"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error exposes credentials: %v", err)
			}
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != raw {
			t.Fatalf("rejected config changed: %q, %v", after, err)
		}
	}
}

func TestFlatConfigErrorProvidesValidNestedExample(t *testing.T) {
	values, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Merge(values, `{"host":"127.0.0.1","server":"https://user:password@example.com","token":"secret-token"}`)
	if err == nil {
		t.Fatal("accepted flat config")
	}
	message := err.Error()
	start := strings.Index(message, "{")
	end := strings.LastIndex(message, "}")
	if start < 0 || end < start {
		t.Fatalf("missing JSON example: %s", message)
	}
	example := message[start : end+1]
	var document Values
	if err := json.Unmarshal([]byte(example), &document); err != nil {
		t.Fatalf("invalid JSON example: %v", err)
	}
	if _, err := Merge(values, example); err != nil {
		t.Fatalf("example rejected by config loader: %v", err)
	}
	if document.Local.Host != "127.0.0.1" || document.Remote.URL == "" || document.Remote.Token == "" {
		t.Fatalf("example missing local/remote structure: %+v", document)
	}
	if strings.Contains(message, "secret-token") || strings.Contains(message, "user:password") {
		t.Fatalf("error exposes credentials: %s", message)
	}
}
