package githubsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/ingestion"
)

// PublicOwnerID owns every shared public pull request snapshot. It is a
// reserved owner, not an account: it never signs in or receives notifications.
const PublicOwnerID = "github-public"

const (
	// maxPublicRepositoryKB refuses previews of repositories over 500 MB.
	maxPublicRepositoryKB = 500 * 1024
	// publicRefreshInterval makes a refresh within a minute of the last public
	// read of GitHub do nothing.
	publicRefreshInterval = time.Minute
)

// ErrInvalidPullRequest reports a malformed public pull request address.
var ErrInvalidPullRequest = errors.New("invalid GitHub pull request address")

// Public preview statuses and failure codes.
const (
	PublicQueued   = "queued"
	PublicFetching = "fetching"
	PublicReady    = "ready"
	PublicFailed   = "failed"

	FailureNotFound    = "not_found"
	FailureTooLarge    = "too_large"
	FailureFetchFailed = "fetch_failed"
	// FailureRestricted is GitHub refusing every read source, as an
	// organization's IP allow list does; Remedy says what the viewer can do.
	FailureRestricted = "restricted"
)

// PublicPullRequest is a public pull request's shared read-only preview.
// ContextID names the newest ready snapshot, also while a refresh is pending;
// FetchedAt is when that snapshot was fetched.
type PublicPullRequest struct {
	FullName    string                 `json:"fullName"`
	Number      int                    `json:"number"`
	Status      string                 `json:"status"`
	PullRequest *ingestion.PullRequest `json:"pullRequest"`
	ContextID   *string                `json:"contextId"`
	FetchedAt   *int64                 `json:"fetchedAt"`
	Failure     *PublicFailure         `json:"failure"`
}

// PublicFailure explains why the latest request produced no snapshot. Its
// detail never carries internal error text. Remedy, only for
// FailureRestricted, is RemedySignIn or RemedyConnectGitHub when the
// viewer's own GitHub access could still read the pull request.
type PublicFailure struct {
	Code   string  `json:"code"`
	Detail string  `json:"detail"`
	Remedy *string `json:"remedy,omitempty"`
}

