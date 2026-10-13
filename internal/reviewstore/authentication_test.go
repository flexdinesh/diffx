package reviewstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
)

const testPassword = "a correct password for test"

func testIdentity(t *testing.T) (*Store, *identityservice.Service) {
	t.Helper()
	store, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, identityservice.New(store)
}

func secretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

func TestRemoteAccountPersistenceAndBootstrapOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := identityservice.New(store)
	admin, err := service.Bootstrap(ctx, "Admin", testPassword)
	if err != nil || admin.Username != "admin" {
		t.Fatalf("bootstrap: %+v %v", admin, err)
	}
	session, err := service.Login(ctx, "ADMIN", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.CreateToken(ctx, admin.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Provision(ctx, "other", testPassword)
	if err != nil || other.ID == admin.ID {
		t.Fatalf("provision: %+v %v", other, err)
	}
	if _, err := service.Provision(ctx, "ADMIN", testPassword); !errors.Is(err, identityservice.ErrConflict) {
		t.Fatalf("duplicate canonical username: %v", err)
	}
	if err := service.ChangePassword(ctx, admin.ID, testPassword, "changed password is valid"); err != nil {
		t.Fatal(err)
	}
	var password, tokenHash, sessionHash string
	if err := store.db.QueryRow(`SELECT password_hash FROM remote_accounts WHERE user_id=?`, admin.ID).Scan(&password); err != nil || password == testPassword {
		t.Fatalf("password persisted unsafely: %v", err)
	}
	if err := store.db.QueryRow(`SELECT secret_hash FROM identity_tokens WHERE id=?`, token.ID).Scan(&tokenHash); err != nil || tokenHash != secretHash(token.Secret) {
		t.Fatalf("token not hashed: %v", err)
	}
	// Issue a session for the second owner to inspect durable hashing independently.
	otherSession, err := service.Login(ctx, "other", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT secret_hash FROM browser_sessions WHERE user_id=?`, other.ID).Scan(&sessionHash); err != nil || sessionHash != secretHash(otherSession.Secret) {
		t.Fatalf("session not hashed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = identityservice.New(store)
	again, err := service.Bootstrap(ctx, "new-admin", "another valid password")
	// Bootstrap is globally one-time, even when an env username is changed.
	if err != nil || again != admin {
		t.Fatalf("bootstrap changed owner: %+v %v", again, err)
	}
	accounts, err := service.Accounts(ctx)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("bootstrap created owner: %+v %v", accounts, err)
	}
	if _, err := service.Login(ctx, "admin", testPassword); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err := service.Login(ctx, "admin", "changed password is valid"); err != nil {
		t.Fatalf("restart replaced password: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("password change retained session: %v", err)
	}
	identity, err := service.AuthenticateToken(ctx, token.Secret)
	if err != nil || identity.Account != admin || identity.Purpose != identityservice.PurposeCollector {
		t.Fatalf("password change affected token: %+v %v", identity, err)
	}
}

func TestSessionTokenIsolationExpiryAndRevocation(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	first, err := service.Provision(ctx, "first", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Provision(ctx, "second", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Login(ctx, "first", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	authenticated, err := service.AuthenticateSession(ctx, session.Secret)
	if err != nil || authenticated.Account != first || authenticated.Secret != "" || authenticated.CSRFToken != session.CSRFToken {
		t.Fatalf("session: %+v %v", authenticated, err)
	}
	if _, err := store.ResolveSession(ctx, secretHash(session.Secret), session.ExpiresAt); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("session valid at expiry: %v", err)
	}
	token, err := service.CreateToken(ctx, first.ID, "upload", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	another, err := service.CreateToken(ctx, first.ID, "MCP", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if token.Secret == another.Secret || token.ID == another.ID {
		t.Fatal("tokens share identity")
	}
	if _, err := service.AuthenticateSession(ctx, token.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("token accepted as browser session: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("session accepted as token: %v", err)
	}
	empty, err := service.Tokens(ctx, second.ID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("tokens cross owner: %+v %v", empty, err)
	}
	if err := service.RevokeToken(ctx, second.ID, token.ID); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("cross-owner revoke: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, token.Secret); err != nil {
		t.Fatal(err)
	}
	listed, err := service.Tokens(ctx, first.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list tokens: %+v %v", listed, err)
	}
	var used bool
	for _, item := range listed {
		if item.ID == token.ID {
			used = item.LastUsedAt != nil
		}
	}
	if !used {
		t.Fatal("successful authentication did not record last use")
	}
	if _, err := store.ResolveIdentityToken(ctx, secretHash(token.Secret), token.ExpiresAt); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("token valid at expiry: %v", err)
	}
	if err := service.RevokeToken(ctx, first.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateToken(ctx, token.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("revoked token accepted: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, another.Secret); err != nil {
		t.Fatalf("revoked other token: %v", err)
	}
	if err := service.Logout(ctx, session.Secret); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateSession(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("logout retained session: %v", err)
	}
}

func TestPasswordResetFencesConcurrentCredentialOperations(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	account, err := service.Provision(ctx, "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Login(ctx, "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := store.AccountPassword(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, "OWNER", "reset password is valid"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplacePassword(ctx, account.ID, stale.Hash, "stale-change"); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("stale authenticated change overwrote reset: %v", err)
	}
	if err := store.CreateSession(ctx, account.ID, stale.Hash, "stale-session", "csrf", time.Now().Add(time.Hour)); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("stale login issued session: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("reset did not revoke session: %v", err)
	}
	if _, err := service.Login(ctx, "owner", "reset password is valid"); err != nil {
		t.Fatalf("reset password lost: %v", err)
	}
}

func TestConcurrentAccountProvisionHasOneOwner(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		workers.Go(func() { _, err := service.Provision(ctx, "Owner", testPassword); results <- err })
	}
	workers.Wait()
	close(results)
	var success int
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, identityservice.ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful creates: %d", success)
	}
	var users int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil || users != 1 {
		t.Fatalf("orphan owners: %d %v", users, err)
	}
}

func TestConcurrentBootstrapCreatesOneAccount(t *testing.T) {
	_, service := testIdentity(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	accounts := make(chan identityservice.Account, 4)
	failures := make(chan error, 4)
	for _, username := range []string{"first", "second", "third", "fourth"} {
		workers.Go(func() {
			account, err := service.Bootstrap(ctx, username, testPassword)
			accounts <- account
			failures <- err
		})
	}
	workers.Wait()
	close(accounts)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var owner string
	for account := range accounts {
		if owner == "" {
			owner = account.ID
		}
		if account.ID != owner {
			t.Fatal("concurrent bootstrap produced distinct owners")
		}
	}
	listed, err := service.Accounts(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("bootstrap accounts: %+v %v", listed, err)
	}
}

func TestConcurrentPasswordReplacementOnlyOneWins(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	account, err := service.Provision(ctx, "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := store.AccountPassword(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for _, replacement := range []string{"first-replacement", "second-replacement"} {
		workers.Go(func() { results <- store.ReplacePassword(ctx, account.ID, credential.Hash, replacement) })
	}
	workers.Wait()
	close(results)
	var success int
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, identityservice.ErrAuthentication) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("password replacements won: %d", success)
	}
}
