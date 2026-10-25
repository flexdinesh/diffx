package config

import (
	"os"
	"strings"
	"testing"
)

// setFetcherEnvironment leaves every fetcher variable absent except values.
func setFetcherEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	for _, name := range []string{"DIFFX_FETCHER_SERVER_URL", "DIFFX_FETCHER_SECRET", "DIFFX_FETCHER_MIRRORS", "DIFFX_FETCHER_DISK_LIMIT_GB", "DIFFX_FETCHER_CONCURRENCY"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func TestFetcherConfigDefaultsAndOverrides(t *testing.T) {
	setFetcherEnvironment(t, map[string]string{"DIFFX_FETCHER_SERVER_URL": "http://server:7979", "DIFFX_FETCHER_SECRET": "shh"})
	values, err := LoadFetcher()
	want := FetcherValues{ServerURL: "http://server:7979", Secret: "shh", Mirrors: "data/mirrors", DiskLimitBytes: 20 << 30, Concurrency: 2}
	if err != nil || values != want {
		t.Fatalf("defaults: %+v %v", values, err)
	}
	setFetcherEnvironment(t, map[string]string{"DIFFX_FETCHER_SERVER_URL": "http://server:7979", "DIFFX_FETCHER_SECRET": "shh",
		"DIFFX_FETCHER_MIRRORS": "/mirrors", "DIFFX_FETCHER_DISK_LIMIT_GB": "3", "DIFFX_FETCHER_CONCURRENCY": "8"})
	values, err = LoadFetcher()
	want = FetcherValues{ServerURL: "http://server:7979", Secret: "shh", Mirrors: "/mirrors", DiskLimitBytes: 3 << 30, Concurrency: 8}
	if err != nil || values != want {
		t.Fatalf("overrides: %+v %v", values, err)
	}
}

func TestFetcherConfigRejectsInvalidValuesWithoutEchoingSecret(t *testing.T) {
	const secret = "fetcher-secret-value"
	for _, tc := range []struct{ name, value string }{
		{"DIFFX_FETCHER_SERVER_URL", ""},
		{"DIFFX_FETCHER_SECRET", ""},
		{"DIFFX_FETCHER_MIRRORS", ""},
		{"DIFFX_FETCHER_DISK_LIMIT_GB", "0"},
		{"DIFFX_FETCHER_DISK_LIMIT_GB", "1.5"},
		{"DIFFX_FETCHER_DISK_LIMIT_GB", "9223372036854775807"},
		{"DIFFX_FETCHER_CONCURRENCY", "0"},
		{"DIFFX_FETCHER_CONCURRENCY", "9"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			setFetcherEnvironment(t, map[string]string{"DIFFX_FETCHER_SERVER_URL": "http://server:7979", "DIFFX_FETCHER_SECRET": secret, tc.name: tc.value})
			_, err := LoadFetcher()
			if err == nil || !strings.Contains(err.Error(), tc.name) || strings.Contains(err.Error(), secret) {
				t.Fatalf("error %v, want mention of %s without secret", err, tc.name)
			}
		})
	}
}