var (
	publicOwnerName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	publicRepoName  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// PublicAvailable reports whether the server can read public repositories
// for previews. Whether previews are served is the admins' setting.
func (s *Service) PublicAvailable() bool { return s.public != 0 }

// publicName validates a public pull request address as GitHub names it.
// ErrNotFound when previews are unavailable.
func (s *Service) publicName(owner, repo string, number int) (string, error) {
	if s.public == 0 {
		return "", ErrNotFound
	}
	if len(owner) > 39 || !publicOwnerName.MatchString(owner) || len(repo) > 100 || !publicRepoName.MatchString(repo) ||
		repo == "." || repo == ".." || number <= 0 {
		return "", ErrInvalidPullRequest
	}
	return owner + "/" + repo, nil
}

// PublicStatus reports the public preview of a pull request. ErrNotFound when no
// public request was made, or its snapshot expired with nothing pending.
func (s *Service) PublicStatus(ctx context.Context, owner, repo string, number int) (PublicPullRequest, error) {
	fullName, err := s.publicName(owner, repo, number)
	if err != nil {
		return PublicPullRequest{}, err
	}
	state, err := s.store.PublicPullRequestNamed(ctx, fullName, number, s.millis())
	if err != nil {
		return PublicPullRequest{}, err
	}
	if !state.Demanded || !state.Pending && !inProgress(state.JobState) && state.JobState != "failed" && state.ContextID == "" {
		return PublicPullRequest{}, ErrNotFound
	}
	return publicStatus(state)
}

// EnsurePublic requests a shared preview of a public pull request. Private,
// missing, oversized and restricted repositories are refused in the
// returned failure, which is not persisted. viewer is the requesting
// browser session's account, or empty; see readPublic. A pending refresh or
// an unexpired snapshot is returned as is; otherwise admit decides whether
// this fetch may proceed, and its error is returned unchanged.
func (s *Service) EnsurePublic(ctx context.Context, owner, repo string, number int, viewer string, admit func(context.Context) error) (PublicPullRequest, error) {
	fullName, err := s.publicName(owner, repo, number)
	if err != nil {
		return PublicPullRequest{}, err
	}
	var repository github.Repository
	_, err = s.readPublic(ctx, fullName, viewer, func(credential github.Credential) (err error) {
		repository, err = s.github.Repository(ctx, credential, fullName)
		return err
	})
	var restricted *restrictedError
	if errors.As(err, &restricted) {
		return restrictedRefusal(fullName, number, restricted.remedy), nil
	}
	if notFound(err) {
		return refused(fullName, number, FailureNotFound), nil
	}
	if err != nil {
		return PublicPullRequest{}, fmt.Errorf("read repository %s: %w", fullName, err)
	}
	if repository.Private || repository.Visibility != "" && repository.Visibility != "public" {
		return refused(fullName, number, FailureNotFound), nil
	}
	if repository.Size > maxPublicRepositoryKB {
		return refused(repository.FullName, number, FailureTooLarge), nil
	}
	state, err := s.store.PublicPullRequest(ctx, repository.ID, number, s.millis())
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return PublicPullRequest{}, err
	case state.Pending:
		return s.join(ctx, state, admit)
	case inProgress(state.JobState) || state.ContextID != "":
		return publicStatus(state)
	}
	ref := PullRequestRef{RepositoryID: repository.ID, InstallationID: s.public, FullName: repository.FullName, Number: number}
	// The read precedes admission, so refused pull requests cost no fetch.
	_, raw, err := s.readPublicPull(ctx, ref, viewer)
	if errors.As(err, &restricted) {
		return restrictedRefusal(ref.FullName, number, restricted.remedy), nil
	}
	if notFound(err) {
		return refused(ref.FullName, number, FailureNotFound), nil
	}
	if err != nil {
		return PublicPullRequest{}, err
	}
	if err := admit(ctx); err != nil {
		return PublicPullRequest{}, err
	}
	return s.requestPublic(ctx, ref, raw)
}

// RefreshPublic rereads a previewed pull request and fetches a new snapshot when
// its head changed. It joins a pending refresh and does nothing within a
// minute of the last public read of GitHub. Without a ready snapshot it
// ensures one. viewer is as for EnsurePublic.
func (s *Service) RefreshPublic(ctx context.Context, owner, repo string, number int, viewer string, admit func(context.Context) error) (PublicPullRequest, error) {
	fullName, err := s.publicName(owner, repo, number)
	if err != nil {
		return PublicPullRequest{}, err
	}
	now := s.now()
	state, err := s.store.PublicPullRequestNamed(ctx, fullName, number, now.UnixMilli())
	if err != nil {
		return PublicPullRequest{}, err
	}
	switch {
	case !state.Demanded:
		return PublicPullRequest{}, ErrNotFound
	case state.Pending:
		return s.join(ctx, state, admit)
	case inProgress(state.JobState):
		return publicStatus(state)
	case state.ContextID == "":
		return s.EnsurePublic(ctx, owner, repo, number, viewer, admit)
	case now.Sub(time.UnixMilli(state.CheckedAt)) < publicRefreshInterval:
		return publicStatus(state)
	}
	if err := admit(ctx); err != nil {
		return PublicPullRequest{}, err
	}
	ref := state.Ref
	ref.InstallationID = s.public
	pull, raw, err := s.readPublicPull(ctx, ref, viewer)
	var restricted *restrictedError
	if errors.As(err, &restricted) {
		return restrictedRefusal(ref.FullName, number, restricted.remedy), nil
	}
	if notFound(err) {
		return refused(ref.FullName, number, FailureNotFound), nil
	}
	if err != nil {
		return PublicPullRequest{}, err
	}
	if state.Head == "" || pull.Head.SHA != state.Head {
		return s.requestPublic(ctx, ref, raw)
	}
	if err := s.store.RecordPublicCheck(ctx, ref.RepositoryID, number, raw, s.millis()); err != nil {
		return PublicPullRequest{}, err
	}
	return s.publicState(ctx, ref)
}

