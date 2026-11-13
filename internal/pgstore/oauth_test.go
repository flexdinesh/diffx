package pgstore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/oauthservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
)

const oauthResource = "https://diffx.example/mcp"

var oauthClient = oauthservice.Client{ID: "https://app.example/client.json", Name: "App", Verified: true}

func oauthRequest(id string, expires time.Time) oauthservice.Request {
	return oauthservice.Request{ID: id, Client: oauthClient, RedirectURI: "http://127.0.0.1/cb", State: "s", CodeChallenge: "challenge",
		Scopes: []string{"reviews:read", "comments:write"}, Resource: oauthResource, ExpiresAt: expires}
}

func issued(name string, now time.Time) oauthservice.IssuedTokens {
	return oauthservice.IssuedTokens{FamilyID: "family-" + name, AccessHash: secretHash("access-" + name), AccessExpiresAt: now.Add(time.Hour),
		RefreshHash: secretHash("refresh-" + name), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)}
}

// consented stores a consented code for the account and returns its hash.
func consented(t *testing.T, store *pgstore.Store, accountID, name string, now time.Time) string {
	t.Helper()
	if err := store.PutOAuthRequest(t.Context(), oauthRequest("request-"+name, now.Add(time.Minute)), now); err != nil {
		t.Fatal(err)
	}
	code := oauthservice.Code{AccountID: accountID, Client: oauthClient, RedirectURI: "http://127.0.0.1/cb", CodeChallenge: "challenge",
		Scopes: []string{"reviews:read"}, Resource: oauthResource, ExpiresAt: now.Add(time.Minute)}
	if _, err := store.ConsumeOAuthRequest(t.Context(), "request-"+name, &code, secretHash("code-"+name), now); err != nil {
		t.Fatal(err)
	}
	return secretHash("code-" + name)
}

func redemption(codeHash string) oauthservice.Redemption {
	return oauthservice.Redemption{CodeHash: codeHash, ClientID: oauthClient.ID, RedirectURI: "http://127.0.0.1/cb", CodeChallenge: "challenge", Resource: oauthResource}
}

