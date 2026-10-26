package reviewstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
)

func invite(t *testing.T, store *Store, id, email string, now time.Time) identityservice.Invite {
	t.Helper()
	value := identityservice.Invite{ID: id, Email: email, CreatedAt: now, ExpiresAt: now.Add(identityservice.InviteLifetime)}
	if err := store.CreateInvite(t.Context(), value, "hash-"+id); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestInviteReplacementAcceptanceAndRevocation(t *testing.T) {
	store, _ := testIdentity(t)
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
			account := identityservice.Account{ID: "account-" + string(rune('a'+index)), Username: "user-" + string(rune('a'+index))}
			results <- store.AcceptInvite(ctx, second.ID, account, identity, now)
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
		t.Fatalf("concurrent acceptance: %d %+v %v", accepted, accounts, err)
	}
	used, err := store.Invite(ctx, second.ID)
	if err != nil || used.Status(now) != identityservice.InviteUsed || used.UsedBy != accounts[0].Username {
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
	if err := store.AcceptInvite(ctx, other.ID, identityservice.Account{ID: "account-revoked", Username: "bob"}, google.Identity{Subject: "subject-b", Email: "b@example.com"}, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("revoked invite accepted: %v", err)
	}
	if again := invite(t, store, "again", "c@example.com", now); store.AcceptInvite(ctx, again.ID, identityservice.Account{ID: "account-linked", Username: "carol"}, identity, now) == nil {
		t.Fatal("a linked Google subject created a second account")
	}
}

func TestGoogleAccountFollowsEmailChangesAndDisable(t *testing.T) {
	store, service := testIdentity(t)
	ctx := context.Background()
	now := time.Now()
	value := invite(t, store, "invite", "old@example.com", now)
	account := identityservice.Account{ID: "account", Username: "old"}
	if err := store.AcceptInvite(ctx, value.ID, account, google.Identity{Subject: "subject", Email: "old@example.com", EmailVerified: true}, now); err != nil {
		t.Fatal(err)
	}
	resolved, disabled, err := store.GoogleAccount(ctx, google.Identity{Subject: "subject", Email: "new@example.com"}, now)
	if err != nil || disabled || resolved != account {
		t.Fatalf("google account: %+v %v %v", resolved, disabled, err)
	}
	summaries, err := service.AccountSummaries(ctx)
	if err != nil || len(summaries) != 1 || summaries[0].Email != "new@example.com" {
		t.Fatalf("email change not recorded: %+v %v", summaries, err)
	}
	if _, _, err := store.GoogleAccount(ctx, google.Identity{Subject: "other", Email: "new@example.com"}, now); !errors.Is(err, identityservice.ErrNotFound) {
		t.Fatalf("email matched another subject: %v", err)
	}
	if err := service.DisableAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, disabled, err := store.GoogleAccount(ctx, google.Identity{Subject: "subject", Email: "new@example.com"}, now); err != nil || !disabled {
		t.Fatalf("disabled google account: %v %v", disabled, err)
	}
	if err := service.DeleteAccount(ctx, account.ID); err != nil {
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
	store, _ := testIdentity(t)
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
	if err != nil || taken.BindingHash != "binding" || taken.Verifier != "verifier" || taken.InviteID != "" {
		t.Fatalf("take flow: %+v %v", taken, err)
	}
	for _, state := range []string{"state", "expired", "unknown"} {
		if _, err := store.TakeGoogleFlow(ctx, state, now); !errors.Is(err, identityservice.ErrNotFound) {
			t.Fatalf("flow %s taken: %v", state, err)
		}
	}
}