// readPublicPull reads the pull request as readPublic does, returning it with
// its stored encoding.
func (s *Service) readPublicPull(ctx context.Context, ref PullRequestRef, viewer string) (github.PullRequest, []byte, error) {
	var pull github.PullRequest
	_, err := s.readPublic(ctx, ref.FullName, viewer, func(credential github.Credential) (err error) {
		pull, err = s.github.PullRequest(ctx, credential, ref.FullName, ref.Number)
		return err
	})
	if err != nil {
		return github.PullRequest{}, nil, fmt.Errorf("read pull request %s#%d: %w", ref.FullName, ref.Number, err)
	}
	raw, err := json.Marshal(pull)
	return pull, raw, err
}

// cachedInstallation is the app's installation on a repository; zero when
// the app is not installed there.
type cachedInstallation struct {
	id    int64
	until time.Time
}

const (
	// installationCacheAge bounds how long a repository's installation is
	// trusted; installation webhooks clear the cache sooner on the replica
	// that receives them.
	installationCacheAge = 10 * time.Minute
	// maxCachedInstallations bounds the cache, which anyone can fill with
	// preview requests; a full cache starts over.
	maxCachedInstallations = 4096
)

// repositoryInstallation returns the app's installation on a repository, or
// zero when it has none or the lookup fails; only answers are cached.
func (s *Service) repositoryInstallation(ctx context.Context, fullName string) int64 {
	key, now := strings.ToLower(fullName), s.now()
	s.installationsMu.Lock()
	cached, ok := s.installations[key]
	s.installationsMu.Unlock()
	if ok && now.Before(cached.until) {
		return cached.id
	}
	installation, err := s.github.RepositoryInstallation(ctx, fullName)
	if notFound(err) {
		installation, err = 0, nil
	}
	if err != nil {
		return 0
	}
	s.installationsMu.Lock()
	if len(s.installations) >= maxCachedInstallations {
		clear(s.installations)
	}
	s.installations[key] = cachedInstallation{id: installation, until: now.Add(installationCacheAge)}
	s.installationsMu.Unlock()
	return installation
}

func (s *Service) forgetInstallation(fullName string) {
	s.installationsMu.Lock()
	delete(s.installations, strings.ToLower(fullName))
	s.installationsMu.Unlock()
}

// forgetInstallations clears the cache when installations change.
func (s *Service) forgetInstallations() {
	s.installationsMu.Lock()
	clear(s.installations)
	s.installationsMu.Unlock()
}

// publicCredential mints the token that fetches a pull request only the public
// demands, following the source that served the lease's read. The
// repository's own installation narrows it to that repository. An anonymous
// read fetches anonymously, with an empty token; the fetcher never holds a
// viewer's token, and the lease reads without one. The public installation's
// token cannot be narrowed to a repository outside it, so it is minted only
// while that installation reads no private repository; otherwise the pull
// request is fetched anonymously.
func (s *Service) publicCredential(ctx context.Context, ref PullRequestRef, read publicRead) (github.Token, error) {
	switch read.source {
	case SourceAnonymous, SourceViewer:
		return github.Token{}, nil
	case SourceRepository:
		token, err := s.github.InstallationToken(ctx, read.installation, []int64{ref.RepositoryID}, map[string]string{"contents": "read"})
		if err == nil {
			return token, nil
		}
		s.forgetInstallation(ref.FullName)
	}
	private, err := s.github.InstallationHasPrivate(ctx, s.public)
	switch {
	case err != nil:
		log.Printf("githubsync: fetching %s anonymously: checking public installation %d: %v", ref.FullName, s.public, err)
		return github.Token{}, nil
	case private:
		log.Printf("githubsync: fetching %s anonymously: public installation %d can read private repositories", ref.FullName, s.public)
		return github.Token{}, nil
	}
	return s.github.InstallationToken(ctx, s.public, nil, map[string]string{"contents": "read", "metadata": "read"})
}

