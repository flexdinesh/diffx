// Package caller carries the authenticated caller of a request from the
// composition that authenticates it to transports that attribute writes.
package caller

import "context"

const (
	ScopeReviewsRead   = "reviews:read"
	ScopeCommentsWrite = "comments:write"
)

// Kind names how a request was authenticated.
type Kind string

const (
	KindLocal    Kind = "local"
	KindSession  Kind = "session"
	KindAPIToken Kind = "api-token"
	KindOAuth    Kind = "oauth"
)

// Caller is who made a request. Name labels it: an API token's name or an
// OAuth client's name. Verified reports whether Name was verified rather
// than asserted by the client. Scopes limit only OAuth callers.
type Caller struct {
	Kind     Kind
	Name     string
	Verified bool
	ClientID string
	Scopes   []string
}

// Allows reports whether the caller may use an operation needing scope.
func (c Caller) Allows(scope string) bool {
	if c.Kind != KindOAuth {
		return true
	}
	for _, granted := range c.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

type key struct{}

// With returns ctx carrying c.
func With(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, key{}, c)
}

// From returns the caller carried by ctx.
func From(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(key{}).(Caller)
	return c, ok
}
