package pgstore

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/flexdinesh/diffx/internal/oauthservice"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
)

var _ oauthservice.Store = (*Store)(nil)

const (
	accessKind  = "access"
	refreshKind = "refresh"
)

func (store *Store) CreateOAuthClient(ctx context.Context, client oauthservice.Client, created time.Time) error {
	return store.queries.InsertOAuthClient(ctx, pgdb.InsertOAuthClientParams{ClientID: client.ID, ClientName: client.Name, RedirectUris: client.RedirectURIs, CreatedAt: millisecond(created)})
}

func (store *Store) OAuthClient(ctx context.Context, clientID string) (oauthservice.Client, error) {
	row, err := store.queries.OAuthClient(ctx, clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oauthservice.Client{}, oauthservice.ErrNotFound
	}
	if err != nil {
		return oauthservice.Client{}, err
	}
	return oauthservice.Client{ID: row.ClientID, Name: row.ClientName, RedirectURIs: row.RedirectUris}, nil
}

// PutOAuthRequest also removes expired requests.
func (store *Store) PutOAuthRequest(ctx context.Context, request oauthservice.Request, now time.Time) error {
	if err := store.queries.PruneOAuthRequests(ctx, now); err != nil {
		return err
	}
	return store.queries.InsertOAuthRequest(ctx, pgdb.InsertOAuthRequestParams{
		ID: request.ID, ClientID: request.Client.ID, ClientName: request.Client.Name, Verified: request.Client.Verified, RedirectUri: request.RedirectURI,
		State: request.State, CodeChallenge: request.CodeChallenge, Scopes: request.Scopes, Resource: request.Resource, ExpiresAt: millisecond(request.ExpiresAt),
	})
}

func oauthRequest(row pgdb.OauthRequest) oauthservice.Request {
	return oauthservice.Request{ID: row.ID, Client: oauthservice.Client{ID: row.ClientID, Name: row.ClientName, Verified: row.Verified}, RedirectURI: row.RedirectUri,
		State: row.State, CodeChallenge: row.CodeChallenge, Scopes: row.Scopes, Resource: row.Resource, ExpiresAt: millisecond(row.ExpiresAt)}
}

func (store *Store) OAuthRequest(ctx context.Context, id string, now time.Time) (oauthservice.Request, error) {
	row, err := store.queries.OAuthRequest(ctx, pgdb.OAuthRequestParams{ID: id, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return oauthservice.Request{}, oauthservice.ErrNotFound
	}
	if err != nil {
		return oauthservice.Request{}, err
	}
	return oauthRequest(row), nil
}

// ConsumeOAuthRequest locks the consenting account before issuing its code,
// so a concurrent disable either sees and revokes the code or commits first
// and refuses it.
func (store *Store) ConsumeOAuthRequest(ctx context.Context, id string, code *oauthservice.Code, codeHash string, now time.Time) (oauthservice.Request, error) {
	var request oauthservice.Request
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		row, err := queries.ConsumeOAuthRequest(ctx, pgdb.ConsumeOAuthRequestParams{ID: id, Now: now})
		if errors.Is(err, pgx.ErrNoRows) {
			return oauthservice.ErrNotFound
		}
		if err != nil {
			return err
		}
		request = oauthRequest(row)
		if code == nil {
			return nil
		}
		if err := queries.LockSessionAccount(ctx, code.AccountID); err != nil {
			return err
		}
		if err := queries.PruneOAuthCodes(ctx, now); err != nil {
			return err
		}
		count, err := queries.InsertOAuthCode(ctx, pgdb.InsertOAuthCodeParams{
			CodeHash: codeHash, ClientID: code.Client.ID, ClientName: code.Client.Name, Verified: code.Client.Verified, RedirectUri: code.RedirectURI,
			CodeChallenge: code.CodeChallenge, Scopes: code.Scopes, Resource: code.Resource, ExpiresAt: millisecond(code.ExpiresAt), UserID: code.AccountID,
		})
		if err != nil {
			return err
		}
		if count != 1 {
			return oauthservice.ErrNotFound
		}
		return nil
	})
	return request, err
}

