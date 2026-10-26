package reviewstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

// member creates a passwordless account and a session for it, as Google sign-in does.
func member(t *testing.T, store *Store, username string) (identityservice.Account, identityservice.Session) {
	t.Helper()
	account := identityservice.Account{ID: "id-" + username, Username: username}
	if err := store.CreateAccount(t.Context(), account, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	session := identityservice.Session{Account: account, Secret: strings.Repeat("s", 40) + username[:3], CSRFToken: "csrf-" + username, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.CreateSession(t.Context(), account.ID, "", secretHash(session.Secret), session.CSRFToken, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	return account, session
}

func setupAdmin(t *testing.T, service *identityservice.Service, username string) identityservice.Session {
	t.Helper()
	secret, err := service.PrepareSetup(t.Context())
	if err != nil || secret == "" {
		t.Fatalf("prepare setup: %q %v", secret, err)
	}
	session, err := service.CompleteSetup(t.Context(), secret, username, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestAdminSetupPersistsAndRunsOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := identityservice.New(store)
	if required, err := service.SetupRequired(ctx); err != nil || !required {
		t.Fatalf("fresh state requires setup: %v %v", required, err)
	}
	replaced, err := service.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := service.PrepareSetup(ctx)
	if err != nil || secret == "" || secret == replaced {
		t.Fatalf("prepare setup: %q %v", secret, err)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT token_hash FROM admin_setup`).Scan(&stored); err != nil || stored != secretHash(secret) {
		t.Fatalf("setup secret not hashed: %v", err)
	}
	if _, err := service.CompleteSetup(ctx, replaced, "admin", testPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("replaced setup secret accepted: %v", err)
	}
	session, err := service.CompleteSetup(ctx, secret, "Admin", testPassword)
	if err != nil || !session.Account.Admin || session.Account.Username != "admin" {
		t.Fatalf("setup: %+v %v", session, err)
	}
	if _, err := service.CompleteSetup(ctx, secret, "second", testPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("setup secret reused: %v", err)
	}
	token, err := service.CreateToken(ctx, session.Account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := member(t, store, "other")
	if _, err := service.Login(ctx, "other", ""); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("member password login: %v", err)
	}
	if err := service.ChangePassword(ctx, other.ID, "", "member password is valid"); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("member gained a password: %v", err)
	}
	if err := service.ResetPassword(ctx, "other", "member password is valid"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("member password reset: %v", err)
	}
	if err := service.ChangePassword(ctx, session.Account.ID, testPassword, "changed password is valid"); err != nil {
		t.Fatal(err)
	}
	var password, tokenHash string
	if err := store.db.QueryRow(`SELECT password_hash FROM remote_accounts WHERE user_id=?`, session.Account.ID).Scan(&password); err != nil || password == testPassword {
		t.Fatalf("password persisted unsafely: %v", err)
	}
	if err := store.db.QueryRow(`SELECT secret_hash FROM identity_tokens WHERE id=?`, token.ID).Scan(&tokenHash); err != nil || tokenHash != secretHash(token.Secret) {
		t.Fatalf("token not hashed: %v", err)
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
	if again, err := service.PrepareSetup(ctx); err != nil || again != "" {
		t.Fatalf("setup issued after admin exists: %q %v", again, err)
	}
	if required, err := service.SetupRequired(ctx); err != nil || required {
		t.Fatalf("setup still required: %v %v", required, err)
	}
	if _, err := service.Login(ctx, "admin", testPassword); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err := service.Login(ctx, "ADMIN", "changed password is valid"); err != nil {
		t.Fatalf("restart replaced password: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("password change retained session: %v", err)
	}
	identity, err := service.AuthenticateToken(ctx, token.Secret)
	if err != nil || identity.Account != session.Account || identity.Purpose != identityservice.PurposeCollector {
		t.Fatalf("password change affected token: %+v %v", identity, err)
	}
}

func TestExpiredSetupSecretIsRefused(t *testing.T) {
	store, service := testIdentity(t)
	secret, err := service.PrepareSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE admin_setup SET expires_at=?`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteSetup(t.Context(), secret, "admin", testPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("expired setup accepted: %v", err)
	}
}

func TestDisableAndDeleteAccount(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	admin := setupAdmin(t, service, "admin")
	account, session := member(t, store, "member")
	token, err := service.CreateToken(ctx, account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{service.DisableAccount(ctx, admin.Account.ID), service.DeleteAccount(ctx, admin.Account.ID)} {
		if !errors.Is(err, identityservice.ErrForbidden) {
			t.Fatalf("admin changed: %v", err)
		}
	}
	if err := service.DisableAccount(ctx, "missing"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	if err := service.DisableAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateSession(ctx, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("disabled session accepted: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, token.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("disabled token accepted: %v", err)
	}
	if err := store.CreateSession(ctx, account.ID, "", "new-session", "csrf", time.Now().Add(time.Hour)); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("disabled account signed in: %v", err)
	}
	accounts, err := service.Accounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0] != admin.Account {
		t.Fatalf("disabled account served: %+v %v", accounts, err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != 2 || !summaries[0].Admin || !summaries[1].Disabled || summaries[1].CreatedAt.IsZero() {
		t.Fatalf("summaries: %+v %v", summaries, err)
	}
	if err := service.EnableAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if tokens, err := service.Tokens(ctx, account.ID); err != nil || len(tokens) != 0 {
		t.Fatalf("enable restored credentials: %+v %v", tokens, err)
	}
	if err := store.CreateSession(ctx, account.ID, "", "new-session", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("enabled account cannot sign in: %v", err)
	}
	if _, err := store.Ingest(account.ID, observationRequest("source", "published")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queue().Accept(ctx, account.ID, queuedRequest("queued")); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "remote_accounts", "browser_sessions", "observations", "ingestion_records", "ingestion_jobs", "diffs"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if table == "users" || table == "remote_accounts" || table == "browser_sessions" {
			want = 1
		}
		if count != want {
			t.Fatalf("%s rows after delete: %d", table, count)
		}
	}
	if err := service.DeleteAccount(ctx, account.ID); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestSessionTokenIsolationExpiryAndRevocation(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	first, session := member(t, store, "first")
	second, _ := member(t, store, "second")
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
	session := setupAdmin(t, service, "owner")
	account := session.Account
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

func TestConcurrentAccountCreationHasOneOwner(t *testing.T) {
	store, _ := testIdentity(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	results := make(chan error, 6)
	for index := range 6 {
		workers.Go(func() {
			results <- store.CreateAccount(ctx, identityservice.Account{ID: fmt.Sprintf("id-%d", index), Username: "owner"}, "", time.Now())
		})
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

func TestConcurrentSetupCreatesOneAdmin(t *testing.T) {
	_, service := testIdentity(t)
	ctx := context.Background()
	secret, err := service.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 4)
	for _, username := range []string{"first", "second", "third", "fourth"} {
		workers.Go(func() {
			_, err := service.CompleteSetup(ctx, secret, username, testPassword)
			failures <- err
		})
	}
	workers.Wait()
	close(failures)
	var success int
	for err := range failures {
		if err == nil {
			success++
		} else if !errors.Is(err, identityservice.ErrNotFound) {
			t.Fatal(err)
		}
	}
	listed, err := service.Accounts(ctx)
	if success != 1 || err != nil || len(listed) != 1 || !listed[0].Admin {
		t.Fatalf("setup accounts: %d %+v %v", success, listed, err)
	}
}

func TestConcurrentPasswordReplacementOnlyOneWins(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	account := setupAdmin(t, service, "owner").Account
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