// requestPublic requests a refresh with public demand. A fresh retention run
// makes the submission fresh, so an expired snapshot of an unchanged pull
// request is published again.
func (s *Service) requestPublic(ctx context.Context, ref PullRequestRef, pull []byte) (PublicPullRequest, error) {
	run, err := randomToken()
	if err != nil {
		return PublicPullRequest{}, err
	}
	if err := s.store.RequestPublicRefresh(ctx, ref, pull, run, s.millis()); err != nil {
		return PublicPullRequest{}, err
	}
	return s.publicState(ctx, ref)
}

func (s *Service) publicState(ctx context.Context, ref PullRequestRef) (PublicPullRequest, error) {
	state, err := s.store.PublicPullRequest(ctx, ref.RepositoryID, ref.Number, s.millis())
	if err != nil {
		return PublicPullRequest{}, err
	}
	return publicStatus(state)
}

// join returns a pending public refresh. One backing off after a failed
// fetch retries now, which admit counts as a fetch.
func (s *Service) join(ctx context.Context, state PublicState, admit func(context.Context) error) (PublicPullRequest, error) {
	if state.Leased || state.Attempt == 0 || state.Detail == "" {
		return publicStatus(state)
	}
	if err := admit(ctx); err != nil {
		return PublicPullRequest{}, err
	}
	if err := s.store.RetryPublicRefresh(ctx, state.Ref.RepositoryID, state.Ref.Number, s.millis()); err != nil {
		return PublicPullRequest{}, err
	}
	return s.publicState(ctx, state.Ref)
}

// publicStatus projects the stored state. A pending refresh decides the
// status until it completes; then the public snapshot's ingestion does.
func publicStatus(state PublicState) (PublicPullRequest, error) {
	result := PublicPullRequest{FullName: state.Ref.FullName, Number: state.Ref.Number}
	if len(state.Pull) > 0 {
		var pull github.PullRequest
		if err := json.Unmarshal(state.Pull, &pull); err != nil {
			return PublicPullRequest{}, err
		}
		result.PullRequest = pullRequestMetadata(state.Ref, pull)
	}
	if state.ContextID != "" {
		id, fetched := state.ContextID, state.FetchedAt
		result.ContextID, result.FetchedAt = &id, &fetched
	}
	switch {
	case state.Pending && state.Leased:
		result.Status = PublicFetching
	case state.Pending && state.Attempt > 0 && state.Detail != "":
		result.Status, result.Failure = PublicFailed, fetchFailure()
	case state.Pending:
		result.Status = PublicQueued
	case inProgress(state.JobState):
		result.Status = PublicFetching
	case state.ContextID != "":
		result.Status = PublicReady
	default:
		result.Status, result.Failure = PublicFailed, fetchFailure()
	}
	return result, nil
}

func refused(fullName string, number int, code string) PublicPullRequest {
	detail := "This pull request isn't public or doesn't exist."
	if code == FailureTooLarge {
		detail = fullName + " is over 500 MB, too large to preview."
	}
	return PublicPullRequest{FullName: fullName, Number: number, Status: PublicFailed, Failure: &PublicFailure{Code: code, Detail: detail}}
}

// restrictedRefusal answers a pull request GitHub refused every read source
// for; like other refusals it is not persisted.
func restrictedRefusal(fullName string, number int, remedy *string) PublicPullRequest {
	return PublicPullRequest{FullName: fullName, Number: number, Status: PublicFailed,
		Failure: &PublicFailure{Code: FailureRestricted, Detail: "GitHub refused access to " + fullName + ".", Remedy: remedy}}
}

func fetchFailure() *PublicFailure {
	return &PublicFailure{Code: FailureFetchFailed, Detail: "The pull request could not be fetched from GitHub."}
}

// inProgress reports an admitted public snapshot still being published.
func inProgress(jobState string) bool {
	return jobState == "queued" || jobState == "running" || jobState == "retrying"
}

func notFound(err error) bool {
	var status *github.Error
	return errors.As(err, &status) && status.Status == http.StatusNotFound
}
