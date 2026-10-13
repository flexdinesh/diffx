package identityservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUsernamePolicy(t *testing.T) {
	for _, input := range []string{"", "ab", " owner", "owner ", ".owner", "name:name", "name/name", "name\n", "éname", strings.Repeat("a", 33)} {
		if _, err := canonicalUsername(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %q: %v", input, err)
		}
	}
	for _, input := range []string{"Owner", "123", "A-b_c.d"} {
		username, err := canonicalUsername(input)
		if err != nil || username != strings.ToLower(input) {
			t.Fatalf("canonical %q: %q %v", input, username, err)
		}
	}
}

func TestPasswordPolicyAndSaltedHash(t *testing.T) {
	for _, length := range []int{0, 11, 129} {
		if err := validatePassword(strings.Repeat("a", length)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %d bytes", length)
		}
	}
	for _, length := range []int{12, 128} {
		if err := validatePassword(strings.Repeat("a", length)); err != nil {
			t.Fatalf("rejected %d bytes: %v", length, err)
		}
	}
	password := "password with enough bytes"
	first, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password salt reused")
	}
	if !verifyPassword(first, password) || verifyPassword(first, "wrong") {
		t.Fatal("password verification incorrect")
	}
	for _, bad := range []string{"", password, strings.Replace(first, "m=19456", "m=9999999999", 1), strings.Replace(first, "t=2", "t=9999999999", 1), strings.Replace(first, "v=19", "v=16", 1), first + "A"} {
		if verifyPassword(bad, password) {
			t.Fatalf("accepted malformed/unbounded hash %q", bad)
		}
	}
}

type tokenStore struct {
	Store
	calls int
	token Token
}

func (store *tokenStore) CreateIdentityToken(_ context.Context, _ string, token Token, _ string) error {
	store.calls++
	store.token = token
	return nil
}

func TestTokenPolicy(t *testing.T) {
	store := &tokenStore{}
	service := New(store)
	now := time.Unix(1700000000, 0)
	service.now = func() time.Time { return now }
	invalid := []struct {
		name    string
		purpose Purpose
		expires time.Time
	}{
		{"", PurposeCollector, time.Time{}}, {"\n", PurposeCollector, time.Time{}}, {strings.Repeat("x", 65), PurposeCollector, time.Time{}},
		{"name", "admin", time.Time{}}, {"name", PurposeCollector, now}, {"name", PurposeCollector, now.Add(MaximumTokenLifetime + time.Second)},
	}
	for _, item := range invalid {
		if _, err := service.CreateToken(context.Background(), "owner", item.name, item.purpose, item.expires); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted invalid token: %+v %v", item, err)
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid token reached storage")
	}
	created, err := service.CreateToken(context.Background(), "owner", " collector ", PurposeCollector, time.Time{})
	if err != nil || len(created.Secret) != 43 || created.ID == created.Secret || created.Name != "collector" || !created.ExpiresAt.Equal(now.Add(DefaultTokenLifetime)) {
		t.Fatalf("default token: %+v %v", created, err)
	}
	if _, err := service.CreateToken(context.Background(), "owner", "api", PurposeAPI, now.Add(MaximumTokenLifetime)); err != nil {
		t.Fatalf("max expiry rejected: %v", err)
	}
}
