package pgstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

const adminPassword = "a correct password for test"

func openIdentityStore(t *testing.T, url string) *pgstore.Store {
	t.Helper()
	store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// identityStore returns a store on a fresh database and a direct connection to it.
func identityStore(t *testing.T) (*pgstore.Store, *identityservice.Service, *pgx.Conn) {
	t.Helper()
	url := pgtest.URL(t)
	store := openIdentityStore(t, url)
	return store, testsupport.Identity(t, store), identityConn(t, url)
}

func identityConn(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func secretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

func tableRows(t *testing.T, conn *pgx.Conn, table string) int {
	t.Helper()
	var rows int
	if err := conn.QueryRow(t.Context(), `SELECT COUNT(*) FROM diffx.`+table).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// member creates a Google account and a session for it, as Google sign-in does.
func member(t *testing.T, store *pgstore.Store, username string) (identityservice.Account, identityservice.Session) {
	t.Helper()
	account := identityservice.Account{ID: "id-" + username, Username: username, Email: username + "@example.com", Method: identityservice.MethodGoogle}
	if err := store.CreateGoogleAccount(t.Context(), account, google.Identity{Subject: "sub-" + username, Email: username + "@example.com", EmailVerified: true}, time.Now(), identityservice.Settings{}); err != nil {
		t.Fatal(err)
	}
	session := identityservice.Session{Account: account, Secret: strings.Repeat("s", 40) + username[:3], CSRFToken: "csrf-" + username, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.CreateSession(t.Context(), account.ID, "", secretHash(session.Secret), session.CSRFToken, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	return account, session
}

func setupAdmin(t *testing.T, store *pgstore.Store, username string) identityservice.Session {
	t.Helper()
	return testsupport.RemoteAdmin(t, store, username, adminPassword)
}

func TestAdminSetupPersistsAndRunsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.URL(t)
	conn := identityConn(t, url)
	store, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	service := testsupport.Identity(t, store)
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
	if err := conn.QueryRow(ctx, `SELECT token_hash FROM diffx.admin_setup`).Scan(&stored); err != nil || stored != secretHash(secret) {
		t.Fatalf("setup secret not hashed: %v", err)
	}
	if _, err := service.CompleteSetup(ctx, replaced, "admin@example.com", adminPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("replaced setup secret accepted: %v", err)
	}
	session, err := service.CompleteSetup(ctx, secret, "Admin@Example.com", adminPassword)
	if err != nil || !session.Account.Admin || session.Account.Username != "admin@example.com" || session.Account.Email != "admin@example.com" {
		t.Fatalf("setup: %+v %v", session, err)
	}
	if _, err := service.CompleteSetup(ctx, secret, "second@example.com", adminPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("setup secret reused: %v", err)
	}
	token, err := service.CreateToken(ctx, session.Account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := member(t, store, "other")
	if _, err := service.Login(ctx, identityservice.AudienceMembers, "other", ""); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("Google account password login: %v", err)
	}
	if err := service.ChangePassword(ctx, other.ID, "", "member password is valid"); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("Google account gained a password: %v", err)
	}
	if _, _, err := service.ResetPasswordByUsername(ctx, "other"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("Google account password reset: %v", err)
	}
	if err := service.ChangePassword(ctx, session.Account.ID, adminPassword, "changed password is valid"); err != nil {
		t.Fatal(err)
	}
	var password, tokenHash string
	if err := conn.QueryRow(ctx, `SELECT password_hash FROM diffx.remote_accounts WHERE user_id=$1`, session.Account.ID).Scan(&password); err != nil || password == adminPassword {
		t.Fatalf("password persisted unsafely: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT secret_hash FROM diffx.identity_tokens WHERE id=$1`, token.ID).Scan(&tokenHash); err != nil || tokenHash != secretHash(token.Secret) {
		t.Fatalf("token not hashed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// A restarted replica opens the same database.
	service = testsupport.Identity(t, openIdentityStore(t, url))
	if again, err := service.PrepareSetup(ctx); err != nil || again != "" {
		t.Fatalf("setup issued after admin exists: %q %v", again, err)
	}
	if required, err := service.SetupRequired(ctx); err != nil || required {
		t.Fatalf("setup still required: %v %v", required, err)
	}
	if _, err := service.Login(ctx, identityservice.AudienceConsole, "admin@example.com", adminPassword); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err := service.Login(ctx, identityservice.AudienceConsole, "ADMIN@EXAMPLE.COM", "changed password is valid"); err != nil {
		t.Fatalf("restart replaced password: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceConsole, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("password change retained session: %v", err)
	}
	identity, err := service.AuthenticateToken(ctx, token.Secret)
	if err != nil || identity.Account != session.Account || identity.Purpose != identityservice.PurposeCollector {
		t.Fatalf("password change affected token: %+v %v", identity, err)
	}
}

func TestExpiredSetupSecretIsRefused(t *testing.T) {
	t.Parallel()
	_, service, conn := identityStore(t)
	secret, err := service.PrepareSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), `UPDATE diffx.admin_setup SET expires_at=$1`, time.UnixMilli(time.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteSetup(t.Context(), secret, "admin@example.com", adminPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("expired setup accepted: %v", err)
	}
}

func TestNewPasswordAccountsRequireAndPersistEmail(t *testing.T) {
	for _, method := range []string{"setup", "signup", "invite"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			_, service, conn := identityStore(t)
			ctx := t.Context()
			email := strings.Repeat("long", 10) + "+diffx@example.com"
			var secret string
			var err error
			if method == "setup" {
				secret, err = service.PrepareSetup(ctx)
			} else if method == "invite" {
				_, secret, err = service.CreateInvite(ctx, email)
			}
			if err != nil {
				t.Fatal(err)
			}
			create := func(input string) (identityservice.Session, error) {
				switch method {
				case "setup":
					return service.CompleteSetup(ctx, secret, input, adminPassword)
				case "invite":
					return service.AcceptPasswordInvite(ctx, secret, input, adminPassword)
				default:
					return service.SignUp(ctx, input, adminPassword)
				}
			}
			if _, err := create("valid-handle"); !errors.Is(err, identityservice.ErrInvalidInput) {
				t.Fatalf("new password account accepted a handle: %v", err)
			}
			session, err := create(strings.ToUpper(email))
			if err != nil || session.Account.Username != email || session.Account.Email != email || session.Account.Method != identityservice.MethodPassword {
				t.Fatalf("email account: %+v %v", session.Account, err)
			}
			var persisted string
			if err := conn.QueryRow(ctx, `SELECT email FROM diffx.remote_accounts WHERE user_id=$1`, session.Account.ID).Scan(&persisted); err != nil || persisted != email {
				t.Fatalf("persisted password email: %q %v", persisted, err)
			}
			reopened := testsupport.Identity(t, openIdentityStore(t, conn.Config().ConnString()))
			loggedIn, err := reopened.Login(ctx, identityservice.AudienceMembers, strings.ToUpper(email), adminPassword)
			if err != nil || loggedIn.Account != session.Account {
				t.Fatalf("email login after reopen: %+v %v", loggedIn.Account, err)
			}
			listed, err := reopened.AccountSummaries(ctx)
			if err != nil || len(listed) != 1 || listed[0].Email != email {
				t.Fatalf("administrative email: %+v %v", listed, err)
			}
			if method == "signup" {
				if _, err := create(email); !errors.Is(err, identityservice.ErrConflict) {
					t.Fatalf("mixed-case duplicate signup: %v", err)
				}
			}
			if rows := tableRows(t, conn, "google_identities"); rows != 0 {
				t.Fatalf("password email became a Google identity: %d", rows)
			}
		})
	}
}

func TestPasswordInviteMismatchPreservesPendingInvite(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := t.Context()
	invited, secret, err := service.CreateInvite(ctx, "invited@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptPasswordInvite(ctx, secret, "other@example.com", adminPassword); !errors.Is(err, identityservice.ErrInviteMismatch) {
		t.Fatalf("invite accepted another email: %v", err)
	}
	if pending, err := service.PendingInvite(ctx, secret); err != nil || pending.ID != invited.ID || pending.Status(time.Now()) != identityservice.InvitePending {
		t.Fatalf("mismatch consumed invite: %+v %v", pending, err)
	}
	if accounts, err := store.IdentityAccounts(ctx); err != nil || len(accounts) != 0 {
		t.Fatalf("mismatch created an account: %+v %v", accounts, err)
	}
	session, err := service.AcceptPasswordInvite(ctx, secret, "Invited@Example.COM", adminPassword)
	if err != nil || session.Account.Email != invited.Email {
		t.Fatalf("matching email invite: %+v %v", session.Account, err)
	}
}

func TestLegacyPasswordHandleKeepsLoginAndReset(t *testing.T) {
	t.Parallel()
	store, service, conn := identityStore(t)
	ctx := t.Context()
	account := testsupport.RemotePasswordAccount(t, store, "legacy-user", adminPassword)
	if account.Email != "" {
		t.Fatalf("legacy fixture has email: %+v", account)
	}
	var email *string
	if err := conn.QueryRow(ctx, `SELECT email FROM diffx.remote_accounts WHERE user_id=$1`, account.ID).Scan(&email); err != nil || email != nil {
		t.Fatalf("legacy email was invented: %v %v", email, err)
	}
	session, err := service.Login(ctx, identityservice.AudienceMembers, "LEGACY-USER", adminPassword)
	if err != nil || session.Account != account {
		t.Fatalf("legacy login: %+v %v", session.Account, err)
	}
	resetAccount, secret, err := service.ResetPasswordByUsername(ctx, "LEGACY-USER")
	if err != nil || resetAccount.ID != account.ID {
		t.Fatalf("legacy recovery: %+v %v", resetAccount, err)
	}
	if err := service.CompletePasswordReset(ctx, identityservice.AudienceMembers, secret, "new legacy password"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, identityservice.AudienceMembers, "legacy-user", "new legacy password"); err != nil {
		t.Fatalf("legacy reset password: %v", err)
	}
}

// ownData records data the account owns, including a queued job, whose
// ingestion record has no cascade from the job.
func ownData(t *testing.T, conn *pgx.Conn, accountID string) {
	t.Helper()
	statements := []string{
		`INSERT INTO diffx.diffs(id, owner_id, kind, mode, created_at) VALUES ('diff-' || $1, $1, 'observation', 'all', now())`,
		`INSERT INTO diffx.ingestion_records(owner_id, source_id, submission_id, payload_hash) VALUES ($1, 'source', 'published', 'hash'), ($1, 'source', 'queued', 'hash')`,
		`INSERT INTO diffx.ingestion_jobs(id, sequence, payload, state, available_at) SELECT 'job-' || $1, sequence, '\x00', 'queued', now() FROM diffx.ingestion_records WHERE owner_id = $1 AND submission_id = 'queued'`,
		`INSERT INTO diffx.push_notifications(id, owner_id, job_id, kind, payload, state, available_at, created_at) VALUES ('notification-' || $1, $1, 'job-' || $1, 'pull_request', '\x00', 'pending', now(), now())`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(t.Context(), statement, accountID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDisableAndDeleteAccount(t *testing.T) {
	t.Parallel()
	store, service, conn := identityStore(t)
	ctx := context.Background()
	admin := setupAdmin(t, store, "admin")
	account, session := member(t, store, "member")
	token, err := service.CreateToken(ctx, account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	actor := admin.Account.ID
	for _, err := range []error{service.DisableAccount(ctx, actor, actor), service.DeleteAccount(ctx, actor, actor), service.SetAdmin(ctx, actor, actor, false)} {
		if !errors.Is(err, identityservice.ErrForbidden) {
			t.Fatalf("admin changed itself: %v", err)
		}
	}
	for _, err := range []error{service.DisableAccount(ctx, account.ID, actor), service.DeleteAccount(ctx, account.ID, actor), service.SetAdmin(ctx, account.ID, actor, false)} {
		if !errors.Is(err, identityservice.ErrLastAdmin) {
			t.Fatalf("last admin removed: %v", err)
		}
	}
	if err := service.DisableAccount(ctx, actor, "missing"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	if err := service.DisableAccount(ctx, actor, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceMembers, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
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
	if err := service.EnableAccount(ctx, actor, account.ID); err != nil {
		t.Fatal(err)
	}
	if tokens, err := service.Tokens(ctx, account.ID); err != nil || len(tokens) != 0 {
		t.Fatalf("enable restored credentials: %+v %v", tokens, err)
	}
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceMembers, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("enable restored session: %v", err)
	}
	if err := store.CreateSession(ctx, account.ID, "", "new-session", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("enabled account cannot sign in: %v", err)
	}
	ownData(t, conn, account.ID)
	if err := service.DeleteAccount(ctx, actor, account.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "remote_accounts", "browser_sessions", "ingestion_records", "ingestion_jobs", "push_notifications", "diffs"} {
		want := 0
		if table == "users" || table == "remote_accounts" || table == "browser_sessions" {
			want = 1
		}
		if rows := tableRows(t, conn, table); rows != want {
			t.Fatalf("%s rows after delete: %d", table, rows)
		}
	}
	if err := service.DeleteAccount(ctx, actor, account.ID); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := service.EnableAccount(ctx, actor, account.ID); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("enable deleted account: %v", err)
	}
	if _, err := service.CreateToken(ctx, account.ID, "collector", identityservice.PurposeCollector, time.Time{}); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("token for deleted account: %v", err)
	}
}

func TestSessionTokenIsolationExpiryAndRevocation(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	first, session := member(t, store, "first")
	second, _ := member(t, store, "second")
	authenticated, err := service.AuthenticateSession(ctx, identityservice.AudienceMembers, session.Secret)
	if err != nil || authenticated.Account != first || authenticated.Secret != "" || authenticated.CSRFToken != session.CSRFToken {
		t.Fatalf("session: %+v %v", authenticated, err)
	}
	if !authenticated.ExpiresAt.Equal(time.UnixMilli(session.ExpiresAt.UnixMilli())) {
		t.Fatalf("session expiry: %v, want %v", authenticated.ExpiresAt, session.ExpiresAt)
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
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceMembers, token.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
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
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceMembers, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("logout retained session: %v", err)
	}
}

func TestTokenTimesRoundTripAtMillisecondsAndLastUseNeverRegresses(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	account, _ := member(t, store, "owner")
	created := time.UnixMilli(1_700_000_000_123).Add(456 * time.Microsecond)
	token := identityservice.Token{ID: "token", Name: "upload", Purpose: identityservice.PurposeAPI, CreatedAt: created, ExpiresAt: created.Add(time.Hour)}
	if err := store.CreateIdentityToken(ctx, account.ID, token, "token-hash"); err != nil {
		t.Fatal(err)
	}
	later, earlier := created.Add(time.Minute), created.Add(time.Second)
	for _, now := range []time.Time{later, earlier} {
		identity, err := store.ResolveIdentityToken(ctx, "token-hash", now)
		if err != nil || identity.Account != account || identity.Purpose != identityservice.PurposeAPI || identity.ExpiresAt.UnixMilli() != token.ExpiresAt.UnixMilli() {
			t.Fatalf("resolve token: %+v %v", identity, err)
		}
	}
	tokens, err := store.IdentityTokens(ctx, account.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens: %+v %v", tokens, err)
	}
	got := tokens[0]
	if !got.CreatedAt.Equal(time.UnixMilli(created.UnixMilli())) || !got.ExpiresAt.Equal(time.UnixMilli(token.ExpiresAt.UnixMilli())) {
		t.Fatalf("token times: %v %v", got.CreatedAt, got.ExpiresAt)
	}
	if got.LastUsedAt == nil || got.LastUsedAt.UnixMilli() != later.UnixMilli() {
		t.Fatalf("last use regressed: %v", got.LastUsedAt)
	}
}

func TestAccountsOrderByUsernameBytes(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	for _, username := range []string{"ab", "a.c", "a-b", "a_d"} {
		if err := store.CreateAccount(ctx, identityservice.Account{ID: "id-" + username, Username: username}, "", time.Now(), identityservice.Settings{}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"a-b", "a.c", "a_d", "ab"}
	accounts, err := service.Accounts(ctx)
	if err != nil || len(accounts) != len(want) {
		t.Fatalf("accounts: %+v %v", accounts, err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != len(want) {
		t.Fatalf("summaries: %+v %v", summaries, err)
	}
	for index, username := range want {
		if accounts[index].Username != username || summaries[index].Username != username {
			t.Fatalf("order at %d: %q %q, want %q", index, accounts[index].Username, summaries[index].Username, username)
		}
	}
	if err := store.CreateAccount(ctx, identityservice.Account{ID: "id-other", Username: "ab"}, "", time.Now(), identityservice.Settings{}); !errors.Is(err, identityservice.ErrConflict) {
		t.Fatalf("duplicate username: %v", err)
	}
}

func TestPasswordResetFencesConcurrentCredentialOperations(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	session := setupAdmin(t, store, "owner")
	account := session.Account
	stale, err := store.AccountPassword(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.CreateToken(ctx, account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	reset, secret, err := service.ResetPasswordByUsername(ctx, "OWNER")
	if err != nil || reset.ID != account.ID || !reset.Admin {
		t.Fatalf("reset: %+v %v", reset, err)
	}
	if pending, err := service.PendingPasswordReset(ctx, identityservice.AudienceConsole, secret); err != nil || pending.Username != "owner" {
		t.Fatalf("pending reset: %+v %v", pending, err)
	}
	if err := service.CompletePasswordReset(ctx, identityservice.AudienceConsole, secret, "short"); !errors.Is(err, identityservice.ErrInvalidInput) {
		t.Fatalf("weak password accepted: %v", err)
	}
	if err := service.CompletePasswordReset(ctx, identityservice.AudienceConsole, secret, "reset password is valid"); err != nil {
		t.Fatal(err)
	}
	if err := service.CompletePasswordReset(ctx, identityservice.AudienceConsole, secret, "another password is valid"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("reset link reused: %v", err)
	}
	if err := store.ReplacePassword(ctx, account.ID, stale.Hash, "stale-change"); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("stale authenticated change overwrote reset: %v", err)
	}
	if err := store.CreateSession(ctx, account.ID, stale.Hash, "stale-session", "csrf", time.Now().Add(time.Hour)); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("stale login issued session: %v", err)
	}
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceConsole, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("reset did not revoke session: %v", err)
	}
	if _, err := service.AuthenticateToken(ctx, token.Secret); err != nil {
		t.Fatalf("reset revoked an API token: %v", err)
	}
	if _, err := service.Login(ctx, identityservice.AudienceConsole, "owner", "reset password is valid"); err != nil {
		t.Fatalf("reset password lost: %v", err)
	}
}

func TestPasswordResetLinksExpireAndStayInTheirAudience(t *testing.T) {
	t.Parallel()
	store, service, conn := identityStore(t)
	ctx := context.Background()
	if _, err := service.SignUp(ctx, "member@example.com", adminPassword); err != nil {
		t.Fatal(err)
	}
	google, _ := member(t, store, "google")
	if _, _, err := service.ResetPassword(ctx, google.ID); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("Google account reset: %v", err)
	}
	_, replaced, err := service.ResetPasswordByUsername(ctx, "member@example.com")
	if err != nil {
		t.Fatal(err)
	}
	account, secret, err := service.ResetPasswordByUsername(ctx, "member@example.com")
	if err != nil || account.Admin {
		t.Fatalf("reset: %+v %v", account, err)
	}
	if _, err := service.PendingPasswordReset(ctx, identityservice.AudienceMembers, replaced); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("replaced reset accepted: %v", err)
	}
	if _, err := service.PendingPasswordReset(ctx, identityservice.AudienceConsole, secret); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("member reset accepted by the console: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE diffx.password_resets SET expires_at=$1`, time.UnixMilli(time.Now().UnixMilli())); err != nil {
		t.Fatal(err)
	}
	if err := service.CompletePasswordReset(ctx, identityservice.AudienceMembers, secret, "reset password is valid"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("expired reset accepted: %v", err)
	}
	if err := store.PruneExpired(time.Now()); err != nil {
		t.Fatal(err)
	}
	if rows := tableRows(t, conn, "password_resets"); rows != 0 {
		t.Fatalf("expired resets kept: %d", rows)
	}
	// Existing password accounts keep their reset links with password signup off.
	testsupport.PutSettings(t, store, identityservice.Settings{})
	closed := testsupport.Identity(t, store)
	_, secret, err = closed.ResetPasswordByUsername(ctx, "member@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.CompletePasswordReset(ctx, identityservice.AudienceMembers, secret, "reset password is valid"); err != nil {
		t.Fatalf("reset refused with password signup off: %v", err)
	}
}

func TestSettingsGateNewAccountsOnly(t *testing.T) {
	t.Parallel()
	store, open, _ := identityStore(t)
	ctx := context.Background()
	admin := setupAdmin(t, store, "admin")
	if _, err := open.SignUp(ctx, "ab", adminPassword); !errors.Is(err, identityservice.ErrInvalidInput) {
		t.Fatalf("invalid username accepted: %v", err)
	}
	session, err := open.SignUp(ctx, "Member@Example.com", adminPassword)
	if err != nil || session.Account.Username != "member@example.com" || session.Account.Admin || session.Account.Method != identityservice.MethodPassword {
		t.Fatalf("sign-up: %+v %v", session, err)
	}
	if _, err := open.SignUp(ctx, "member@example.com", adminPassword); !errors.Is(err, identityservice.ErrConflict) {
		t.Fatalf("duplicate sign-up: %v", err)
	}
	token, err := open.CreateToken(ctx, session.Account.ID, "api", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open.Login(ctx, identityservice.AudienceConsole, "member@example.com", adminPassword); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("member signed in to the console: %v", err)
	}
	if _, err := open.AuthenticateSession(ctx, identityservice.AudienceConsole, session.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("member session admitted by the console: %v", err)
	}
	for _, settings := range []identityservice.Settings{{PasswordSignup: true}, {Signup: true}} {
		testsupport.PutSettings(t, store, settings)
		closed := testsupport.Identity(t, store)
		if _, err := closed.SignUp(ctx, "someone@example.com", adminPassword); !errors.Is(err, identityservice.ErrDisabled) {
			t.Fatalf("password sign-up with %+v: %v", settings, err)
		}
	}
	// Password accounts made before password signup closed keep every way in.
	closed := testsupport.Identity(t, store)
	if closed.Settings().PasswordSignup {
		t.Fatal("password signup still on")
	}
	if _, err := closed.Login(ctx, identityservice.AudienceMembers, "member@example.com", adminPassword); err != nil {
		t.Fatalf("existing password login: %v", err)
	}
	if _, err := closed.AuthenticateSession(ctx, identityservice.AudienceMembers, session.Secret); err != nil {
		t.Fatalf("existing password session: %v", err)
	}
	if _, err := closed.AuthenticateToken(ctx, token.Secret); err != nil {
		t.Fatalf("existing password account token: %v", err)
	}
	if err := closed.ChangePassword(ctx, session.Account.ID, adminPassword, "a changed password for test"); err != nil {
		t.Fatalf("existing password change: %v", err)
	}
	if _, err := closed.AuthenticateSession(ctx, identityservice.AudienceMembers, admin.Secret); err != nil {
		t.Fatalf("password admin refused by the web app: %v", err)
	}
	if accounts, err := closed.Accounts(ctx); err != nil || len(accounts) != 2 {
		t.Fatalf("worker accounts with password signup off: %+v %v", accounts, err)
	}
}

func TestSignupRechecksStoredSettings(t *testing.T) {
	t.Parallel()
	store, open, _ := identityStore(t)
	ctx := context.Background()
	// This replica's snapshot is stale: an admin closed signup elsewhere.
	testsupport.PutSettings(t, store, identityservice.Settings{PasswordSignup: true})
	if !open.Settings().Signup {
		t.Fatal("snapshot reloaded")
	}
	if _, err := open.SignUp(ctx, "late@example.com", adminPassword); !errors.Is(err, identityservice.ErrDisabled) {
		t.Fatalf("stale snapshot created a password account: %v", err)
	}
	account := identityservice.Account{ID: "id-late", Username: "late", Method: identityservice.MethodGoogle}
	identity := google.Identity{Subject: "sub-late", Email: "late@example.com", EmailVerified: true}
	if err := store.CreateGoogleAccount(ctx, account, identity, time.Now(), identityservice.Settings{Signup: true}); !errors.Is(err, identityservice.ErrDisabled) {
		t.Fatalf("closed signup created a Google account: %v", err)
	}
	testsupport.PutSettings(t, store, identityservice.Settings{Signup: true})
	invite := invite(t, store, "late-invite", "late@example.com", time.Now())
	password := identityservice.Account{ID: "id-invited", Username: "invited", Method: identityservice.MethodPassword}
	if err := store.AcceptPasswordInvite(ctx, invite.ID, password, "hash", time.Now(), identityservice.Settings{PasswordSignup: true}); !errors.Is(err, identityservice.ErrDisabled) {
		t.Fatalf("closed password signup accepted an invite: %v", err)
	}
	if err := store.AcceptInvite(ctx, invite.ID, account, identity, time.Now()); err != nil {
		t.Fatalf("Google invite with password signup off: %v", err)
	}
	if accounts, err := open.AccountSummaries(ctx); err != nil || len(accounts) != 1 {
		t.Fatalf("accounts: %+v %v", accounts, err)
	}
}

func TestSettingsDefaultOnAndAnnounceChanges(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	if got := service.Settings(); got != (identityservice.Settings{Signup: true, PasswordSignup: true}) {
		t.Fatalf("default settings: %+v", got)
	}
	if got := identityservice.New(store).Settings(); got != (identityservice.Settings{}) {
		t.Fatalf("unloaded snapshot: %+v", got)
	}
	listener, err := store.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Run closes the listener when its context ends; Close must not race it.
	listening, stop := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-stopped
	})
	changes := make(chan pgstore.Notification, 8)
	go func() {
		defer close(stopped)
		_ = listener.Run(listening, func(change pgstore.Notification) { changes <- change })
	}()
	admin := setupAdmin(t, store, "admin")
	want := identityservice.Settings{PasswordSignup: true}
	if err := service.PutSettings(ctx, admin.Account.ID, want); err != nil {
		t.Fatal(err)
	}
	if got := service.Settings(); got != want {
		t.Fatalf("snapshot after put: %+v", got)
	}
	timeout := time.After(10 * time.Second)
	for announced := false; !announced; {
		select {
		case change := <-changes:
			announced = change.Kind == pgstore.SettingsChanged
		case <-timeout:
			t.Fatal("settings change not announced")
		}
	}
	if got, err := identityservice.New(store).LoadSettings(ctx); err != nil || got != want {
		t.Fatalf("stored settings: %+v %v", got, err)
	}
	// Deleting the admin who changed them keeps the settings.
	other, _ := member(t, store, "other")
	if err := service.SetAdmin(ctx, admin.Account.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteAccount(ctx, other.ID, admin.Account.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := store.AppSettings(ctx); err != nil || got != want {
		t.Fatalf("settings after deleting their author: %+v %v", got, err)
	}
}

func TestAdminRoleChangesKeepAnEnabledAdmin(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	first := setupAdmin(t, store, "first").Account
	second, secondSession := member(t, store, "second")
	if _, err := service.AuthenticateSession(ctx, identityservice.AudienceConsole, secondSession.Secret); !errors.Is(err, identityservice.ErrAuthentication) {
		t.Fatalf("member admitted by the console: %v", err)
	}
	if err := service.SetAdmin(ctx, first.ID, second.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAdmin(ctx, first.ID, second.ID, true); err != nil {
		t.Fatalf("grant twice: %v", err)
	}
	if session, err := service.AuthenticateSession(ctx, identityservice.AudienceConsole, secondSession.Secret); err != nil || !session.Account.Admin {
		t.Fatalf("promoted admin refused by the console: %+v %v", session, err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != 2 || !summaries[0].Admin || !summaries[1].Admin || summaries[1].Method != identityservice.MethodGoogle || summaries[1].Email != "second@example.com" {
		t.Fatalf("summaries: %+v %v", summaries, err)
	}
	// Two admins demoting each other at once leave exactly one.
	results := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(func() { results <- service.SetAdmin(ctx, first.ID, second.ID, false) })
	workers.Go(func() { results <- service.SetAdmin(ctx, second.ID, first.ID, false) })
	workers.Wait()
	close(results)
	var demoted int
	for err := range results {
		if err == nil {
			demoted++
		} else if !errors.Is(err, identityservice.ErrLastAdmin) {
			t.Fatal(err)
		}
	}
	summaries, err = service.AccountSummaries(ctx)
	if err != nil || demoted != 1 || summaries[0].Admin == summaries[1].Admin {
		t.Fatalf("admins after mutual demotion: %d %+v %v", demoted, summaries, err)
	}
	remaining, other := first, second
	if summaries[1].Admin {
		remaining, other = second, first
	}
	if err := service.DisableAccount(ctx, other.ID, remaining.ID); !errors.Is(err, identityservice.ErrLastAdmin) {
		t.Fatalf("last admin disabled: %v", err)
	}
	if err := service.SetAdmin(ctx, remaining.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.DisableAccount(ctx, other.ID, remaining.ID); err != nil {
		t.Fatalf("admin disabled with another admin enabled: %v", err)
	}
	if err := service.DeleteAccount(ctx, remaining.ID, other.ID); !errors.Is(err, identityservice.ErrLastAdmin) {
		t.Fatalf("last enabled admin deleted: %v", err)
	}
	if err := service.DeleteAccount(ctx, other.ID, remaining.ID); err != nil {
		t.Fatalf("disabled admin not deletable: %v", err)
	}
}

func TestConcurrentAccountCreationHasOneOwner(t *testing.T) {
	t.Parallel()
	store, _, conn := identityStore(t)
	ctx := context.Background()
	var workers sync.WaitGroup
	results := make(chan error, 6)
	for index := range 6 {
		workers.Go(func() {
			results <- store.CreateAccount(ctx, identityservice.Account{ID: fmt.Sprintf("id-%d", index), Username: "owner"}, "", time.Now(), identityservice.Settings{})
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
	if users := tableRows(t, conn, "users"); users != 1 {
		t.Fatalf("orphan owners: %d", users)
	}
}

func TestConcurrentSetupCreatesOneAdmin(t *testing.T) {
	t.Parallel()
	_, service, _ := identityStore(t)
	ctx := context.Background()
	secret, err := service.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	failures := make(chan error, 4)
	for _, username := range []string{"first@example.com", "second@example.com", "third@example.com", "fourth@example.com"} {
		workers.Go(func() {
			_, err := service.CompleteSetup(ctx, secret, username, adminPassword)
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

// waitForLockWaiters returns once sessions on the test database wait on locks.
func waitForLockWaiters(t *testing.T, conn *pgx.Conn, waiters int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := conn.QueryRow(t.Context(), `SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= waiters {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d sessions waiting on locks", waiting, waiters)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// holdAccount holds an uncommitted account with the username and, unless
// empty, the Google subject, so transactions claiming either wait until the
// returned transaction rolls back.
func holdAccount(t *testing.T, conn *pgx.Conn, username, subject string) pgx.Tx {
	t.Helper()
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO diffx.users(id, name, created_at) VALUES ('holder', $1, now())`, username); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO diffx.remote_accounts(user_id, username, password_hash) VALUES ('holder', $1, '')`, username); err != nil {
		t.Fatal(err)
	}
	if subject == "" {
		return tx
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO diffx.google_identities(sub, user_id, email, updated_at) VALUES ($1, 'holder', 'holder@example.com', now())`, subject); err != nil {
		t.Fatal(err)
	}
	return tx
}

// Setup issued while an admin commits is never stored, so no live secret
// remains once an admin exists.
func TestSetupIssuanceDuringAdminCreationLeavesNoSecret(t *testing.T) {
	t.Parallel()
	url := pgtest.URL(t)
	store := openIdentityStore(t, url)
	conn, blocker := identityConn(t, url), identityConn(t, url)
	ctx := context.Background()
	if stored, err := store.PutAdminSetup(ctx, "setup", time.Now().Add(time.Hour)); err != nil || !stored {
		t.Fatalf("put setup: %v %v", stored, err)
	}
	// Admin creation consumes the secret, then waits on the username.
	hold := holdAccount(t, blocker, "admin", "")
	var workers sync.WaitGroup
	var created, issued error
	var stored bool
	workers.Go(func() {
		created = store.CreateAdmin(ctx, identityservice.Account{ID: "admin", Username: "admin", Admin: true}, "hash", "setup", time.Now())
	})
	waitForLockWaiters(t, conn, 1)
	workers.Go(func() { stored, issued = store.PutAdminSetup(ctx, "replacement", time.Now().Add(time.Hour)) })
	waitForLockWaiters(t, conn, 2)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if created != nil || issued != nil || stored {
		t.Fatalf("create %v, issue %v %v", created, stored, issued)
	}
	if pending := tableRows(t, conn, "admin_setup"); pending != 0 {
		t.Fatalf("setup secrets after admin creation: %d", pending)
	}
}

func TestConcurrentPasswordReplacementOnlyOneWins(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	account := setupAdmin(t, store, "owner").Account
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

// A session issued against a hash racing its replacement never survives it.
func TestConcurrentSessionIssuanceNeverOutlivesPasswordChange(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	account := setupAdmin(t, store, "owner").Account
	for attempt := range 20 {
		credential, err := store.AccountPassword(ctx, "owner")
		if err != nil {
			t.Fatal(err)
		}
		hash := fmt.Sprintf("session-%d", attempt)
		var workers sync.WaitGroup
		var issued, replaced error
		workers.Go(func() {
			issued = store.CreateSession(ctx, account.ID, credential.Hash, hash, "csrf", time.Now().Add(time.Hour))
		})
		workers.Go(func() {
			replaced = store.ReplacePassword(ctx, account.ID, credential.Hash, fmt.Sprintf("replacement-%d", attempt))
		})
		workers.Wait()
		if replaced != nil || issued != nil && !errors.Is(issued, identityservice.ErrAuthentication) {
			t.Fatalf("attempt %d: issue %v, replace %v", attempt, issued, replaced)
		}
		if _, err := store.ResolveSession(ctx, hash, time.Now()); !errors.Is(err, identityservice.ErrAuthentication) {
			t.Fatalf("attempt %d: session outlived password change: %v", attempt, err)
		}
	}
}

// A session issued racing a disable never returns when the account is enabled.
func TestConcurrentSessionIssuanceNeverOutlivesDisable(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	account, _ := member(t, store, "member")
	for attempt := range 20 {
		hash := fmt.Sprintf("session-%d", attempt)
		var workers sync.WaitGroup
		var issued, disabled error
		workers.Go(func() { issued = store.CreateSession(ctx, account.ID, "", hash, "csrf", time.Now().Add(time.Hour)) })
		workers.Go(func() { disabled = service.DisableAccount(ctx, "admin", account.ID) })
		workers.Wait()
		if disabled != nil || issued != nil && !errors.Is(issued, identityservice.ErrAuthentication) {
			t.Fatalf("attempt %d: issue %v, disable %v", attempt, issued, disabled)
		}
		if err := service.EnableAccount(ctx, "admin", account.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveSession(ctx, hash, time.Now()); !errors.Is(err, identityservice.ErrAuthentication) {
			t.Fatalf("attempt %d: session outlived disable: %v", attempt, err)
		}
	}
}
