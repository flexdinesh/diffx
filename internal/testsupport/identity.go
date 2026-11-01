package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"golang.org/x/crypto/argon2"
)

func secret(t testing.TB) string {
	t.Helper()
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

// RemotePasswordAccount restores a legacy handle account for compatibility
// fixtures. New accounts must go through email-only setup, signup or invites.
func RemotePasswordAccount(t testing.TB, store identityservice.Store, username, password string) identityservice.Account {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	encoded := fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
	account := identityservice.Account{ID: secret(t), Username: username, Method: identityservice.MethodPassword}
	if err := store.CreateAccount(t.Context(), account, encoded, time.Now(), identityservice.Settings{}); err != nil {
		t.Fatal(err)
	}
	return account
}

// RemoteAdmin restores a legacy admin for tests unrelated to first-admin setup.
func RemoteAdmin(t testing.TB, store identityservice.Store, username, password string) identityservice.Session {
	t.Helper()
	account := RemotePasswordAccount(t, store, username, password)
	if err := store.SetAdmin(t.Context(), account.ID, true); err != nil {
		t.Fatal(err)
	}
	session, err := identityservice.New(store).Login(t.Context(), identityservice.AudienceConsole, username, password)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// Identity composes the identity service with the stored settings loaded, as
// a server replica does before serving.
func Identity(t testing.TB, store identityservice.Store, options ...identityservice.Option) *identityservice.Service {
	t.Helper()
	identity := identityservice.New(store, options...)
	if _, err := identity.LoadSettings(t.Context()); err != nil {
		t.Fatal(err)
	}
	return identity
}

// PutSettings stores app settings, as an admin does in the console.
func PutSettings(t testing.TB, store identityservice.Store, settings identityservice.Settings) {
	t.Helper()
	if err := store.PutAppSettings(t.Context(), settings, "", time.Now()); err != nil {
		t.Fatal(err)
	}
}

// AccountStore creates accounts directly; server storage implements it.
type AccountStore interface {
	CreateGoogleAccount(ctx context.Context, account identityservice.Account, identity google.Identity, now time.Time, required identityservice.Settings) error
}

// RemoteAccount creates a Google member account, as an accepted invite does.
func RemoteAccount(t testing.TB, store AccountStore, username string) identityservice.Account {
	t.Helper()
	account := identityservice.Account{ID: secret(t), Username: username, Email: username + "@example.com", Method: identityservice.MethodGoogle}
	identity := google.Identity{Subject: "subject-" + account.ID, Email: username + "@example.com", EmailVerified: true}
	if err := store.CreateGoogleAccount(t.Context(), account, identity, time.Now(), identityservice.Settings{}); err != nil {
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
