package identityservice

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/google"
)

const (
	InviteLifetime     = 48 * time.Hour
	GoogleFlowLifetime = 10 * time.Minute
)

var (
	// ErrGoogleDisabled reports Google sign-in is not configured.
	ErrGoogleDisabled = errors.New("google sign-in is not configured")
	// ErrNoAccount reports a Google account without a diffx account or invite
	// while sign-up is closed.
	ErrNoAccount = errors.New("no account for this Google account")
	// ErrInviteMismatch reports a verified Google email that differs from the invite.
	ErrInviteMismatch = errors.New("google account does not match the invite")
)

// GoogleClient is the Google wire adapter this service drives.
type GoogleClient interface {
	AuthorizeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, nonce string) (google.Identity, error)
}

type Option func(*Service)

// WithGoogle enables Google sign-in.
func WithGoogle(client GoogleClient) Option {
	return func(service *Service) { service.google = client }
}

type InviteStatus string

const (
	InvitePending InviteStatus = "pending"
	InviteUsed    InviteStatus = "used"
	InviteExpired InviteStatus = "expired"
	InviteRevoked InviteStatus = "revoked"
)

// Invite admits one email to the web app as a member.
type Invite struct {
	ID        string
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	// UsedBy is the username that accepted the invite while that account exists.
	UsedBy    string
	RevokedAt *time.Time
}

func (invite Invite) Status(now time.Time) InviteStatus {
	switch {
	case invite.UsedAt != nil:
		return InviteUsed
	case invite.RevokedAt != nil:
		return InviteRevoked
	case !now.Before(invite.ExpiresAt):
		return InviteExpired
	}
	return InvitePending
}

// GoogleFlow is one started authorization, bound to the starting browser. It
// may accept an invite or complete first-admin setup.
type GoogleFlow struct {
	BindingHash string
	Nonce       string
	Verifier    string
	InviteID    string
	SetupHash   string
	ExpiresAt   time.Time
}

// GoogleStore persists Google identities, invites and authorization flows.
type GoogleStore interface {
	// CreateInvite revokes the email's pending invites in the same transaction;
	// an email linked to an account is ErrConflict.
	CreateInvite(ctx context.Context, invite Invite, tokenHash string) error
	Invites(context.Context) ([]Invite, error)
	Invite(ctx context.Context, id string) (Invite, error)
	RevokeInvite(ctx context.Context, id string, at time.Time) error
	// PendingInvite resolves an unexpired, unused, unrevoked invite by its secret.
	PendingInvite(ctx context.Context, tokenHash string, now time.Time) (Invite, error)
	// AcceptInvite creates the account and its Google identity and uses the
	// pending invite in one transaction. A taken username is ErrConflict; an
	// unavailable invite or an already linked subject is ErrNotFound.
	AcceptInvite(ctx context.Context, inviteID string, account Account, identity google.Identity, now time.Time) error
	// AcceptPasswordInvite creates a password account and uses the pending
	// invite in one transaction, refusing with ErrDisabled unless every setting
	// required is on. A taken username is ErrConflict; an unavailable invite is ErrNotFound.
	AcceptPasswordInvite(ctx context.Context, inviteID string, account Account, passwordHash string, now time.Time, required Settings) error
	// CreateGoogleAccount creates an account and its Google identity without an
	// invite, refusing with ErrDisabled unless every setting required is on. A
	// taken username is ErrConflict; an already linked subject is ErrNotFound.
	CreateGoogleAccount(ctx context.Context, account Account, identity google.Identity, now time.Time, required Settings) error
	// CreateGoogleAdmin consumes the setup secret and creates the first admin
	// with its Google identity in one transaction. A taken username is
	// ErrConflict; a missing, expired or used secret or a linked subject is ErrNotFound.
	CreateGoogleAdmin(ctx context.Context, account Account, identity google.Identity, setupHash string, now time.Time) error
	// PromoteSetupAdmin consumes the setup secret and makes an existing,
	// enabled account the first admin; otherwise it is ErrNotFound.
	PromoteSetupAdmin(ctx context.Context, accountID, setupHash string, now time.Time) error
	// GoogleAccount resolves a linked subject, recording the latest email.
	GoogleAccount(ctx context.Context, identity google.Identity, now time.Time) (account Account, disabled bool, err error)
	// GoogleEmail is the latest email Google reported for the account; "" for
	// accounts without a Google identity.
	GoogleEmail(ctx context.Context, accountID string) (string, error)
	PutGoogleFlow(ctx context.Context, stateHash string, flow GoogleFlow) error
	// TakeGoogleFlow consumes an unexpired flow exactly once.
	TakeGoogleFlow(ctx context.Context, stateHash string, now time.Time) (GoogleFlow, error)
}

func (service *Service) GoogleEnabled() bool { return service.google != nil }

// GoogleEmail is the account's latest Google email, or "" for password accounts.
func (service *Service) GoogleEmail(ctx context.Context, accountID string) (string, error) {
	return service.store.GoogleEmail(ctx, accountID)
}

func canonicalEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || !strings.Contains(domain, ".") || len(email) > 254 || strings.ContainsAny(email, " \t\r\n<>\"") || strings.Contains(domain, "@") {
		return "", fmt.Errorf("%w: email address is invalid", ErrInvalidInput)
	}
	return email, nil
}

// CreateInvite issues a single-use 48 hour member invite for one email,
// replacing any pending invite for it. The secret is returned once and stored
// hashed. Invites work whether or not signup is open; the settings decide
// whether a password can accept it.
func (service *Service) CreateInvite(ctx context.Context, email string) (Invite, string, error) {
	email, err := canonicalEmail(email)
	if err != nil {
		return Invite{}, "", err
	}
	id, err := opaqueSecret()
	if err != nil {
		return Invite{}, "", err
	}
	secret, err := opaqueSecret()
	if err != nil {
		return Invite{}, "", err
	}
	now := service.now()
	invite := Invite{ID: id, Email: email, CreatedAt: now, ExpiresAt: now.Add(InviteLifetime)}
	if err := service.store.CreateInvite(ctx, invite, credentialHash(secret)); err != nil {
		return Invite{}, "", err
	}
	return invite, secret, nil
}

func (service *Service) Invites(ctx context.Context) ([]Invite, error) {
	return service.store.Invites(ctx)
}

func (service *Service) RevokeInvite(ctx context.Context, id string) error {
	return service.store.RevokeInvite(ctx, id, service.now())
}

// PendingInvite resolves an invite link for the invite page.
func (service *Service) PendingInvite(ctx context.Context, secret string) (Invite, error) {
	if len(secret) != 43 {
		return Invite{}, ErrNotFound
	}
	return service.store.PendingInvite(ctx, credentialHash(secret), service.now())
}

// StartGoogle begins Google authorization, optionally for an invite. It returns
// the authorization URL and a browser binding the callback must present.
func (service *Service) StartGoogle(ctx context.Context, inviteSecret string) (string, string, error) {
	if service.google == nil {
		return "", "", ErrGoogleDisabled
	}
	var flow GoogleFlow
	if inviteSecret != "" {
		invite, err := service.PendingInvite(ctx, inviteSecret)
		if err != nil {
			return "", "", err
		}
		flow.InviteID = invite.ID
	}
	return service.startGoogle(ctx, flow)
}

// StartGoogleSetup begins Google authorization that completes first-admin
// setup with a pending setup secret; others are ErrNotFound.
func (service *Service) StartGoogleSetup(ctx context.Context, setupSecret string) (string, string, error) {
	if service.google == nil {
		return "", "", ErrGoogleDisabled
	}
	if len(setupSecret) != 43 {
		return "", "", ErrNotFound
	}
	setupHash := credentialHash(setupSecret)
	pending, err := service.store.PendingAdminSetup(ctx, setupHash, service.now())
	if err != nil {
		return "", "", err
	}
	if !pending {
		return "", "", ErrNotFound
	}
	return service.startGoogle(ctx, GoogleFlow{SetupHash: setupHash})
}

func (service *Service) startGoogle(ctx context.Context, flow GoogleFlow) (string, string, error) {
	secrets := make([]string, 4)
	for index := range secrets {
		secret, err := opaqueSecret()
		if err != nil {
			return "", "", err
		}
		secrets[index] = secret
	}
	state, binding, nonce, verifier := secrets[0], secrets[1], secrets[2], secrets[3]
	flow.BindingHash, flow.Nonce, flow.Verifier, flow.ExpiresAt = credentialHash(binding), nonce, verifier, service.now().Add(GoogleFlowLifetime)
	if err := service.store.PutGoogleFlow(ctx, credentialHash(state), flow); err != nil {
		return "", "", err
	}
	return service.google.AuthorizeURL(state, nonce, verifier), binding, nil
}

// CompleteGoogle finishes authorization in the browser that started it. A
// linked Google subject signs in to an audience that admits it, keeping its
// account when the email changes. For the web app, an invite flow creates the
// account when Google verified the invited email, and open signup creates one
// for any verified email. The console creates only the first admin, from a
// setup flow.
func (service *Service) CompleteGoogle(ctx context.Context, audience Audience, state, binding, code string) (Session, error) {
	if service.google == nil {
		return Session{}, ErrGoogleDisabled
	}
	if len(state) != 43 {
		return Session{}, ErrAuthentication
	}
	flow, err := service.store.TakeGoogleFlow(ctx, credentialHash(state), service.now())
	if errors.Is(err, ErrNotFound) || err == nil && subtle.ConstantTimeCompare([]byte(flow.BindingHash), []byte(credentialHash(binding))) != 1 {
		return Session{}, ErrAuthentication
	}
	if err != nil {
		return Session{}, err
	}
	identity, err := service.google.Exchange(ctx, code, flow.Verifier, flow.Nonce)
	if errors.Is(err, google.ErrRejected) {
		return Session{}, ErrAuthentication
	}
	if err != nil {
		return Session{}, err
	}
	account, disabled, err := service.store.GoogleAccount(ctx, identity, service.now())
	if flow.SetupHash != "" {
		if audience != AudienceConsole {
			return Session{}, ErrAuthentication
		}
		return service.completeGoogleSetup(ctx, flow.SetupHash, identity, account, disabled, err)
	}
	if err == nil {
		if disabled || !service.admits(audience, account) {
			return Session{}, ErrAuthentication
		}
		return service.issueSession(ctx, account, "")
	}
	if !errors.Is(err, ErrNotFound) {
		return Session{}, err
	}
	if audience == AudienceConsole {
		return Session{}, ErrNoAccount
	}
	if flow.InviteID != "" {
		return service.acceptInvite(ctx, flow.InviteID, identity)
	}
	required := Settings{Signup: true}
	if !service.Settings().allows(required) {
		return Session{}, ErrNoAccount
	}
	// Open signup still requires an email Google verified, which names the account.
	email, err := canonicalEmail(identity.Email)
	if err != nil || !identity.EmailVerified {
		return Session{}, ErrAuthentication
	}
	identity.Email = email
	created, err := service.createNamedAccount(email, Account{Method: MethodGoogle}, func(account Account) error {
		return service.store.CreateGoogleAccount(ctx, account, identity, service.now(), required)
	})
	if errors.Is(err, ErrDisabled) {
		return Session{}, ErrNoAccount
	}
	if err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, created, "")
}

