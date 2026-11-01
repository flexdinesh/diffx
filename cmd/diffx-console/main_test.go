package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

func TestConsoleRefusesIncompleteConfiguration(t *testing.T) {
	t.Setenv("DIFFX_CONSOLE_DATABASE_URL", "")
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "DIFFX_CONSOLE_DATABASE_URL") {
		t.Fatalf("missing database: %v", err)
	}
	t.Setenv("DIFFX_CONSOLE_DATABASE_URL", "postgres://console")
	if err := run(t.Context(), nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "DIFFX_CONSOLE_APP_URL") {
		t.Fatalf("missing app origin: %v", err)
	}
	if err := run(t.Context(), []string{"extra"}, &stdout, &stderr); err == nil {
		t.Fatal("accepted a positional argument")
	}
	if err := run(t.Context(), []string{"--version"}, &stdout, &stderr); err != nil || stdout.Len() == 0 {
		t.Fatalf("version: %v %q", err, stdout.String())
	}
}

func TestResetPasswordPrintsALinkForTheAccountsSurface(t *testing.T) {
	url := pgtest.URL(t)
	store, err := pgstore.Open(context.Background(), url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	testsupport.RemoteAdmin(t, store, "root", "console-password-000001")
	if _, err := testsupport.Identity(t, store).SignUp(t.Context(), "member@example.com", "console-password-000001"); err != nil {
		t.Fatal(err)
	}
	testsupport.RemoteAccount(t, store, "google")
	t.Setenv("DIFFX_CONSOLE_DATABASE_URL", url)
	t.Setenv("DIFFX_CONSOLE_APP_URL", "https://diffx.example.com")
	t.Setenv("DIFFX_CONSOLE_PUBLIC_URL", "https://console.example.com")
	for username, prefix := range map[string]string{"root": "https://console.example.com/reset-password#t=", "member@example.com": "https://diffx.example.com/reset-password#t="} {
		var stdout, stderr bytes.Buffer
		if err := run(t.Context(), []string{"user", "reset-password", "--username", username}, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v", username, err)
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[1], prefix) {
			t.Fatalf("%s: %q", username, stdout.String())
		}
	}
	var stdout, stderr bytes.Buffer
	for _, arguments := range [][]string{{"user"}, {"user", "reset-password"}, {"user", "reset-password", "--username", "google"}, {"user", "reset-password", "--username", "nobody"}} {
		if err := run(t.Context(), arguments, &stdout, &stderr); err == nil {
			t.Fatalf("%v succeeded", arguments)
		}
	}
	if err := run(t.Context(), []string{"user", "reset-password", "--username", "google"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "no password account named google") {
		t.Fatalf("Google account: %v", err)
	}
}
