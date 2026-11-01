package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

func invite(t *testing.T, store *pgstore.Store, id, email string, now time.Time) identityservice.Invite {
	t.Helper()
	value := identityservice.Invite{ID: id, Email: email, CreatedAt: now, ExpiresAt: now.Add(identityservice.InviteLifetime)}
	if err := store.CreateInvite(t.Context(), value, "hash-"+id); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestInviteReplacementAcceptanceAndRevocation(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	first := invite(t, store, "first", "a@example.com", now)
	second := invite(t, store, "second", "a@example.com", now.Add(time.Second))
	if replaced, err := store.Invite(ctx, first.ID); err != nil || replaced.Status(now) != identityservice.InviteRevoked {
		t.Fatalf("re-invite did not replace pending invite: %+v %v", replaced, err)
	}
	if _, err := store.PendingInvite(ctx, "hash-first", now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("replaced invite pending: %v", err)
	}
	if pending, err := store.PendingInvite(ctx, "hash-second", now); err != nil || pending.ID != second.ID {
		t.Fatalf("pending invite: %+v %v", pending, err)
	}
	if _, err := store.PendingInvite(ctx, "hash-second", second.ExpiresAt); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("expired invite pending: %v", err)
	}
	identity := google.Identity{Subject: "subject-a", Email: "a@example.com", EmailVerified: true}
	var workers sync.WaitGroup
	results := make(chan error, 4)
	for index := range 4 {
		workers.Go(func() {
			account := identityservice.Account{ID: "account-" + string(rune('a'+index)), Email: identity.Email, Method: identityservice.MethodGoogle}
			results <- store.AcceptInvite(ctx, second.ID, account, identity, now)
		})
	}
	workers.Wait()
	close(results)
	var accepted int
	for err := range results {
		// Later acceptances find the invite used or its email taken.
		if err == nil {
			accepted++
		} else if !errors.Is(err, identityservice.ErrNotFound) && !errors.Is(err, identityservice.ErrConflict) {
			t.Fatal(err)
		}
	}
	accounts, err := store.IdentityAccounts(ctx)
	if accepted != 1 || err != nil || len(accounts) != 1 {
		t.Fatalf("concurrent acceptance: %d %+v %v", accepted, accounts, err)
	}
	used, err := store.Invite(ctx, second.ID)
	if err != nil || used.Status(now) != identityservice.InviteUsed || used.UsedBy != accounts[0].Email {
		t.Fatalf("used invite: %+v %v", used, err)
	}
	if err := store.CreateInvite(ctx, identityservice.Invite{ID: "third", Email: "a@example.com", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, "hash-third"); !errors.Is(err, identityservice.ErrConflict) {
		t.Fatalf("invited an email an account uses: %v", err)
	}
	if err := store.RevokeInvite(ctx, second.ID, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("revoked a used invite: %v", err)
	}
	other := invite(t, store, "other", "b@example.com", now)
	if err := store.RevokeInvite(ctx, other.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptInvite(ctx, other.ID, identityservice.Account{ID: "account-revoked", Email: "b@example.com"}, google.Identity{Subject: "subject-b", Email: "b@example.com"}, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("revoked invite accepted: %v", err)
	}
	if again := invite(t, store, "again", "c@example.com", now); store.AcceptInvite(ctx, again.ID, identityservice.Account{ID: "account-linked", Email: "c@example.com"}, identity, now) == nil {
		t.Fatal("a linked Google subject created a second account")
	}
}

func TestInvitesListNewestFirstWithMillisecondTimes(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.UnixMilli(1_700_000_000_123).Add(789 * time.Microsecond)
	older := invite(t, store, "older", "a@example.com", now)
	newer := invite(t, store, "newer", "b@example.com", now.Add(time.Second))
	if err := store.RevokeInvite(ctx, older.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	invites, err := store.Invites(ctx)
	if err != nil || len(invites) != 2 || invites[0].ID != newer.ID || invites[1].ID != older.ID {
		t.Fatalf("invites: %+v %v", invites, err)
	}
	listed := invites[1]
	if !listed.CreatedAt.Equal(time.UnixMilli(older.CreatedAt.UnixMilli())) || !listed.ExpiresAt.Equal(time.UnixMilli(older.ExpiresAt.UnixMilli())) {
		t.Fatalf("invite times: %v %v", listed.CreatedAt, listed.ExpiresAt)
	}
	if listed.UsedAt != nil || listed.UsedBy != "" || listed.RevokedAt == nil || listed.RevokedAt.UnixMilli() != now.Add(time.Minute).UnixMilli() {
		t.Fatalf("revoked invite: %+v", listed)
	}
	if _, err := store.Invite(ctx, "missing"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("missing invite: %v", err)
	}
	if err := store.RevokeInvite(ctx, "missing", now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("revoked a missing invite: %v", err)
	}
}

func TestGoogleAccountFollowsEmailChangesAndDisable(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	value := invite(t, store, "invite", "old@example.com", now)
	account := identityservice.Account{ID: "account", Email: "old@example.com", Method: identityservice.MethodGoogle}
	if err := store.AcceptInvite(ctx, value.ID, account, google.Identity{Subject: "subject", Email: "old@example.com", EmailVerified: true}, now); err != nil {
		t.Fatal(err)
	}
	if credential, err := store.AccountPasswordByID(ctx, account.ID); err != nil || credential.Account.Email != "old@example.com" {
		t.Fatalf("google email: %+v %v", credential.Account, err)
	}
	account.Email = "new@example.com"
	resolved, disabled, err := store.GoogleAccount(ctx, google.Identity{Subject: "subject", Email: "new@example.com"}, now)
	if err != nil || disabled || resolved != account {
		t.Fatalf("google account: %+v %v %v", resolved, disabled, err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != 1 || summaries[0].Email != "new@example.com" {
		t.Fatalf("email change not recorded: %+v %v", summaries, err)
	}
	if credential, err := store.AccountPasswordByID(ctx, account.ID); err != nil || credential.Account.Email != "new@example.com" {
		t.Fatalf("google email after change: %+v %v", credential.Account, err)
	}
	if _, err := store.AccountPasswordByID(ctx, "missing"); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("missing account: %v", err)
	}
	if _, _, err := store.GoogleAccount(ctx, google.Identity{Subject: "other", Email: "new@example.com"}, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("email matched another subject: %v", err)
	}
	if err := service.DisableAccount(ctx, "admin", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, disabled, err := store.GoogleAccount(ctx, google.Identity{Subject: "subject", Email: "new@example.com"}, now); err != nil || !disabled {
		t.Fatalf("disabled google account: %v %v", disabled, err)
	}
	if err := service.DeleteAccount(ctx, "admin", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GoogleAccount(ctx, google.Identity{Subject: "subject"}, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("deleted account kept its Google identity: %v", err)
	}
	if used, err := store.Invite(ctx, value.ID); err != nil || used.Status(now) != identityservice.InviteUsed || used.UsedBy != "" {
		t.Fatalf("invite after account deletion: %+v %v", used, err)
	}
}

func TestGoogleFlowIsConsumedOnce(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	flow := identityservice.GoogleFlow{BindingHash: "binding", Nonce: "nonce", Verifier: "verifier", ExpiresAt: now.Add(time.Minute)}
	if err := store.PutGoogleFlow(ctx, "state", flow); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGoogleFlow(ctx, "expired", identityservice.GoogleFlow{BindingHash: "b", Nonce: "n", Verifier: "v", ExpiresAt: now}); err != nil {
		t.Fatal(err)
	}
	taken, err := store.TakeGoogleFlow(ctx, "state", now)
	if err != nil || taken.BindingHash != "binding" || taken.Nonce != "nonce" || taken.Verifier != "verifier" || taken.InviteID != "" || !taken.ExpiresAt.Equal(time.UnixMilli(flow.ExpiresAt.UnixMilli())) {
		t.Fatalf("take flow: %+v %v", taken, err)
	}
	for _, state := range []string{"state", "expired", "unknown"} {
		if _, err := store.TakeGoogleFlow(ctx, state, now); !errors.Is(err, identityservice.ErrNotFound) {
			t.Fatalf("flow %s taken: %v", state, err)
		}
	}
}

func TestGoogleFlowCarriesInviteAndPrunesExpired(t *testing.T) {
	t.Parallel()
	store, _, conn := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	value := invite(t, store, "invite", "a@example.com", now)
	if err := store.PutGoogleFlow(ctx, "expired", identityservice.GoogleFlow{BindingHash: "b", Nonce: "n", Verifier: "v", ExpiresAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGoogleFlow(ctx, "state", identityservice.GoogleFlow{BindingHash: "b", Nonce: "n", Verifier: "v", InviteID: value.ID, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if flows := tableRows(t, conn, "google_auth_states"); flows != 1 {
		t.Fatalf("expired flow kept: %d flows", flows)
	}
	taken, err := store.TakeGoogleFlow(ctx, "state", now)
	if err != nil || taken.InviteID != value.ID {
		t.Fatalf("take invite flow: %+v %v", taken, err)
	}
}

func TestConcurrentGoogleFlowIsTakenOnce(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := store.PutGoogleFlow(ctx, "state", identityservice.GoogleFlow{BindingHash: "b", Nonce: "n", Verifier: "v", ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			_, err := store.TakeGoogleFlow(ctx, "state", now)
			results <- err
		})
	}
	workers.Wait()
	close(results)
	var taken int
	for err := range results {
		if err == nil {
			taken++
		} else if !errors.Is(err, identityservice.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if taken != 1 {
		t.Fatalf("flow taken %d times", taken)
	}
}

func TestConcurrentInvitesLeaveOnePending(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	var workers sync.WaitGroup
	results := make(chan error, 6)
	for index := range 6 {
		workers.Go(func() {
			id := fmt.Sprintf("invite-%d", index)
			results <- store.CreateInvite(ctx, identityservice.Invite{ID: id, Email: "a@example.com", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, "hash-"+id)
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	invites, err := store.Invites(ctx)
	if err != nil || len(invites) != 6 {
		t.Fatalf("invites: %+v %v", invites, err)
	}
	var pending int
	for _, value := range invites {
		if value.Status(now) == identityservice.InvitePending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending invites for one email: %d", pending)
	}
}

func TestConcurrentAcceptancesLinkSubjectOnce(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	ctx := context.Background()
	now := time.Now()
	emails := []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"}
	var workers sync.WaitGroup
	results := make(chan error, len(emails))
	for index, email := range emails {
		value := invite(t, store, fmt.Sprintf("invite-%d", index), email, now)
		workers.Go(func() {
			account := identityservice.Account{ID: fmt.Sprintf("account-%d", index), Email: email}
			results <- store.AcceptInvite(ctx, value.ID, account, google.Identity{Subject: "subject", Email: email, EmailVerified: true}, now)
		})
	}
	workers.Wait()
	close(results)
	var accepted int
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, identityservice.ErrNotFound) {
			t.Fatal(err)
		}
	}
	accounts, err := store.IdentityAccounts(ctx)
	if accepted != 1 || err != nil || len(accounts) != 1 {
		t.Fatalf("subject linked: %d %+v %v", accepted, accounts, err)
	}
}

// Re-inviting an email while its invite is being accepted waits for the
// acceptance, then conflicts with the account it linked.
func TestInviteDuringAcceptanceOfEmailConflicts(t *testing.T) {
	t.Parallel()
	url := pgtest.URL(t)
	store := openIdentityStore(t, url)
	conn, blocker := identityConn(t, url), identityConn(t, url)
	ctx := context.Background()
	now := time.Now()
	value := invite(t, store, "invite", "a@example.com", now)
	// Acceptance uses the invite, then waits on the subject.
	hold := holdAccount(t, blocker, "holder@example.com", "subject")
	var workers sync.WaitGroup
	var accepted, invited error
	workers.Go(func() {
		accepted = store.AcceptInvite(ctx, value.ID, identityservice.Account{ID: "account", Email: "a@example.com"}, google.Identity{Subject: "subject", Email: "a@example.com", EmailVerified: true}, now)
	})
	waitForLockWaiters(t, conn, 1)
	workers.Go(func() {
		invited = store.CreateInvite(ctx, identityservice.Invite{ID: "again", Email: "a@example.com", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, "hash-again")
	})
	waitForLockWaiters(t, conn, 2)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if accepted != nil || !errors.Is(invited, identityservice.ErrConflict) {
		t.Fatalf("accept %v, invite %v", accepted, invited)
	}
	if _, err := store.PendingInvite(ctx, "hash-again", now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("pending invite for a linked email: %v", err)
	}
}

func TestInvitesCreateMembersByEitherMethod(t *testing.T) {
	t.Parallel()
	store, service, _ := identityStore(t)
	ctx := context.Background()
	member, memberSecret, err := service.CreateInvite(ctx, "Member@Example.com")
	if err != nil || member.Email != "member@example.com" {
		t.Fatalf("invite: %+v %v", member, err)
	}
	// Invites admit people whether or not signup is open.
	testsupport.PutSettings(t, store, identityservice.Settings{})
	closed := testsupport.Identity(t, store)
	if _, err := closed.AcceptPasswordInvite(ctx, memberSecret, "member@example.com", adminPassword); !errors.Is(err, identityservice.ErrDisabled) {
		t.Fatalf("password invite accepted while password signup is off: %v", err)
	}
	testsupport.PutSettings(t, store, identityservice.Settings{PasswordSignup: true})
	inviteOnly := testsupport.Identity(t, store)
	session, err := inviteOnly.AcceptPasswordInvite(ctx, memberSecret, "member@example.com", adminPassword)
	if err != nil || session.Account.Admin || session.Account.Method != identityservice.MethodPassword {
		t.Fatalf("member invite: %+v %v", session, err)
	}
	if _, err := inviteOnly.AcceptPasswordInvite(ctx, memberSecret, "member@example.com", adminPassword); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("invite reused: %v", err)
	}
	if used, err := store.Invite(ctx, member.ID); err != nil || used.UsedBy != "member@example.com" {
		t.Fatalf("used invite: %+v %v", used, err)
	}
	now := time.Now()
	googleInvite := invite(t, store, "google-member", "g@example.com", now)
	account := identityservice.Account{ID: "google-member", Email: "g@example.com", Method: identityservice.MethodGoogle}
	if err := store.AcceptInvite(ctx, googleInvite.ID, account, google.Identity{Subject: "subject-g", Email: "g@example.com", EmailVerified: true}, now); err != nil {
		t.Fatal(err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != 2 {
		t.Fatalf("accounts: %+v %v", summaries, err)
	}
	for _, summary := range summaries {
		if summary.Admin {
			t.Fatalf("invite made an admin: %+v", summary)
		}
	}
}

func TestPasswordInviteStorageBindsEmail(t *testing.T) {
	t.Parallel()
	store, service, conn := identityStore(t)
	ctx := t.Context()
	now := time.Now()
	invited := invite(t, store, "password-invite", "invited@example.com", now)
	account := identityservice.Account{ID: "other-account", Email: "other@example.com", Method: identityservice.MethodPassword}
	if err := store.AcceptPasswordInvite(ctx, invited.ID, account, "hash", now, identityservice.Settings{PasswordSignup: true}); err == nil {
		t.Fatal("storage accepted an email different from the invite")
	}
	if rows := tableRows(t, conn, "users"); rows != 0 {
		t.Fatalf("mismatched acceptance retained an owner: %d", rows)
	}
	if pending, err := service.Invites(ctx); err != nil || len(pending) != 1 || pending[0].Status(now) != identityservice.InvitePending {
		t.Fatalf("mismatched acceptance consumed invite: %+v %v", pending, err)
	}
}
