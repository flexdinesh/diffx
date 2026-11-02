package github

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxOpenPullRequests = 3000

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
}

type Ref struct {
	Ref        string
	SHA        string
	Repository Repository
}

// PullRequest.Head.Repository is zero when the head fork was deleted.
type PullRequest struct {
	Number    int
	State     string
	Merged    bool
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
}

func (repository repositoryJSON) repository(installationID int64) Repository {
	return Repository{ID: repository.ID, FullName: repository.FullName, CloneURL: repository.CloneURL,
		Private: repository.Private, InstallationID: installationID}
}

// Repository reads a repository through an installation-wide token. GitHub
// answers 404 for repositories the token cannot read.
func (c *Client) Repository(ctx context.Context, installationID int64, fullName string) (Repository, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return Repository{}, err
	}
	bearer, err := c.installationBearer(ctx, installationID)
	if err != nil {
		return Repository{}, err
	}
	var repository repositoryJSON
	if _, err := c.get(ctx, c.apiURL+path, bearer, &repository); err != nil {
		return Repository{}, c.forgetRejected(installationID, err)
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
	Number    int         `json:"number"`
	State     string      `json:"state"`
	MergedAt  *time.Time  `json:"merged_at"`
	Draft     bool        `json:"draft"`
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	HTMLURL   string      `json:"html_url"`
	User      accountJSON `json:"user"`
	Base      refJSON     `json:"base"`
	Head      refJSON     `json:"head"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (pull pullRequestJSON) pullRequest() PullRequest {
	return PullRequest{Number: pull.Number, State: pull.State, Merged: pull.MergedAt != nil, Draft: pull.Draft,
		Title: pull.Title, Body: pull.Body, HTMLURL: pull.HTMLURL, Author: pull.User.account(),
		Base: pull.Base.ref(), Head: pull.Head.ref(), CreatedAt: pull.CreatedAt, UpdatedAt: pull.UpdatedAt}
}

func (c *Client) PullRequest(ctx context.Context, installationID int64, fullName string, number int) (PullRequest, error) {
	path, err := repositoryPath(fullName)
	if err != nil {
		return PullRequest{}, err
	}
	if number <= 0 {
		return PullRequest{}, errors.New("github pull request number must be positive")
	}
	bearer, err := c.installationBearer(ctx, installationID)
	if err != nil {
		return PullRequest{}, err
	}
	var pull pullRequestJSON
	if _, err := c.get(ctx, c.apiURL+path+"/pulls/"+strconv.Itoa(number), bearer, &pull); err != nil {
		return PullRequest{}, c.forgetRejected(installationID, err)
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