// lockOAuthAccount locks an account against a concurrent disable or delete
// and reports whether it is enabled. Callers lock the account before its
// OAuth rows, in the order disabling takes them, so the two never deadlock.
func lockOAuthAccount(ctx context.Context, queries *pgdb.Queries, accountID string) (bool, error) {
	if err := queries.LockSessionAccount(ctx, accountID); err != nil {
		return false, err
	}
	return queries.OAuthAccountEnabled(ctx, accountID)
}

func insertTokens(ctx context.Context, queries *pgdb.Queries, grantID string, scopes []string, resource string, tokens oauthservice.IssuedTokens, now time.Time) error {
	if err := queries.PruneOAuthTokens(ctx, now); err != nil {
		return err
	}
	for _, token := range []pgdb.InsertOAuthTokenParams{
		{SecretHash: tokens.AccessHash, Kind: accessKind, ExpiresAt: millisecond(tokens.AccessExpiresAt)},
		{SecretHash: tokens.RefreshHash, Kind: refreshKind, ExpiresAt: millisecond(tokens.RefreshExpiresAt)},
	} {
		token.GrantID, token.FamilyID, token.Scopes, token.Resource = grantID, tokens.FamilyID, scopes, resource
		if err := queries.InsertOAuthToken(ctx, token); err != nil {
			return err
		}
	}
	return nil
}

// errRevoked commits a family's revocation while the request still fails.
var errRevoked = errors.New("oauth family revoked")

// revokingTx runs work in a transaction; when work returns errRevoked the
// revocation commits and the caller gets ErrInvalidGrant.
func (store *Store) revokingTx(ctx context.Context, work func(*pgdb.Queries) error) error {
	var revoked bool
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		err := work(queries)
		if errors.Is(err, errRevoked) {
			revoked = true
			return nil
		}
		return err
	})
	if err == nil && revoked {
		return oauthservice.ErrInvalidGrant
	}
	return err
}

func (store *Store) RedeemOAuthCode(ctx context.Context, redemption oauthservice.Redemption, tokens oauthservice.IssuedTokens, now time.Time) ([]string, error) {
	var scopes []string
	err := store.revokingTx(ctx, func(queries *pgdb.Queries) error {
		owner, err := queries.OAuthCodeOwner(ctx, redemption.CodeHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return oauthservice.ErrInvalidGrant
		}
		if err != nil {
			return err
		}
		enabled, err := lockOAuthAccount(ctx, queries, owner)
		if err != nil {
			return err
		}
		code, err := queries.LockOAuthCode(ctx, redemption.CodeHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return oauthservice.ErrInvalidGrant
		}
		if err != nil {
			return err
		}
		if code.FamilyID != nil {
			// A replayed code revokes the tokens it issued (RFC 6749 section 4.1.2).
			if err := queries.DeleteOAuthFamily(ctx, *code.FamilyID); err != nil {
				return err
			}
			return errRevoked
		}
		if !enabled || !now.Before(code.ExpiresAt) || code.ClientID != redemption.ClientID || code.RedirectUri != redemption.RedirectURI ||
			code.CodeChallenge != redemption.CodeChallenge || code.Resource != redemption.Resource {
			return oauthservice.ErrInvalidGrant
		}
		if err := queries.RedeemOAuthCode(ctx, pgdb.RedeemOAuthCodeParams{FamilyID: &tokens.FamilyID, CodeHash: code.CodeHash}); err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		grantID, err := queries.UpsertOAuthGrant(ctx, pgdb.UpsertOAuthGrantParams{ID: id, UserID: code.UserID, ClientID: code.ClientID, ClientName: code.ClientName,
			Verified: code.Verified, Scopes: code.Scopes, Now: millisecond(now)})
		if err != nil {
			return err
		}
		scopes = code.Scopes
		return insertTokens(ctx, queries, grantID, code.Scopes, code.Resource, tokens, now)
	})
	return scopes, err
}

