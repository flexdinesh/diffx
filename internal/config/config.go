// Package config owns persisted settings, separate from private runtime credentials.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

type ServerSettings struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	State         string `json:"state"`
	WebDir        string `json:"webDir"`
	RetentionDays int    `json:"retentionDays"`
}

// LocalValues configure the foreground `diffx [PATH]` server only.
type LocalValues struct {
	Host          string `json:"host"`
	Port          *int   `json:"port"`
	State         string `json:"state"`
	WebDir        string `json:"webDir"`
	RetentionDays int    `json:"retentionDays"`
}

// RemoteValues select the destination for sync and agent hooks only.
type RemoteValues struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Values keep local serving and remote delivery in separate namespaces so one
// file configures both modes without either reading the other's settings.
type Values struct {
	Local  LocalValues  `json:"local"`
	Remote RemoteValues `json:"remote"`
}

// Keys lists persisted settings by dotted path.
var Keys = []string{"local.host", "local.port", "local.state", "local.webDir", "local.retentionDays", "remote.url", "remote.token"}

// legacyKeys maps retired flat keys to their namespaced replacement.
var legacyKeys = map[string]string{"host": "local.host", "port": "local.port", "state": "local.state", "webDir": "local.webDir", "retentionDays": "local.retentionDays", "server": "remote.url", "token": "remote.token"}

func (values *Values) field(key string) (interface{}, error) {
	switch key {
	case "local.host":
		return &values.Local.Host, nil
	case "local.port":
		return &values.Local.Port, nil
	case "local.state":
		return &values.Local.State, nil
	case "local.webDir":
		return &values.Local.WebDir, nil
	case "local.retentionDays":
		return &values.Local.RetentionDays, nil
	case "remote.url":
		return &values.Remote.URL, nil
	case "remote.token":
		return &values.Remote.Token, nil
	}
	if replacement, ok := legacyKeys[key]; ok {
		return nil, fmt.Errorf("config key %q moved to %q; use %s with your existing value", key, replacement, keyExample(replacement))
	}
	if section, name, ok := strings.Cut(key, "."); ok && (section == "local" || section == "remote") {
		replacement := legacyKeys[name]
		if name == "url" {
			replacement = "remote.url"
		}
		if replacement != "" {
			return nil, fmt.Errorf("unknown config key %q; move its value to %q; use %s with your existing value", key, replacement, keyExample(replacement))
		}
	}
	return nil, fmt.Errorf("unknown config key %q; remove or rename it; valid keys: %s", key, strings.Join(Keys, ", "))
}

// Examples never interpolate config values: errors may be logged, including
// when a token or credentials in a URL caused validation to fail.
func keyExample(key string) string {
	section, name, _ := strings.Cut(key, ".")
	value := `""`
	switch key {
	case "local.host":
		value = `"127.0.0.1"`
	case "local.port":
		value = "null"
	case "local.state":
		value = `"/path/to/state.db"`
	case "local.retentionDays":
		value = "7"
	case "remote.url":
		value = `"https://reviews.example.com"`
	case "remote.token":
		value = `"<existing token>"`
	}
	return fmt.Sprintf(`{%q: {%q: %s}}`, section, name, value)
}

// Get returns one persisted setting by dotted path.
func (values Values) Get(key string) (interface{}, error) {
	field, err := values.field(key)
	if err != nil {
		return nil, err
	}
	switch target := field.(type) {
	case *string:
		return *target, nil
	case *int:
		return *target, nil
	case **int:
		return *target, nil
	}
	return nil, fmt.Errorf("unsupported config key %q", key)
}

func Default() (Values, error) {
	state, err := reviewstore.DefaultPath()
	return Values{Local: LocalValues{Host: "127.0.0.1", State: state, RetentionDays: 7}}, err
}

func Path() (string, error) {
	if path := os.Getenv("DIFFX_CONFIG_PATH"); path != "" {
		return filepath.Abs(path)
	}
	if runtime := os.Getenv("DIFFX_RUNTIME_DIR"); runtime != "" {
		return filepath.Abs(filepath.Join(runtime, "config.json"))
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".config", "diffx", "config.json"), err
}

