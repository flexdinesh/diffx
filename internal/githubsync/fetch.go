package githubsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

// leaseDuration bounds one fetch; installation tokens outlive it.
const leaseDuration = 10 * time.Minute

// maxSkips bounds pending refreshes resolved without a fetch in one lease call.
const maxSkips = 32

// ErrInvalidResult reports a fetcher snapshot that does not match its lease.
var ErrInvalidResult = errors.New("fetch result does not match the leased pull request")

// Lease claims the next pending pull request refresh for the fetcher. It
// resolves refreshes that need no fetch (deleted pull requests, repositories
// without followers) itself. Nil means nothing is pending.
func (s *Service) Lease(ctx context.Context) (*fetchprotocol.Job, error) {
	for range maxSkips {
		token, err := randomToken()
		if err != nil {
			return nil, err
		}
		now := s.now()
		lease, err := s.store.ClaimRefresh(ctx, token, now.UnixMilli(), now.Add(leaseDuration).UnixMilli())
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		job, err := s.prepare(ctx, lease, now)
		if err != nil {
			return nil, errors.Join(err, s.release(ctx, lease, err.Error()))
		}
		if job != nil {
			return job, nil
		}
	}
	return nil, nil
}

// prepare reads the pull request and mints the fetch credential. Nil without
// error means the refresh completed without a fetch.
func (s *Service) prepare(ctx context.Context, lease Lease, now time.Time) (*fetchprotocol.Job, error) {
	ref := lease.Ref
	pull, err := s.github.PullRequest(ctx, ref.InstallationID, ref.FullName, ref.Number)
	var status *github.Error
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return nil, s.store.CompleteRefresh(ctx, lease, "closed", "", nil, now.UnixMilli())
	}
	if err != nil {
		return nil, fmt.Errorf("read pull request %s#%d: %w", ref.FullName, ref.Number, err)
	}
	follows, err := s.store.RepositoryFollows(ctx, ref.RepositoryID)
	if err != nil {
		return nil, err
	}
	if !hasActive(follows) {
		return nil, s.store.CompleteRefresh(ctx, lease, pullState(pull), "", nil, now.UnixMilli())
	}
	credential, err := s.github.InstallationToken(ctx, ref.InstallationID, []int64{ref.RepositoryID}, map[string]string{"contents": "read"})
	if err != nil {
		return nil, fmt.Errorf("mint fetch token for %s: %w", ref.FullName, err)
	}
	raw, err := json.Marshal(pull)
	if err != nil {
		return nil, err
	}
	if err := s.store.RecordLease(ctx, lease, raw, now.UnixMilli()); err != nil {
		return nil, err
	}
	// GitHub's current name follows renames; the fetcher refuses redirects.
	name := ref.FullName
	if pull.Base.Repository.FullName != "" {
		name = pull.Base.Repository.FullName
	}
	return &fetchprotocol.Job{
		ID: jobID(ref), LeaseToken: lease.Token, LeaseExpiresAt: now.Add(leaseDuration).UnixMilli(),
		Repository: fetchprotocol.Repository{ID: ref.RepositoryID, FullName: name, CloneURL: "https://github.com/" + name + ".git"},
		Number:     ref.Number, HeadSHA: pull.Head.SHA, BaseRef: pull.Base.Ref, BaseSHA: pull.Base.SHA,
		Token: credential.Value, TokenExpiresAt: credential.ExpiresAt.UnixMilli(),
	}, nil
}

// Finish applies a fetcher result. A snapshot is published to every account
// with verified access in one transaction with the refresh; a failure retries
// with backoff. ErrLeaseLost means another lease superseded this one. When the
// snapshot announces new work (see announcement), the same transaction records
// a push notification for every admitted follower that has not muted the
// repository.
func (s *Service) Finish(ctx context.Context, result fetchprotocol.Result) error {
	repositoryID, number, ok := parseJobID(result.ID)
	if !ok {
		return ErrLeaseLost
	}
	lease, raw, err := s.store.Leased(ctx, repositoryID, number, result.LeaseToken, s.millis())
	if err != nil {
		return err
	}
	if result.Request == nil {
		detail := result.Error
		if detail == "" {
			detail = "fetch failed"
		}
		return s.release(ctx, lease, detail)
	}
	var pull github.PullRequest
	if err := json.Unmarshal(raw, &pull); err != nil {
		return err
	}
	request, err := publication(lease, pull, *result.Request)
	if err != nil {
		return errors.Join(err, s.release(ctx, lease, err.Error()))
	}
	follows, err := s.followers(ctx, repositoryID)
	if err != nil {
		return err
	}
	// The lease fences other refreshes of this pull request, so the recorded
	// head cannot change before CompleteRefresh commits.
	previous, opened, err := s.store.PullRequestHead(ctx, repositoryID, number)
	if err != nil {
		return err
	}
	notification := announcement(lease.Ref, pull, previous, opened)
	admissions := make([]Admission, 0, len(follows))
	for _, follow := range follows {
		admission := Admission{OwnerID: follow.OwnerID, Request: request}
		if notification != nil && !follow.Muted {
			admission.Notification = notification
		}
		admissions = append(admissions, admission)
	}
	return s.store.CompleteRefresh(ctx, lease, pullState(pull), pull.Head.SHA, admissions, s.millis())
}

// maxNotificationTitle bounds the pull request title shown in a notification.
const maxNotificationTitle = 120

