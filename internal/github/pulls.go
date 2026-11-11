package github

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxOpenPullRequests   = 3000
	maxClosedPullRequests = 1000
)

type Account struct {
	ID      int64
	Login   string
	HTMLURL string
}

// Repository.InstallationID is set only by installation-scoped listings and
// webhooks; Visibility and Size (in KB) only by Repository.
type Repository struct {
	ID             int64
	FullName       string
	CloneURL       string
	Private        bool
	InstallationID int64
	Visibility     string
	Size           int64
	// PullRequestsDisabled is set when the repository turned pull requests
	// off; GitHub then answers 404 for its pull requests.
	PullRequestsDisabled bool
}

type Ref struct {
	Ref        string
	SHA        string
	Repository Repository
}

// PullRequest.Head.Repository is zero when the head fork was deleted.
// MergedAt and ClosedAt are nil until the pull request merged or closed;
// MergedBy is nil until it merged, and when GitHub omits the merger.
type PullRequest struct {
	Number    int
	State     string
	Merged    bool
	MergedAt  *time.Time
	ClosedAt  *time.Time
	MergedBy  *Account
	Draft     bool
	Title     string
	Body      string
	HTMLURL   string
	Author    Account
	Base      Ref
	Head      Ref
	CreatedAt time.Time
	UpdatedAt time.Time
}

type accountJSON struct {
	ID      int64  `json:"id"`
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
}

func (account accountJSON) account() Account {
	return Account{ID: account.ID, Login: account.Login, HTMLURL: account.HTMLURL}
}

type repositoryJSON struct {
	ID         int64  `json:"id"`
	FullName   string `json:"full_name"`
	CloneURL   string `json:"clone_url"`
	Private    bool   `json:"private"`
	Visibility string `json:"visibility"`
	Size       int64  `json:"size"`
	// HasPullRequests is absent from older payloads, which means enabled.
	HasPullRequests *bool `json:"has_pull_requests"`
}

func (repository repositoryJSON) repository(installationID int64) Repository {
	return Repository{ID: repository.ID, FullName: repository.FullName, CloneURL: repository.CloneURL,
		Private: repository.Private, InstallationID: installationID,
		PullRequestsDisabled: repository.HasPullRequests != nil && !*repository.HasPullRequests}
}

// Repository reads a repository with credential. GitHub answers 404 for
// repositories the credential cannot read.
func (c *Client) Repository(ctx context.Context, credential Credential, fullName string) (Repository, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return Repository{}, err
	}
	bearer, err := c.bearer(ctx, credential)
	if err != nil {
		return Repository{}, err
	}
	var repository repositoryJSON
	if _, err := c.get(ctx, c.apiURL+path, bearer, &repository); err != nil {
		return Repository{}, c.rejected(credential, err)
	}
	result := repository.repository(0)
	result.Visibility, result.Size = repository.Visibility, repository.Size
	return result, nil
}

type refJSON struct {
	Ref  string          `json:"ref"`
	SHA  string          `json:"sha"`
	Repo *repositoryJSON `json:"repo"`
}

func (ref refJSON) ref() Ref {
	result := Ref{Ref: ref.Ref, SHA: ref.SHA}
	if ref.Repo != nil {
		result.Repository = ref.Repo.repository(0)
	}
	return result
}

type pullRequestJSON struct {
	Number    int          `json:"number"`
	State     string       `json:"state"`
	MergedAt  *time.Time   `json:"merged_at"`
	ClosedAt  *time.Time   `json:"closed_at"`
	MergedBy  *accountJSON `json:"merged_by"`
	Draft     bool         `json:"draft"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	HTMLURL   string       `json:"html_url"`
	User      accountJSON  `json:"user"`
	Base      refJSON      `json:"base"`
	Head      refJSON      `json:"head"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func (pull pullRequestJSON) pullRequest() PullRequest {
	var mergedBy *Account
	if pull.MergedBy != nil {
		merger := pull.MergedBy.account()
		mergedBy = &merger
	}
	return PullRequest{Number: pull.Number, State: pull.State, Merged: pull.MergedAt != nil, MergedAt: pull.MergedAt, ClosedAt: pull.ClosedAt, MergedBy: mergedBy, Draft: pull.Draft,
		Title: pull.Title, Body: pull.Body, HTMLURL: pull.HTMLURL, Author: pull.User.account(),
		Base: pull.Base.ref(), Head: pull.Head.ref(), CreatedAt: pull.CreatedAt, UpdatedAt: pull.UpdatedAt}
}

// PullRequest reads a pull request with credential.
func (c *Client) PullRequest(ctx context.Context, credential Credential, fullName string, number int) (PullRequest, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return PullRequest{}, err
	}
	if number <= 0 {
		return PullRequest{}, errors.New("github pull request number must be positive")
	}
	bearer, err := c.bearer(ctx, credential)
	if err != nil {
		return PullRequest{}, err
	}
	var pull pullRequestJSON
	if _, err := c.get(ctx, c.apiURL+path+"/pulls/"+strconv.Itoa(number), bearer, &pull); err != nil {
		return PullRequest{}, c.rejected(credential, err)
	}
	return pull.pullRequest(), nil
}

// OpenPullRequests returns at most 3000 open pull requests.
func (c *Client) OpenPullRequests(ctx context.Context, installationID int64, fullName string) ([]PullRequest, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return nil, err
	}
	bearer, err := c.installationBearer(ctx, installationID)
	if err != nil {
		return nil, err
	}
	pulls, err := collect(ctx, c, bearer, path+"/pulls?state=open&per_page="+pageSize, maxOpenPullRequests,
		func(page []pullRequestJSON) []pullRequestJSON { return page })
	if err != nil {
		return nil, c.forgetRejected(installationID, err)
	}
	result := make([]PullRequest, len(pulls))
	for index, pull := range pulls {
		result[index] = pull.pullRequest()
	}
	return result, nil
}

// ClosedPullRequests returns closed pull requests updated at or after since,
// most recently updated first, stopping at the first older page item and after
// at most 1000.
func (c *Client) ClosedPullRequests(ctx context.Context, installationID int64, fullName string, since time.Time) ([]PullRequest, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return nil, err
	}
	bearer, err := c.installationBearer(ctx, installationID)
	if err != nil {
		return nil, err
	}
	result := []PullRequest{}
	for target := c.apiURL + path + "/pulls?state=closed&sort=updated&direction=desc&per_page=" + pageSize; target != ""; {
		var page []pullRequestJSON
		header, err := c.get(ctx, target, bearer, &page)
		if err != nil {
			return nil, c.forgetRejected(installationID, err)
		}
		for _, pull := range page {
			if pull.UpdatedAt.Before(since) || len(result) == maxClosedPullRequests {
				return result, nil
			}
			result = append(result, pull.pullRequest())
		}
		if len(page) == 0 {
			break
		}
		if target, err = c.nextPage(header); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// repositoryPath builds /repos/{owner}/{name} from exactly "owner/name".
func repositoryPath(fullName string) (string, error) {
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok || !validName(owner) || !validName(name) {
		return "", ErrInvalidRepository
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

func validName(name string) bool {
	if name == "" || len(name) > 100 || name == "." || name == ".." {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' ||
			char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}