// Load creates defaults once. The lock also serializes first-use and config edits.
func Load() (Values, error) {
	return ReadFile("")
}

// ReadFile reads persisted values only. An empty path selects the machine config.
func ReadFile(path string) (Values, error) {
	path, err := resolvePath(path)
	if err != nil {
		return Values{}, err
	}
	lock, err := processlock.Acquire(path + ".lock")
	if err != nil {
		return Values{}, err
	}
	defer lock.Close()
	return load(path)
}

func resolvePath(path string) (string, error) {
	if path == "" {
		return Path()
	}
	return filepath.Abs(path)
}

// LoadFile resolves defaults, a JSON file, then environment overrides. It never
// persists environment values. An empty path selects the machine config.
// Environment names are namespaced like the file so a diffx-server deployment's
// DIFFX_SERVER_* variables never configure the local CLI.
func LoadFile(path string) (Values, error) {
	values, err := ReadFile(path)
	if err != nil {
		return values, err
	}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"DIFFX_LOCAL_HOST", &values.Local.Host},
		{"DIFFX_LOCAL_STATE", &values.Local.State},
		{"DIFFX_LOCAL_WEB_DIR", &values.Local.WebDir},
		{"DIFFX_REMOTE_URL", &values.Remote.URL},
		{"DIFFX_REMOTE_TOKEN", &values.Remote.Token},
	} {
		if value, exists := os.LookupEnv(field.name); exists {
			*field.target = value
		}
	}
	if raw, exists := os.LookupEnv("DIFFX_LOCAL_PORT"); exists {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return values, errors.New("invalid DIFFX_LOCAL_PORT: set a whole number between 0 and 65535, or unset it for automatic port selection")
		}
		values.Local.Port = &port
	}
	if raw, exists := os.LookupEnv("DIFFX_LOCAL_RETENTION_DAYS"); exists {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return values, errors.New("invalid DIFFX_LOCAL_RETENTION_DAYS: set a whole number between 1 and 106751; use 7 for one week")
		}
		values.Local.RetentionDays = days
	}
	if err := values.Validate(); err != nil {
		return values, fmt.Errorf("invalid environment config: %w", err)
	}
	return values, nil
}

func load(path string) (Values, error) {
	values, err := Default()
	if err != nil {
		return values, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, save(path, values)
	}
	if err != nil {
		return values, err
	}
	values, err = Merge(values, string(raw))
	if err != nil {
		return values, fmt.Errorf("config file %s: %w\n  Edit this file, then retry. Omitted keys use defaults.", path, err)
	}
	return values, nil
}

func decodeObject(raw []byte, name string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("invalid %s JSON: %w; use a JSON object with double-quoted keys and no trailing commas", name, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s must be a JSON object; use {} for defaults", name)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("%s must contain one JSON object; combine settings inside a single pair of braces", name)
	}
	return fields, nil
}

func Merge(values Values, raw string) (Values, error) {
	sections, err := decodeObject([]byte(raw), "config")
	if err != nil {
		return values, err
	}
	var moves []string
	for key := range sections {
		if replacement, ok := legacyKeys[key]; ok {
			moves = append(moves, key+" → "+replacement)
		}
	}
	if len(moves) > 0 {
		sort.Strings(moves)
		return values, fmt.Errorf(`uses the retired flat format; move each key into its "local" or "remote" section (%s).
  Use nested JSON, keeping your existing values (omit settings you do not need):
  {
    "local": {"host": "127.0.0.1", "port": null, "state": "/path/to/state.db", "webDir": "", "retentionDays": 7},
    "remote": {"url": "https://reviews.example.com", "token": "<existing token>"}
  }
  "server" becomes "url" inside "remote"; "token" also belongs inside "remote".`, strings.Join(moves, ", "))
	}
	for section, body := range sections {
		if section != "local" && section != "remote" {
			return values, fmt.Errorf(`unknown config section %q; remove or rename it; expected "local" and "remote", like {"local": {"port": null}, "remote": {"url": "https://reviews.example.com"}}`, section)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
			return values, fmt.Errorf("%s must be a JSON object; use {%q: {}} for defaults, then put settings inside that object", section, section)
		}
		for name, field := range fields {
			key := section + "." + name
			target, err := values.field(key)
			if err != nil {
				return values, err
			}
			if string(field) == "null" && key != "local.port" {
				return values, fmt.Errorf("%s cannot be null; omit it to use the default, or use %s", key, keyExample(key))
			}
			if err := json.Unmarshal(field, target); err != nil {
				return values, fmt.Errorf("%s must be %s; use %s", key, expectedType(target), keyExample(key))
			}
		}
	}
	if err := values.Validate(); err != nil {
		return values, err
	}
	return values, nil
}