func TestOAuthStoreRequestsAndClients(t *testing.T) {
	t.Parallel()
	store, _, _ := identityStore(t)
	account, _ := member(t, store, "alice")
	now := time.Now()
	client := oauthservice.Client{ID: "diffx_client_x", Name: "Cursor", RedirectURIs: []string{"https://cursor.example/cb"}}
	if err := store.CreateOAuthClient(t.Context(), client, now); err != nil {
		t.Fatal(err)
	}
	if stored, err := store.OAuthClient(t.Context(), client.ID); err != nil || stored.Name != "Cursor" || stored.Verified || len(stored.RedirectURIs) != 1 {
		t.Fatalf("client: %+v %v", stored, err)
	}
	if _, err := store.OAuthClient(t.Context(), "unknown"); !errors.Is(err, oauthservice.ErrNotFound) {
		t.Fatalf("unknown client: %v", err)
	}
	if err := store.PutOAuthRequest(t.Context(), oauthRequest("expired", now.Add(-time.Second)), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OAuthRequest(t.Context(), "expired", now); !errors.Is(err, oauthservice.ErrNotFound) {
		t.Fatalf("expired request: %v", err)
	}
	if err := store.PutOAuthRequest(t.Context(), oauthRequest("live", now.Add(time.Minute)), now); err != nil {
		t.Fatal(err)
	}
	if request, err := store.OAuthRequest(t.Context(), "live", now); err != nil || request.Client.Name != "App" || !request.Client.Verified || len(request.Scopes) != 2 {
		t.Fatalf("request: %+v %v", request, err)
	}
	// A request is answered once.
	if _, err := store.ConsumeOAuthRequest(t.Context(), "live", nil, "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeOAuthRequest(t.Context(), "live", nil, "", now); !errors.Is(err, oauthservice.ErrNotFound) {
		t.Fatalf("answered twice: %v", err)
	}
	// A disabled account cannot consent.
	if err := store.PutOAuthRequest(t.Context(), oauthRequest("disabled", now.Add(time.Minute)), now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAccountDisabled(t.Context(), account.ID, true, now); err != nil {
		t.Fatal(err)
	}
	code := oauthservice.Code{AccountID: account.ID, Client: oauthClient, Scopes: []string{"reviews:read"}, Resource: oauthResource, ExpiresAt: now.Add(time.Minute)}
	if _, err := store.ConsumeOAuthRequest(t.Context(), "disabled", &code, secretHash("c"), now); !errors.Is(err, oauthservice.ErrNotFound) {
		t.Fatalf("disabled consent: %v", err)
	}
}

func TestOAuthStoreTokenFamilies(t *testing.T) {
	t.Parallel()
	store, _, conn := identityStore(t)
	account, _ := member(t, store, "alice")
	now := time.Now()
	codeHash := consented(t, store, account.ID, "one", now)
	wrong := redemption(codeHash)
	wrong.CodeChallenge = "other"
	if _, err := store.RedeemOAuthCode(t.Context(), wrong, issued("x", now), now); !errors.Is(err, oauthservice.ErrInvalidGrant) {
		t.Fatalf("wrong challenge: %v", err)
	}
	scopes, err := store.RedeemOAuthCode(t.Context(), redemption(codeHash), issued("one", now), now)
	if err != nil || len(scopes) != 1 {
		t.Fatalf("redeem: %v %v", scopes, err)
	}
	access, err := store.ResolveOAuthToken(t.Context(), secretHash("access-one"), now)
	if err != nil || access.AccountID != account.ID || access.AccountEmail != account.Email || access.Client.ID != oauthClient.ID || access.Resource != oauthResource {
		t.Fatalf("resolve: %+v %v", access, err)
	}
	if _, err := store.ResolveOAuthToken(t.Context(), secretHash("refresh-one"), now); !errors.Is(err, oauthservice.ErrInvalidToken) {
		t.Fatalf("refresh token used as access: %v", err)
	}
	if _, err := store.ResolveOAuthToken(t.Context(), secretHash("access-one"), now.Add(2*time.Hour)); !errors.Is(err, oauthservice.ErrInvalidToken) {
		t.Fatalf("expired access: %v", err)
	}
	// Rotation keeps the family; a second consent keeps one grant.
	refresh := oauthservice.Refresh{TokenHash: secretHash("refresh-one"), ClientID: oauthClient.ID, Resource: oauthResource}
	if _, err := store.RefreshOAuthToken(t.Context(), refresh, issued("two", now), now); err != nil {
		t.Fatal(err)
	}
	other := consented(t, store, account.ID, "other", now)
	if _, err := store.RedeemOAuthCode(t.Context(), redemption(other), issued("other", now), now); err != nil {
		t.Fatal(err)
	}
	grants, err := store.OAuthGrants(t.Context(), account.ID)
	if err != nil || len(grants) != 1 || grants[0].LastUsedAt == nil {
		t.Fatalf("grants: %+v %v", grants, err)
	}
	// Reusing the rotated token revokes its family only.
	if _, err := store.RefreshOAuthToken(t.Context(), refresh, issued("three", now), now); !errors.Is(err, oauthservice.ErrInvalidGrant) {
		t.Fatalf("reuse: %v", err)
	}
	for name, want := range map[string]error{"two": oauthservice.ErrInvalidToken, "one": oauthservice.ErrInvalidToken, "other": nil} {
		if _, err := store.ResolveOAuthToken(t.Context(), secretHash("access-"+name), now); !errors.Is(err, want) {
			t.Fatalf("access %s after reuse: %v", name, err)
		}
	}
	// Revoking the grant ends every token; deleting the account removes all.
	if err := store.DeleteOAuthGrant(t.Context(), "someone-else", grants[0].ID); !errors.Is(err, oauthservice.ErrNotFound) {
		t.Fatalf("foreign grant: %v", err)
	}
	if err := store.DeleteOAuthGrant(t.Context(), account.ID, grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if rows := tableRows(t, conn, "oauth_tokens"); rows != 0 {
		t.Fatalf("tokens after revoke: %d", rows)
	}
	consented(t, store, account.ID, "last", now)
	if err := store.DeleteAccount(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"oauth_codes", "oauth_grants", "oauth_tokens"} {
		if rows := tableRows(t, conn, table); rows != 0 {
			t.Fatalf("%s after delete: %d", table, rows)
		}
	}
}
