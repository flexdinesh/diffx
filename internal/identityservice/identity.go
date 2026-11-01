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
	"sync/atomic"
	"time"
)

var (
	ErrAuthentication = errors.New("invalid credentials")
	ErrInvalidInput   = errors.New("invalid identity input")
	ErrInvalidEmail   = fmt.Errorf("%w: invalid email address", ErrInvalidInput)
	ErrConflict       = errors.New("email already has an account")
	ErrNotFound       = errors.New("identity not found")
	// ErrForbidden refuses an administrative action an admin takes on itself.
	ErrForbidden = errors.New("identity operation not permitted")
	// ErrLastAdmin refuses an action that would leave no enabled admin.
	ErrLastAdmin = errors.New("the last admin cannot be removed")
	// ErrDisabled reports a sign-up or sign-in method this deployment turned off.
	ErrDisabled = errors.New("identity method is disabled")
)

type Purpose string

const (
	PurposeCollector      Purpose = "collector"
	PurposeAPI            Purpose = "api"
	SessionLifetime               = 7 * 24 * time.Hour
	DefaultTokenLifetime          = 90 * 24 * time.Hour
	MaximumTokenLifetime          = 365 * 24 * time.Hour
	SetupLifetime                 = 24 * time.Hour
	PasswordResetLifetime         = 24 * time.Hour
)

// Method is how an account signs in. It is fixed when the account is created:
// an account with a Google identity uses Google, every other one a password.
type Method string

const (
	MethodPassword Method = "password"
	MethodGoogle   Method = "google"
)

// Audience is the surface a credential is presented to. Every account is a
// member of the web app and its APIs; admins also use the console.
type Audience string

const (
	AudienceMembers Audience = "members"
	AudienceConsole Audience = "console"
)

// Settings decide how people join the web app; admins change them in the
// console. They never affect existing accounts or the console.
type Settings struct {
	// Signup lets people create accounts without an invite.
	Signup bool
	// PasswordSignup lets new accounts choose a password, by signup or invite.
	// Existing password accounts always sign in, whatever its value.
	PasswordSignup bool
}

// allows reports whether every setting required is on.
func (settings Settings) allows(required Settings) bool {
	return (settings.Signup || !required.Signup) && (settings.PasswordSignup || !required.PasswordSignup)
}

// Account is an authenticated owner. Admin marks accounts that may also sign
// in to the console and manage other accounts.
type Account struct {
	ID string
	// Email identifies the account: the canonical password sign-in email, or
	// the latest one Google reported for a Google account.
	Email  string
	Admin  bool
	Method Method
}

// AccountSummary is the administrative view of an account.
type AccountSummary struct {
	Account
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
	// reports whether it was stored. Setup creates the first admin only.
	PutAdminSetup(context.Context, string, time.Time) (bool, error)
	AdminExists(context.Context) (bool, error)
	// CreateAdmin consumes the unexpired setup secret and creates the admin in
	// one transaction; a missing, expired or used secret is ErrNotFound.
	CreateAdmin(ctx context.Context, account Account, passwordHash, setupHash string, now time.Time) error
	// PendingAdminSetup reports whether the setup secret is unexpired while no admin exists.
	PendingAdminSetup(ctx context.Context, setupHash string, now time.Time) (bool, error)
	// CreateAccount commits a member and its password hash; a taken email
	// is ErrConflict. It reads the settings in its transaction and refuses with
	// ErrDisabled unless every setting required is on.
	CreateAccount(ctx context.Context, account Account, passwordHash string, created time.Time, required Settings) error
	AppSettings(context.Context) (Settings, error)
	// PutAppSettings replaces the settings and notifies every replica at commit.
	PutAppSettings(ctx context.Context, settings Settings, actorID string, at time.Time) error
	AccountSummaries(context.Context) ([]AccountSummary, error)
	// SetAccountDisabled revokes sessions and tokens when disabling. It,
	// DeleteAccount and SetAdmin refuse with ErrLastAdmin when the change would
	// leave no enabled admin, deciding atomically with concurrent changes.
	SetAccountDisabled(ctx context.Context, accountID string, disabled bool, at time.Time) error
	DeleteAccount(context.Context, string) error
	SetAdmin(ctx context.Context, accountID string, admin bool) error
	// AccountPassword resolves an enabled account by its canonical email.
	AccountPassword(context.Context, string) (PasswordCredential, error)
	AccountPasswordByID(context.Context, string) (PasswordCredential, error)
	ReplacePassword(context.Context, string, string, string) error
	// PutPasswordReset replaces the account's pending reset secret.
	PutPasswordReset(ctx context.Context, accountID, tokenHash string, expires time.Time) error
	// PasswordReset resolves an unexpired reset of an enabled account to its credential.
	PasswordReset(ctx context.Context, tokenHash string, now time.Time) (PasswordCredential, error)
	// CompletePasswordReset consumes the unexpired reset, replaces the password
	// fenced on the expected hash and revokes browser sessions in one
	// transaction; API tokens are independent. A missing reset is ErrNotFound.
	CompletePasswordReset(ctx context.Context, tokenHash, expected, replacement string, now time.Time) error
	CreateSession(context.Context, string, string, string, string, time.Time) error
	ResolveSession(context.Context, string, time.Time) (Session, error)
	DeleteSession(context.Context, string) error
	CreateIdentityToken(context.Context, string, Token, string) error
	IdentityTokens(context.Context, string) ([]Token, error)
	DeleteIdentityToken(context.Context, string, string) error
	ResolveIdentityToken(context.Context, string, time.Time) (TokenIdentity, error)
	IdentityAccounts(context.Context) ([]Account, error)
	GoogleStore
}