func expectedType(target interface{}) string {
	switch target.(type) {
	case *string:
		return "a string"
	case **int:
		return "a whole number or null"
	default:
		return "a whole number"
	}
}

func (values Values) Validate() error {
	local := values.Local
	if net.ParseIP(local.Host) == nil {
		return errors.New(`local.host must be an IP address; use "127.0.0.1" for this machine or "0.0.0.0" to listen on all IPv4 interfaces`)
	}
	if local.Port != nil && (*local.Port < 0 || *local.Port > 65535) {
		return errors.New("local.port must be null (automatic) or between 0 and 65535; use 0 for an ephemeral port, or omit the setting for automatic selection")
	}
	if local.State == "" {
		return errors.New(`local.state must be a database path or memory; omit it to use the default persistent database, or use "memory" for temporary state`)
	}
	if local.RetentionDays <= 0 || local.RetentionDays > 106751 {
		return errors.New("local.retentionDays must be between 1 and 106751; use 7 for one week")
	}
	if values.Remote.URL != "" {
		endpoint, err := url.Parse(values.Remote.URL)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
			return errors.New(`remote.url must be an HTTP(S) origin without credentials, path, query, or fragment; use "https://reviews.example.com" and put the API token in remote.token`)
		}
	}
	return nil
}

// RemoteConfigured reports whether sync and hooks have a complete destination.
func (values Values) RemoteConfigured() bool {
	return values.Remote.URL != "" && values.Remote.Token != ""
}

func (values Values) Settings() (ServerSettings, error) {
	if err := values.Validate(); err != nil {
		return ServerSettings{}, err
	}
	local := values.Local
	settings := ServerSettings{Host: net.ParseIP(local.Host).String(), Port: -1, State: local.State, WebDir: local.WebDir, RetentionDays: local.RetentionDays}
	if local.Port != nil {
		settings.Port = *local.Port
	}
	var err error
	if settings.State != "memory" {
		settings.State, err = filepath.Abs(settings.State)
		if err != nil {
			return settings, err
		}
	}
	if settings.WebDir != "" {
		settings.WebDir, err = filepath.Abs(settings.WebDir)
	}
	return settings, err
}

func Edit(key string, value *string) error {
	return EditFile("", key, value)
}

// EditFile edits only persisted values, independent of environment overrides.
// A nil value restores the default.
func EditFile(path, key string, value *string) error {
	path, err := resolvePath(path)
	if err != nil {
		return err
	}
	lock, err := processlock.Acquire(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	values, err := load(path)
	if err != nil {
		return err
	}
	defaults, err := Default()
	if err != nil {
		return err
	}
	target, err := values.field(key)
	if err != nil {
		return err
	}
	var raw []byte
	if value == nil {
		fallback, err := defaults.Get(key)
		if err != nil {
			return err
		}
		if raw, err = json.Marshal(fallback); err != nil {
			return err
		}
	} else if _, text := target.(*string); text {
		if raw, err = json.Marshal(*value); err != nil {
			return err
		}
	} else {
		raw = []byte(*value)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid %s: %w", key, err)
	}
	if err := values.Validate(); err != nil {
		return err
	}
	return save(path, values)
}

func save(path string, values Values) error {
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
