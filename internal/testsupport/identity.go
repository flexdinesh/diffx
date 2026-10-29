package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

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
	CreateAccount(ctx context.Context, account identityservice.Account, passwordHash string, created time.Time) error
}

// RemoteAccount creates a passwordless member account, as an accepted invite does.
func RemoteAccount(t testing.TB, store AccountStore, username string) identityservice.Account {
	t.Helper()
	account := identityservice.Account{ID: secret(t), Username: username}
	if err := store.CreateAccount(t.Context(), account, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return account
}

// RemoteSession signs a member in without Google, as a completed sign-in does.
func RemoteSession(t testing.TB, store identityservice.Store, account identityservice.Account) identityservice.Session {
	t.Helper()
	session := identityservice.Session{Account: account, Secret: secret(t), CSRFToken: secret(t), ExpiresAt: time.Now().Add(identityservice.SessionLifetime)}
	hash := sha256.Sum256([]byte(session.Secret))
	if err := store.CreateSession(t.Context(), account.ID, "", hex.EncodeToString(hash[:]), session.CSRFToken, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	return session
}
