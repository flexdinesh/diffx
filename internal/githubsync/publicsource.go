package githubsync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/flexdinesh/diffx/internal/github"
)

// PublicSource names a credential public pull request previews may read
// GitHub with. The service tries its sources in order; see readPublic.
type PublicSource string

const (
	// SourceRepository reads through the app's installation on the
	// repository, spending that installation's rate limit. It is skipped
	// when the app is not installed there.
	SourceRepository PublicSource = "repository"
	// SourcePublic reads through the public installation.
	SourcePublic PublicSource = "public"
	// SourceViewer reads with the requesting browser session's GitHub
	// connection. It is skipped without one, and the fetcher, which never
	// holds a user token, never uses it.
	SourceViewer PublicSource = "viewer"
	// SourceAnonymous reads without credentials, within GitHub's per-IP
	// limit for unauthenticated requests.
	SourceAnonymous PublicSource = "anonymous"
)

// Remedies a viewer can take when GitHub refused every source.
const (
	RemedySignIn        = "sign_in"
	RemedyConnectGitHub = "connect_github"
)

// DefaultPublicReadSources is the app's read policy: the installations that
// spend the app's rate limits first, then the viewer's own access, then
// anonymous reads, which share one small per-IP limit.
func DefaultPublicReadSources() []PublicSource {
	return []PublicSource{SourceRepository, SourcePublic, SourceViewer, SourceAnonymous}
}

// publicReadSources validates a read order; nil is the default order.
func publicReadSources(sources []PublicSource) ([]PublicSource, error) {
	if sources == nil {
		return DefaultPublicReadSources(), nil
	}
	if len(sources) == 0 {
		return nil, errors.New("public read sources must not be empty")
	}
	seen := make(map[PublicSource]bool, len(sources))
	for _, source := range sources {
		switch source {
		case SourceRepository, SourcePublic, SourceViewer, SourceAnonymous:
		default:
			return nil, fmt.Errorf("unknown public read source %q", source)
		}
		if seen[source] {
			return nil, fmt.Errorf("public read source %q repeats", source)
		}
		seen[source] = true
	}
	return slices.Clone(sources), nil
}

// publicRead is the source that served a public read and, for installation
// sources, the installation it read through.
type publicRead struct {
	source       PublicSource
	installation int64
}

// restrictedError reports that GitHub refused every applicable source.
// Remedy is what the viewer can do about it; nil when nothing.
type restrictedError struct {
	remedy   *string
	refusals []string
}

func (err *restrictedError) Error() string {
	if len(err.refusals) == 0 {
		return "no GitHub read source applies"
	}
	return "GitHub refused every read source: " + strings.Join(err.refusals, "; ")
}

// readPublic reads a public repository through the first source, in the
// service's order, that serves it; viewer is the requesting account, or
// empty. A source that does not apply is skipped. A refused credential
// (fallsBack) moves on to the next source. A 404 ends the chain, except at
// the repository source, whose installation's reach may be stale; after
// any 404 the viewer source is skipped, so a viewer's access never reads a
// repository the other sources cannot. Other errors end the chain. When
// every source is refused, a rate limit is returned as is, and otherwise a
// *restrictedError naming the viewer's remedy.
func (s *Service) readPublic(ctx context.Context, fullName, viewer string, read func(github.Credential) error) (publicRead, error) {
	var refusals []string
	var limited error
	var remedy *string
	missing := false
	for _, source := range s.sources {
		var credential github.Credential
		served := publicRead{source: source}
		switch source {
		case SourceRepository:
			served.installation = s.repositoryInstallation(ctx, fullName)
			if served.installation == 0 {
				continue
			}
			credential = github.Installation(served.installation)
		case SourcePublic:
			served.installation = s.public
			credential = github.Installation(s.public)
		case SourceViewer:
			if missing {
				continue
			}
			if viewer == "" {
				remedy = remedyOf(RemedySignIn)
				continue
			}
			token, err := s.accessToken(ctx, viewer)
			if errors.Is(err, ErrNotConnected) || errors.Is(err, ErrReconnect) {
				remedy = remedyOf(RemedyConnectGitHub)
				continue
			}
			if err != nil {
				log.Printf("githubsync: public read of %s skips the viewer's GitHub connection: %v", fullName, err)
				continue
			}
			credential = github.UserAccess(token)
		case SourceAnonymous:
			credential = github.Anonymous()
		}
		err := read(credential)
		switch {
		case err == nil:
			return served, nil
		case fallsBack(err):
		case notFound(err) && source == SourceRepository:
			missing = true
		default:
			return served, err
		}
		if source == SourceRepository {
			s.forgetInstallation(fullName)
		}
		var status *github.Error
		if errors.As(err, &status) && status.Status == http.StatusTooManyRequests {
			limited = err
		}
		log.Printf("githubsync: public read of %s through the %s source failed, trying the next: %v", fullName, source, err)
		refusals = append(refusals, fmt.Sprintf("%s: %v", source, err))
	}
	if limited != nil {
		return publicRead{}, limited
	}
	return publicRead{}, &restrictedError{remedy: remedy, refusals: refusals}
}

// fallsBack reports a refusal of the credential rather than of the read:
// it could not be obtained, or GitHub rejected, refused or rate limited it.
func fallsBack(err error) bool {
	var status *github.Error
	return errors.Is(err, github.ErrCredentialUnavailable) || errors.Is(err, github.ErrUnauthorized) ||
		errors.Is(err, github.ErrRestricted) || errors.As(err, &status) && status.Status == http.StatusTooManyRequests
}

func remedyOf(remedy string) *string { return &remedy }