// announcement decides whether a snapshot notifies followers. A captured head
// that differs from the last recorded one is new work, whatever the pull
// request's author. A first snapshot notifies only when a webhook announced
// the pull request, so first follows, startup reconciliation and Resync import
// existing pull requests silently. Metadata edits, closing and merging keep
// the head and never notify.
func announcement(ref PullRequestRef, pull github.PullRequest, previous string, opened bool) *Notification {
	var action string
	switch {
	case previous == "" && opened:
		action = "New PR by @"
	case previous != "" && previous != pull.Head.SHA:
		action = "Updated by @"
	default:
		return nil
	}
	title := strings.TrimSpace(pull.Title)
	if runes := []rune(title); len(runes) > maxNotificationTitle {
		title = strings.TrimSpace(string(runes[:maxNotificationTitle-1])) + "…"
	}
	return &Notification{Title: ref.FullName + " #" + strconv.Itoa(ref.Number), Body: title + "\n" + action + pull.Author.Login}
}

// release ends a lease with exponential backoff capped at an hour.
func (s *Service) release(ctx context.Context, lease Lease, detail string) error {
	delay := time.Minute << min(lease.Attempt, 6)
	if delay > time.Hour {
		delay = time.Hour
	}
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	return s.store.ReleaseRefresh(ctx, lease, s.now().Add(delay).UnixMilli(), detail)
}

// publication turns an owner-free fetcher snapshot into the server-authored
// observation. Identity, labels and pull request metadata come from the lease,
// never the fetcher; the snapshot must be of the leased head and base.
func publication(lease Lease, pull github.PullRequest, fetched ingestion.Request) (ingestion.Request, error) {
	metadata := fetched.Metadata
	comparison := metadata.Comparison
	if metadata.Head == nil || *metadata.Head != pull.Head.SHA || comparison == nil || comparison.Kind != "branch" || comparison.BaseCommit != pull.Base.SHA {
		return ingestion.Request{}, ErrInvalidResult
	}
	ref := lease.Ref
	remote := "https://github.com/" + ref.FullName + ".git"
	repositoryKey := review.RemoteRepositoryKey(remote)
	pullURL := "https://github.com/" + ref.FullName + "/pull/" + strconv.Itoa(ref.Number)
	_, name, _ := strings.Cut(ref.FullName, "/")
	identity := strconv.FormatInt(ref.RepositoryID, 10) + "\x00" + strconv.Itoa(ref.Number)
	head := pull.Head.SHA
	request := ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion,
		ContentHash:     fetched.ContentHash,
		Metadata: ingestion.Metadata{
			SourceID: ingestion.GitHubSourceID, Hostname: "github.com", Trigger: ingestion.GitHubTrigger,
			RepositoryKey: repositoryKey, RepositoryName: name, RemoteURL: remote,
			CheckoutKey: digest("github-pull-request-checkout", identity), BranchID: digest("github-pull-request-branch", identity),
			Root: pullURL, WorktreeName: "PR #" + strconv.Itoa(ref.Number), Branch: pull.Head.Ref, Head: &head,
			CollectedAt: pull.UpdatedAt.UnixMilli(), ActivityAt: pull.UpdatedAt.UnixMilli(), CollectorVersion: metadata.CollectorVersion,
			Comparison: &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/" + pull.Base.Ref, BaseCommit: comparison.BaseCommit, MergeBase: comparison.MergeBase},
			PullRequest: &ingestion.PullRequest{
				Provider: "github", RepositoryID: ref.RepositoryID, FullName: ref.FullName, Number: ref.Number, URL: pullURL,
				Title: pull.Title, Body: pull.Body, State: pullState(pull), Draft: pull.Draft,
				Author:  ingestion.PullRequestAuthor{Login: pull.Author.Login, URL: "https://github.com/" + pull.Author.Login},
				BaseRef: pull.Base.Ref, HeadRef: pull.Head.Ref, HeadRepository: pull.Head.Repository.FullName,
				CreatedAt: pull.CreatedAt.UnixMilli(), UpdatedAt: pull.UpdatedAt.UnixMilli(),
			},
		},
		Scopes: make([]ingestion.Scope, 0, len(fetched.Scopes)),
	}
	for _, scope := range fetched.Scopes {
		scope.Snapshot.Root, scope.Snapshot.Name, scope.Snapshot.Branch = pullURL, ref.FullName+"#"+strconv.Itoa(ref.Number), pull.Head.Ref
		request.Scopes = append(request.Scopes, scope)
	}
	// Identical pull request state yields an identical payload, so refetches
	// replay; a retention run makes the submission fresh.
	raw, err := json.Marshal(request)
	if err != nil {
		return ingestion.Request{}, err
	}
	request.SubmissionID = "gh-" + digest(string(raw), lease.RetentionRun)
	if err := ingestion.Validate(request); err != nil {
		return ingestion.Request{}, errors.Join(ErrInvalidResult, err)
	}
	return request, nil
}

func pullState(pull github.PullRequest) string {
	switch {
	case pull.Merged:
		return "merged"
	case pull.State == "closed":
		return "closed"
	default:
		return "open"
	}
}

func hasActive(follows []Follow) bool {
	for _, follow := range follows {
		if follow.Status == "active" {
			return true
		}
	}
	return false
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func jobID(ref PullRequestRef) string {
	return strconv.FormatInt(ref.RepositoryID, 10) + "-" + strconv.Itoa(ref.Number)
}

func parseJobID(id string) (int64, int, bool) {
	repository, number, ok := strings.Cut(id, "-")
	if !ok {
		return 0, 0, false
	}
	repositoryID, err := strconv.ParseInt(repository, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	pullNumber, err := strconv.Atoi(number)
	return repositoryID, pullNumber, err == nil
}