func (store *Store) RefreshOAuthToken(ctx context.Context, refresh oauthservice.Refresh, tokens oauthservice.IssuedTokens, now time.Time) ([]string, error) {
	var scopes []string
	err := store.revokingTx(ctx, func(queries *pgdb.Queries) error {
		owner, err := queries.OAuthRefreshOwner(ctx, refresh.TokenHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return oauthservice.ErrInvalidGrant
		}
		if err != nil {
			return err
		}
		enabled, err := lockOAuthAccount(ctx, queries, owner)
		if err != nil {
			return err
		}
		token, err := queries.LockOAuthRefresh(ctx, refresh.TokenHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return oauthservice.ErrInvalidGrant
		}
		if err != nil {
			return err
		}
		if token.ClientID != refresh.ClientID || token.UserID != owner {
			return oauthservice.ErrInvalidGrant
		}
		if token.RotatedAt != nil {
			// A rotated token presented again may have leaked: end its family.
			if err := queries.DeleteOAuthFamily(ctx, token.FamilyID); err != nil {
				return err
			}
			return errRevoked
		}
		if !enabled || !now.Before(token.ExpiresAt) || token.Resource != refresh.Resource {
			return oauthservice.ErrInvalidGrant
		}
		rotated := millisecond(now)
		if err := queries.RotateOAuthRefresh(ctx, pgdb.RotateOAuthRefreshParams{Now: &rotated, SecretHash: token.SecretHash}); err != nil {
			return err
		}
		if err := queries.TouchOAuthGrant(ctx, pgdb.TouchOAuthGrantParams{Now: millisecond(now), ID: token.GrantID}); err != nil {
			return err
		}
		scopes = token.Scopes
		tokens.FamilyID = token.FamilyID
		return insertTokens(ctx, queries, token.GrantID, token.Scopes, token.Resource, tokens, now)
	})
	return scopes, err
}

// ResolveOAuthToken authenticates and records the grant's use in one statement.
func (store *Store) ResolveOAuthToken(ctx context.Context, tokenHash string, now time.Time) (oauthservice.Access, error) {
	row, err := store.queries.ResolveOAuthToken(ctx, pgdb.ResolveOAuthTokenParams{Now: millisecond(now), SecretHash: tokenHash})
	if errors.Is(err, pgx.ErrNoRows) {
		return oauthservice.Access{}, oauthservice.ErrInvalidToken
	}
	if err != nil {
		return oauthservice.Access{}, err
	}
	return oauthservice.Access{AccountID: row.UserID, AccountEmail: row.Email, Client: oauthservice.Client{ID: row.ClientID, Name: row.ClientName, Verified: row.Verified},
		Scopes: row.Scopes, Resource: row.Resource, ExpiresAt: millisecond(row.ExpiresAt)}, nil
}

func (store *Store) RevokeOAuthToken(ctx context.Context, tokenHash, clientID string) error {
	return store.queries.RevokeOAuthToken(ctx, pgdb.RevokeOAuthTokenParams{SecretHash: tokenHash, ClientID: clientID})
}

func (store *Store) OAuthGrants(ctx context.Context, accountID string) ([]oauthservice.Grant, error) {
	rows, err := store.queries.OAuthGrants(ctx, accountID)
	if err != nil {
		return nil, err
	}
	grants := make([]oauthservice.Grant, 0, len(rows))
	for _, row := range rows {
		grant := oauthservice.Grant{ID: row.ID, Client: oauthservice.Client{ID: row.ClientID, Name: row.ClientName, Verified: row.Verified},
			Scopes: slices.Clone(row.Scopes), CreatedAt: millisecond(row.CreatedAt)}
		if row.LastUsedAt != nil {
			last := millisecond(*row.LastUsedAt)
			grant.LastUsedAt = &last
		}
		grants = append(grants, grant)
	}
	return grants, nil
}

// DeleteOAuthGrant cascades to every token issued under the grant.
func (store *Store) DeleteOAuthGrant(ctx context.Context, accountID, grantID string) error {
	count, err := store.queries.DeleteOAuthGrant(ctx, pgdb.DeleteOAuthGrantParams{UserID: accountID, ID: grantID})
	if err != nil {
		return err
	}
	if count != 1 {
		return oauthservice.ErrNotFound
	}
	return nil
}
