package config

import (
	"strings"
	"testing"
)

func TestConsoleConfigComesFromItsOwnEnvironment(t *testing.T) {
	// Server settings never configure the console.
	t.Setenv("DIFFX_SERVER_DATABASE_URL", "postgres://server")
	t.Setenv("DIFFX_SERVER_PORT", "7000")
	values, err := LoadConsole()
	if err != nil || values.DatabaseURL != "" || values.Host != "127.0.0.1" || values.Port != DefaultConsolePort {
		t.Fatalf("defaults: %+v %v", values, err)
	}
	if err := values.Validate(); err == nil || !strings.Contains(err.Error(), "DIFFX_CONSOLE_DATABASE_URL") {
		t.Fatalf("missing database: %v", err)
	}
	t.Setenv("DIFFX_CONSOLE_DATABASE_URL", "postgres://console")
	t.Setenv("DIFFX_CONSOLE_PORT", "7001")
	t.Setenv("DIFFX_CONSOLE_INSECURE_COOKIES", "true")
	values, err = LoadConsole()
	if err != nil || values.DatabaseURL != "postgres://console" || values.Port != 7001 || !values.InsecureCookies {
		t.Fatalf("environment: %+v %v", values, err)
	}
	if err := values.Validate(); err == nil || !strings.Contains(err.Error(), "DIFFX_CONSOLE_APP_URL") {
		t.Fatalf("missing app origin: %v", err)
	}
	t.Setenv("DIFFX_CONSOLE_PORT", "console")
	if _, err := LoadConsole(); err == nil || !strings.Contains(err.Error(), "DIFFX_CONSOLE_PORT") {
		t.Fatalf("invalid port: %v", err)
	}
}

func TestConsoleConfigValidatesOriginsGoogleAndRateLimit(t *testing.T) {
	valid := ConsoleValues{Host: "127.0.0.1", Port: DefaultConsolePort, DatabaseURL: "postgres://console", AppURL: "https://diffx.example.com"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		change func(*ConsoleValues)
		want   string
	}{
		"http app":         {func(values *ConsoleValues) { values.AppURL = "http://diffx.example.com" }, "DIFFX_CONSOLE_APP_URL"},
		"app path":         {func(values *ConsoleValues) { values.AppURL = "https://diffx.example.com/app" }, "DIFFX_CONSOLE_APP_URL"},
		"http console":     {func(values *ConsoleValues) { values.PublicURL = "http://console.example.com" }, "DIFFX_CONSOLE_PUBLIC_URL"},
		"google secret":    {func(values *ConsoleValues) { values.Google.ClientID = "client" }, "DIFFX_CONSOLE_GOOGLE_CLIENT_SECRET"},
		"google origin":    {func(values *ConsoleValues) { values.Google = Google{ClientID: "client", ClientSecret: "secret"} }, "DIFFX_CONSOLE_PUBLIC_URL"},
		"rate limit":       {func(values *ConsoleValues) { values.AuthRateLimit = true }, "DIFFX_CONSOLE_CLIENT_IP_HEADER"},
		"rate limit value": {func(values *ConsoleValues) { values.AuthRateLimit, values.ClientIPHeader = true, "X Real IP" }, "DIFFX_CONSOLE_CLIENT_IP_HEADER"},
	} {
		values := valid
		test.change(&values)
		if err := values.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	insecure := valid
	insecure.InsecureCookies, insecure.AppURL, insecure.PublicURL = true, "http://localhost:7980", "http://localhost:7970"
	if err := insecure.Validate(); err != nil {
		t.Fatalf("development origins: %v", err)
	}
	if _, err := (ConsoleValues{Host: "localhost", Port: 1}).ListenAddress(); err == nil || !strings.Contains(err.Error(), "console host") {
		t.Fatalf("host: %v", err)
	}
}