// completeGoogleSetup consumes the setup secret for a Google identity. A
// linked, enabled account becomes the first admin; otherwise a new admin is
// named after the email Google verified.
func (service *Service) completeGoogleSetup(ctx context.Context, setupHash string, identity google.Identity, linked Account, disabled bool, lookup error) (Session, error) {
	if lookup == nil {
		if disabled {
			return Session{}, ErrAuthentication
		}
		if err := service.store.PromoteSetupAdmin(ctx, linked.ID, setupHash, service.now()); err != nil {
			return Session{}, err
		}
		linked.Admin = true
		return service.issueSession(ctx, linked, "")
	}
	if !errors.Is(lookup, ErrNotFound) {
		return Session{}, lookup
	}
	email, err := canonicalEmail(identity.Email)
	if err != nil || !identity.EmailVerified {
		return Session{}, ErrAuthentication
	}
	identity.Email = email
	created, err := service.createNamedAccount(email, Account{Admin: true, Method: MethodGoogle}, func(account Account) error {
		return service.store.CreateGoogleAdmin(ctx, account, identity, setupHash, service.now())
	})
	if err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, created, "")
}

// AcceptPasswordInvite creates a password account from an invite link and
// signs it in while password signup is on. Holding the link is the proof; the
// invited email is not verified.
func (service *Service) AcceptPasswordInvite(ctx context.Context, secret, username, password string) (Session, error) {
	required := Settings{PasswordSignup: true}
	if !service.Settings().allows(required) {
		return Session{}, ErrDisabled
	}
	account, hash, err := service.newPasswordAccount(username, password)
	if err != nil {
		return Session{}, err
	}
	invite, err := service.PendingInvite(ctx, secret)
	if err != nil {
		return Session{}, err
	}
	if err := service.store.AcceptPasswordInvite(ctx, invite.ID, account, hash, service.now(), required); err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, account, hash)
}

func (service *Service) acceptInvite(ctx context.Context, inviteID string, identity google.Identity) (Session, error) {
	invite, err := service.store.Invite(ctx, inviteID)
	if err != nil {
		return Session{}, err
	}
	if invite.Status(service.now()) != InvitePending {
		return Session{}, ErrNotFound
	}
	email, err := canonicalEmail(identity.Email)
	if err != nil || !identity.EmailVerified || email != invite.Email {
		return Session{}, ErrInviteMismatch
	}
	identity.Email = email
	account, err := service.createNamedAccount(email, Account{Method: MethodGoogle}, func(account Account) error {
		return service.store.AcceptInvite(ctx, inviteID, account, identity, service.now())
	})
	if err != nil {
		return Session{}, err
	}
	return service.issueSession(ctx, account, "")
}

// createNamedAccount creates an account like template, named after the email
// and numbered until the name is free.
func (service *Service) createNamedAccount(email string, template Account, create func(Account) error) (Account, error) {
	id, err := opaqueSecret()
	if err != nil {
		return Account{}, err
	}
	base := usernameFromEmail(email)
	for attempt := 1; attempt <= 100; attempt++ {
		username := base
		if attempt > 1 {
			suffix := fmt.Sprintf("-%d", attempt)
			username = strings.TrimRight(base[:min(len(base), 32-len(suffix))], "._-") + suffix
		}
		account := template
		account.ID, account.Username = id, username
		err := create(account)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return Account{}, err
		}
		return account, nil
	}
	return Account{}, ErrConflict
}

// usernameFromEmail derives a valid username from the email's local part.
func usernameFromEmail(email string) string {
	local, _, _ := strings.Cut(email, "@")
	var name strings.Builder
	for _, character := range strings.ToLower(local) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || name.Len() > 0 && (character == '.' || character == '-' || character == '_') {
			name.WriteRune(character)
		}
	}
	username := strings.TrimRight(name.String()[:min(name.Len(), 32)], "._-")
	if username == "" {
		return "user"
	}
	if len(username) < 3 {
		username += "-user"
	}
	return username
}