type Service struct {
	store     Store
	google    GoogleClient
	settings  atomic.Pointer[Settings]
	now       func() time.Time
	dummyHash string
}

// LoadSettings reads the settings and keeps them as this process's snapshot.
func (service *Service) LoadSettings(ctx context.Context) (Settings, error) {
	settings, err := service.store.AppSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	service.settings.Store(&settings)
	return settings, nil
}

// Settings is the latest loaded snapshot; before the first load every
// setting is off. Account creation re-checks the stored settings.
func (service *Service) Settings() Settings {
	if settings := service.settings.Load(); settings != nil {
		return *settings
	}
	return Settings{}
}

// PutSettings is an admin's change to the settings.
func (service *Service) PutSettings(ctx context.Context, actorID string, settings Settings) error {
	if err := service.store.PutAppSettings(ctx, settings, actorID, service.now()); err != nil {
		return err
	}
	service.settings.Store(&settings)
	return nil
}

// admits reports whether an account may authenticate to an audience: the
// console admits admins by any method; the web app admits every account.
func (service *Service) admits(audience Audience, account Account) bool {
	return audience != AudienceConsole || account.Admin
}

func New(store Store, options ...Option) *Service {
	// A well-formed fixed dummy verifier pays the same Argon2 cost on misses.
	const dummy = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	service := &Service{store: store, now: time.Now, dummyHash: dummy}
	for _, option := range options {
		option(service)
	}
	return service
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 128 {
		return fmt.Errorf("%w: password must be 8–128 bytes", ErrInvalidInput)
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
func (service *Service) CompleteSetup(ctx context.Context, secret, email, password string) (Session, error) {
	account, hash, err := service.newPasswordAccount(email, password)
	if err != nil {
		return Session{}, err
	}
	if len(secret) != 43 {
		return Session{}, ErrNotFound
	}
	account.Admin = true
	if err := service.store.CreateAdmin(ctx, account, hash, credentialHash(secret), service.now()); err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, account, hash)
}

// Login signs a password account in to an audience it is admitted to. Every
// failure is the same ErrAuthentication, so it never reveals which accounts
// exist, use Google or belong to another audience.
func (service *Service) Login(ctx context.Context, audience Audience, email, password string) (Session, error) {
	email, invalid := canonicalEmail(email)
	if len(password) > 128 {
		return Session{}, ErrAuthentication
	}
	credential, err := service.store.AccountPassword(ctx, email)
	if invalid != nil || errors.Is(err, ErrNotFound) {
		verifyPassword(service.dummyHash, password)
		return Session{}, ErrAuthentication
	}
	if err != nil {
		return Session{}, err
	}
	if !verifyPassword(credential.Hash, password) || !service.admits(audience, credential.Account) {
		return Session{}, ErrAuthentication
	}
	return service.issueSession(ctx, credential.Account, credential.Hash)
}

// SignUp creates a password member and signs it in, while both signup and
// password signup are on.
func (service *Service) SignUp(ctx context.Context, email, password string) (Session, error) {
	required := Settings{Signup: true, PasswordSignup: true}
	if !service.Settings().allows(required) {
		return Session{}, ErrDisabled
	}
	account, hash, err := service.newPasswordAccount(email, password)
	if err != nil {
		return Session{}, err
	}
	if err := service.store.CreateAccount(ctx, account, hash, service.now(), required); err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, account, hash)
}

