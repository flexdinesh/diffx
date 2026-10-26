// Package identityservice owns remote account and credential policies.
package identityservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAuthentication = errors.New("invalid credentials")
	ErrInvalidInput   = errors.New("invalid identity input")
	ErrConflict       = errors.New("username already exists")
	ErrNotFound       = errors.New("identity not found")
	// ErrForbidden refuses an administrative action on the admin account itself.
	ErrForbidden = errors.New("identity operation not permitted")
)

type Purpose string

const (
	PurposeCollector     Purpose = "collector"
	PurposeAPI           Purpose = "api"
	SessionLifetime              = 7 * 24 * time.Hour
	DefaultTokenLifetime         = 90 * 24 * time.Hour
	MaximumTokenLifetime         = 365 * 24 * time.Hour
	SetupLifetime                = 24 * time.Hour
)

// Account is an authenticated owner. Admin marks the single account that
// signs in with a password and manages other accounts.
type Account struct {
	ID       string
	Username string
	Admin    bool
}

// AccountSummary is the administrative view of an account.
type AccountSummary struct {
	Account
	Email     string
	Disabled  bool
	CreatedAt time.Time
}

type PasswordCredential struct {
	Account Account
	Hash    string
}

type Session struct {
	Account   Account
	Secret    string
	CSRFToken string
	ExpiresAt time.Time
}

type Token struct {
	ID         string
	Name       string
	Purpose    Purpose
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
}

type CreatedToken struct {
	Token
	Secret string
}

type TokenIdentity struct {
	Account   Account
	Purpose   Purpose
	ExpiresAt time.Time
}

// Store operations preserve credential races atomically. Password replacement
// revokes sessions in its transaction; session creation compares the verified hash
// and refuses disabled accounts.
type Store interface {
	// PutAdminSetup replaces the pending setup secret while no admin exists and
	// reports whether it was stored.
	PutAdminSetup(context.Context, string, time.Time) (bool, error)
	AdminExists(context.Context) (bool, error)
	// CreateAdmin consumes the unexpired setup secret and creates the admin in
	// one transaction; a missing, expired or used secret is ErrNotFound.
	CreateAdmin(ctx context.Context, account Account, passwordHash, setupHash string, now time.Time) error
	AccountSummaries(context.Context) ([]AccountSummary, error)
	// SetAccountDisabled revokes sessions and tokens when disabling. Both it and
	// DeleteAccount refuse the admin with ErrForbidden.
	SetAccountDisabled(ctx context.Context, accountID string, disabled bool, at time.Time) error
	DeleteAccount(context.Context, string) error
	AccountPassword(context.Context, string) (PasswordCredential, error)
	AccountPasswordByID(context.Context, string) (PasswordCredential, error)
	ReplacePassword(context.Context, string, string, string) error
	CreateSession(context.Context, string, string, string, string, time.Time) error
	ResolveSession(context.Context, string, time.Time) (Session, error)
	DeleteSession(context.Context, string) error
	CreateIdentityToken(context.Context, string, Token, string) error
	IdentityTokens(context.Context, string) ([]Token, error)
	DeleteIdentityToken(context.Context, string, string) error
	ResolveIdentityToken(context.Context, string, time.Time) (TokenIdentity, error)
	IdentityAccounts(context.Context) ([]Account, error)
}

type Service struct {
	store     Store
	now       func() time.Time
	dummyHash string
}

func New(store Store) *Service {
	// A well-formed fixed dummy verifier pays the same Argon2 cost on misses.
	const dummy = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return &Service{store: store, now: time.Now, dummyHash: dummy}
}

func canonicalUsername(username string) (string, error) {
	username = strings.ToLower(username)
	if len(username) < 3 || len(username) > 32 {
		return "", fmt.Errorf("%w: username must be 3–32 characters", ErrInvalidInput)
	}
	for index, character := range username {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || index > 0 && (character == '_' || character == '-' || character == '.') {
			continue
		}
		return "", fmt.Errorf("%w: username must start with a letter or digit and contain letters, digits, dot, dash or underscore", ErrInvalidInput)
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return fmt.Errorf("%w: password must be 12–128 bytes", ErrInvalidInput)
	}
	return nil
}

func opaqueSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func credentialHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

// PrepareSetup issues a one-time admin setup secret while no admin exists,
// replacing any earlier one. It returns "" once an admin exists. The secret is
// stored hashed, so each start issues a new one.
func (service *Service) PrepareSetup(ctx context.Context) (string, error) {
	secret, err := opaqueSecret()
	if err != nil {
		return "", err
	}
	stored, err := service.store.PutAdminSetup(ctx, credentialHash(secret), service.now().Add(SetupLifetime))
	if err != nil || !stored {
		return "", err
	}
	return secret, nil
}

func (service *Service) SetupRequired(ctx context.Context) (bool, error) {
	exists, err := service.store.AdminExists(ctx)
	return !exists, err
}

// CompleteSetup creates the admin from a valid setup secret and signs it in.
func (service *Service) CompleteSetup(ctx context.Context, secret, username, password string) (Session, error) {
	username, err := canonicalUsername(username)
	if err != nil {
		return Session{}, err
	}
	if err := validatePassword(password); err != nil {
		return Session{}, err
	}
	if len(secret) != 43 {
		return Session{}, ErrNotFound
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Session{}, err
	}
	id, err := opaqueSecret()
	if err != nil {
		return Session{}, err
	}
	account := Account{ID: id, Username: username, Admin: true}
	if err := service.store.CreateAdmin(ctx, account, hash, credentialHash(secret), service.now()); err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, account, hash)
}

