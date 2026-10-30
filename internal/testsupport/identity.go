package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
)

func secret(t testing.TB) string {
	t.Helper()
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

// RemoteAdmin completes one-time setup, as the logged setup link does.
func RemoteAdmin(t testing.TB, store identityservice.Store, username, password string) identityservice.Session {
	t.Helper()
	identity := identityservice.New(store)
	setup, err := identity.PrepareSetup(t.Context())
	if err != nil || setup == "" {
		t.Fatalf("prepare setup: %q %v", setup, err)
	}
	session, err := identity.CompleteSetup(t.Context(), setup, username, password)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// AccountStore creates accounts directly; server storage implements it.
type AccountStore interface {
	CreateGoogleAccount(ctx context.Context, account identityservice.Account, identity google.Identity, now time.Time) error
}

// RemoteAccount creates a Google member account, as an accepted invite does.
func RemoteAccount(t testing.TB, store AccountStore, username string) identityservice.Account {
	t.Helper()
	account := identityservice.Account{ID: secret(t), Username: username, Method: identityservice.MethodGoogle}
	identity := google.Identity{Subject: "subject-" + account.ID, Email: username + "@example.com", EmailVerified: true}
	if err := store.CreateGoogleAccount(t.Context(), account, identity, time.Now()); err != nil {
		t.Fatal(err)
	}
	return account
}

func RemoteSession(t testing.TB, store identityservice.Store, account identityservice.Account) identityservice.Session {
	t.Helper()
	session := identityservice.Session{Account: account, Secret: secret(t), CSRFToken: secret(t), ExpiresAt: time.Now().Add(identityservice.SessionLifetime)}
	hash := sha256.Sum256([]byte(session.Secret))
	if err := store.CreateSession(t.Context(), account.ID, "", hex.EncodeToString(hash[:]), session.CSRFToken, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	return session
}