// newPasswordAccount requires an email, which identifies the account.
// Collecting an email does not verify ownership of its mailbox.
func (service *Service) newPasswordAccount(email, password string) (Account, string, error) {
	email, err := canonicalEmail(email)
	if err != nil {
		return Account{}, "", err
	}
	if err := validatePassword(password); err != nil {
		return Account{}, "", err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, "", err
	}
	id, err := opaqueSecret()
	if err != nil {
		return Account{}, "", err
	}
	return Account{ID: id, Email: email, Method: MethodPassword}, hash, nil
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

// AuthenticateSession resolves a browser session for an audience; sessions of
// accounts the audience does not admit fail like unknown ones.
func (service *Service) AuthenticateSession(ctx context.Context, audience Audience, secret string) (Session, error) {
	if len(secret) != 43 {
		return Session{}, ErrAuthentication
	}
	session, err := service.store.ResolveSession(ctx, credentialHash(secret), service.now())
	if err == nil && !service.admits(audience, session.Account) {
		return Session{}, ErrAuthentication
	}
	return session, err
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
	if len(current) > 128 || credential.Account.Method != MethodPassword || !verifyPassword(credential.Hash, current) {
		return ErrAuthentication
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return service.store.ReplacePassword(ctx, accountID, credential.Hash, hash)
}

// ResetPassword issues a one-time reset secret for a password account,
// replacing its pending one. It returns the account so the caller links the
// secret to the surface the account signs in to. Google accounts are ErrNotFound.
func (service *Service) ResetPassword(ctx context.Context, accountID string) (Account, string, error) {
	credential, err := service.store.AccountPasswordByID(ctx, accountID)
	if err != nil {
		return Account{}, "", err
	}
	return service.issuePasswordReset(ctx, credential.Account)
}

// ResetPasswordByEmail is ResetPassword for offline recovery by email.
func (service *Service) ResetPasswordByEmail(ctx context.Context, email string) (Account, string, error) {
	email, err := canonicalEmail(email)
	if err != nil {
		return Account{}, "", err
	}
	credential, err := service.store.AccountPassword(ctx, email)
	if err != nil {
		return Account{}, "", err
	}
	return service.issuePasswordReset(ctx, credential.Account)
}

func (service *Service) issuePasswordReset(ctx context.Context, account Account) (Account, string, error) {
	if account.Method != MethodPassword {
		return Account{}, "", ErrNotFound
	}
	secret, err := opaqueSecret()
	if err != nil {
		return Account{}, "", err
	}
	if err := service.store.PutPasswordReset(ctx, account.ID, credentialHash(secret), service.now().Add(PasswordResetLifetime)); err != nil {
		return Account{}, "", err
	}
	return account, secret, nil
}

// PendingPasswordReset resolves a reset link for an audience's reset page.
func (service *Service) PendingPasswordReset(ctx context.Context, audience Audience, secret string) (Account, error) {
	credential, err := service.passwordReset(ctx, audience, secret)
	return credential.Account, err
}

// CompletePasswordReset sets a new password from a reset link and revokes the
// account's browser sessions; its API tokens are unaffected.
func (service *Service) CompletePasswordReset(ctx context.Context, audience Audience, secret, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	credential, err := service.passwordReset(ctx, audience, secret)
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return service.store.CompletePasswordReset(ctx, credentialHash(secret), credential.Hash, hash, service.now())
}

// passwordReset resolves a pending reset; links for another audience's
// accounts are ErrNotFound, like unknown ones.
func (service *Service) passwordReset(ctx context.Context, audience Audience, secret string) (PasswordCredential, error) {
	if len(secret) != 43 {
		return PasswordCredential{}, ErrNotFound
	}
	credential, err := service.store.PasswordReset(ctx, credentialHash(secret), service.now())
	if err != nil {
		return PasswordCredential{}, err
	}
	if credential.Account.Method != MethodPassword || !service.admits(audience, credential.Account) {
		return PasswordCredential{}, ErrNotFound
	}
	return credential, nil
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

// AuthenticateToken resolves an API token, a member credential.
func (service *Service) AuthenticateToken(ctx context.Context, secret string) (TokenIdentity, error) {
	if len(secret) != 43 {
		return TokenIdentity{}, ErrAuthentication
	}
	return service.store.ResolveIdentityToken(ctx, credentialHash(secret), service.now())
}

// Accounts lists enabled members, the owners the ingestion worker may serve.
func (service *Service) Accounts(ctx context.Context) ([]Account, error) {
	return service.store.IdentityAccounts(ctx)
}

func (service *Service) AccountSummaries(ctx context.Context) ([]AccountSummary, error) {
	return service.store.AccountSummaries(ctx)
}

// DisableAccount, EnableAccount, DeleteAccount and SetAdmin are an admin's
// actions on another account; an admin never acts on itself, so it cannot
// lock itself out.
func (service *Service) DisableAccount(ctx context.Context, actorID, accountID string) error {
	if actorID == accountID {
		return ErrForbidden
	}
	return service.store.SetAccountDisabled(ctx, accountID, true, service.now())
}

func (service *Service) EnableAccount(ctx context.Context, actorID, accountID string) error {
	if actorID == accountID {
		return ErrForbidden
	}
	return service.store.SetAccountDisabled(ctx, accountID, false, service.now())
}

// DeleteAccount removes the account and every review, token and integration
// it owns.
func (service *Service) DeleteAccount(ctx context.Context, actorID, accountID string) error {
	if actorID == accountID {
		return ErrForbidden
	}
	return service.store.DeleteAccount(ctx, accountID)
}

// SetAdmin grants or removes the admin role. Removing it ends the account's
// console sessions at their next request.
func (service *Service) SetAdmin(ctx context.Context, actorID, accountID string, admin bool) error {
	if actorID == accountID {
		return ErrForbidden
	}
	return service.store.SetAdmin(ctx, accountID, admin)
}