// Login accepts passwords only for the admin; every other failure is the same
// ErrAuthentication, so it never reveals which accounts exist or use Google.
func (service *Service) Login(ctx context.Context, username, password string) (Session, error) {
	username, invalid := canonicalUsername(username)
	if len(password) > 128 {
		return Session{}, ErrAuthentication
	}
	credential, err := service.store.AccountPassword(ctx, username)
	if invalid != nil || errors.Is(err, ErrNotFound) {
		verifyPassword(service.dummyHash, password)
		return Session{}, ErrAuthentication
	}
	if err != nil {
		return Session{}, err
	}
	if !verifyPassword(credential.Hash, password) || !credential.Account.Admin {
		return Session{}, ErrAuthentication
	}
	return service.issueSession(ctx, credential.Account, credential.Hash)
}

// issueSession fences on the credential the caller verified: a concurrent
// password change or disable makes issuance fail.
func (service *Service) issueSession(ctx context.Context, account Account, expected string) (Session, error) {
	secret, err := opaqueSecret()
	if err != nil {
		return Session{}, err
	}
	csrf, err := opaqueSecret()
	if err != nil {
		return Session{}, err
	}
	expires := service.now().Add(SessionLifetime)
	if err := service.store.CreateSession(ctx, account.ID, expected, credentialHash(secret), csrf, expires); err != nil {
		return Session{}, err
	}
	return Session{Account: account, Secret: secret, CSRFToken: csrf, ExpiresAt: expires}, nil
}

func (service *Service) AuthenticateSession(ctx context.Context, secret string) (Session, error) {
	if len(secret) != 43 {
		return Session{}, ErrAuthentication
	}
	return service.store.ResolveSession(ctx, credentialHash(secret), service.now())
}

func (service *Service) Logout(ctx context.Context, secret string) error {
	return service.store.DeleteSession(ctx, credentialHash(secret))
}

func (service *Service) ChangePassword(ctx context.Context, accountID, current, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	credential, err := service.store.AccountPasswordByID(ctx, accountID)
	if errors.Is(err, ErrNotFound) {
		return ErrAuthentication
	}
	if err != nil {
		return err
	}
	if len(current) > 128 || !verifyPassword(credential.Hash, current) || !credential.Account.Admin {
		return ErrAuthentication
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return service.store.ReplacePassword(ctx, accountID, credential.Hash, hash)
}

// ResetPassword recovers the admin offline; other accounts have no password.
func (service *Service) ResetPassword(ctx context.Context, username, password string) error {
	username, err := canonicalUsername(username)
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	credential, err := service.store.AccountPassword(ctx, username)
	if err != nil {
		return err
	}
	if !credential.Account.Admin {
		return ErrNotFound
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return service.store.ReplacePassword(ctx, credential.Account.ID, credential.Hash, hash)
}

func (service *Service) CreateToken(ctx context.Context, accountID, name string, purpose Purpose, expires time.Time) (CreatedToken, error) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 64 || strings.ContainsAny(name, "\r\n") {
		return CreatedToken{}, fmt.Errorf("%w: token name must be 1–64 bytes", ErrInvalidInput)
	}
	if purpose != PurposeCollector && purpose != PurposeAPI {
		return CreatedToken{}, fmt.Errorf("%w: unknown token purpose", ErrInvalidInput)
	}
	now := service.now()
	if expires.IsZero() {
		expires = now.Add(DefaultTokenLifetime)
	}
	if !expires.After(now) || expires.After(now.Add(MaximumTokenLifetime)) {
		return CreatedToken{}, fmt.Errorf("%w: token expiry must be within 365 days", ErrInvalidInput)
	}
	id, err := opaqueSecret()
	if err != nil {
		return CreatedToken{}, err
	}
	secret, err := opaqueSecret()
	if err != nil {
		return CreatedToken{}, err
	}
	token := Token{ID: id, Name: name, Purpose: purpose, CreatedAt: now, ExpiresAt: expires}
	if err := service.store.CreateIdentityToken(ctx, accountID, token, credentialHash(secret)); err != nil {
		return CreatedToken{}, err
	}
	return CreatedToken{Token: token, Secret: secret}, nil
}

func (service *Service) Tokens(ctx context.Context, accountID string) ([]Token, error) {
	return service.store.IdentityTokens(ctx, accountID)
}

func (service *Service) RevokeToken(ctx context.Context, accountID, tokenID string) error {
	return service.store.DeleteIdentityToken(ctx, accountID, tokenID)
}

func (service *Service) AuthenticateToken(ctx context.Context, secret string) (TokenIdentity, error) {
	if len(secret) != 43 {
		return TokenIdentity{}, ErrAuthentication
	}
	return service.store.ResolveIdentityToken(ctx, credentialHash(secret), service.now())
}

// Accounts lists enabled accounts, the owners the ingestion worker may serve.
func (service *Service) Accounts(ctx context.Context) ([]Account, error) {
	return service.store.IdentityAccounts(ctx)
}

func (service *Service) AccountSummaries(ctx context.Context) ([]AccountSummary, error) {
	return service.store.AccountSummaries(ctx)
}

func (service *Service) DisableAccount(ctx context.Context, accountID string) error {
	return service.store.SetAccountDisabled(ctx, accountID, true, service.now())
}

func (service *Service) EnableAccount(ctx context.Context, accountID string) error {
	return service.store.SetAccountDisabled(ctx, accountID, false, service.now())
}

// DeleteAccount removes the account and every review, token and integration
// it owns.
func (service *Service) DeleteAccount(ctx context.Context, accountID string) error {
	return service.store.DeleteAccount(ctx, accountID)
}
