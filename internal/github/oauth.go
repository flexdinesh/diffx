package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxUserRepositories = 10000

// UserToken times are zero when GitHub omits the matching expiry.
type UserToken struct {
	AccessToken      string
	ExpiresAt        time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

func (c *Client) AuthorizeURL(state, redirectURI string) string {
	query := url.Values{"client_id": {c.app.ClientID}, "redirect_uri": {redirectURI}, "state": {state}}
	return c.webURL + "/login/oauth/authorize?" + query.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, redirectURI string) (UserToken, error) {
	if code == "" {
		return UserToken{}, errors.New("github OAuth code is required")
	}
	return c.userToken(ctx, url.Values{"code": {code}, "redirect_uri": {redirectURI}})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (UserToken, error) {
	if refreshToken == "" {
		return UserToken{}, errors.New("github refresh token is required")
	}
	return c.userToken(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

// userToken posts to the OAuth token endpoint, which reports failures as HTTP 200 {"error":...}.
func (c *Client) userToken(ctx context.Context, form url.Values) (UserToken, error) {
	form.Set("client_id", c.app.ClientID)
	form.Set("client_secret", c.app.ClientSecret)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return UserToken{}, err
	}
	setHeaders(request.Header, "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	now := time.Now()
	var response struct {
		AccessToken           string `json:"access_token"`
		ExpiresIn             int64  `json:"expires_in"`
		RefreshToken          string `json:"refresh_token"`
		RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
		Error                 string `json:"error"`
	}
	if _, err := c.send(request, &response); err != nil {
		return UserToken{}, err
	}
	switch response.Error {
	case "":
	case "bad_verification_code", "bad_refresh_token":
		return UserToken{}, fmt.Errorf("%w: %s", ErrUnauthorized, response.Error)
	default:
		return UserToken{}, fmt.Errorf("github OAuth error: %s", bounded(response.Error))
	}
	if response.AccessToken == "" {
		return UserToken{}, errors.New("github OAuth response missing access token")
	}
	token := UserToken{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken}
	if response.ExpiresIn > 0 {
		token.ExpiresAt = now.Add(time.Duration(response.ExpiresIn) * time.Second)
	}
	if response.RefreshTokenExpiresIn > 0 {
		token.RefreshExpiresAt = now.Add(time.Duration(response.RefreshTokenExpiresIn) * time.Second)
	}
	return token, nil
}

func (c *Client) User(ctx context.Context, accessToken string) (Account, error) {
	var user accountJSON
	if _, err := c.get(ctx, c.apiURL+"/user", accessToken, &user); err != nil {
		return Account{}, err
	}
	if user.ID <= 0 || user.Login == "" {
		return Account{}, errors.New("github user response missing identity")
	}
	return user.account(), nil
}

type installationsPage struct {
	Installations []struct {
		ID int64 `json:"id"`
	} `json:"installations"`
}

type repositoriesPage struct {
	Repositories []repositoryJSON `json:"repositories"`
}

// UserRepositories lists repositories both the user and the app can access,
// tagged with their installation and capped at 10000.
func (c *Client) UserRepositories(ctx context.Context, accessToken string) ([]Repository, error) {
	installations, err := collect(ctx, c, accessToken, "/user/installations?per_page="+pageSize, maxUserRepositories,
		func(page installationsPage) []int64 {
			ids := make([]int64, 0, len(page.Installations))
			for _, installation := range page.Installations {
				ids = append(ids, installation.ID)
			}
			return ids
		})
	if err != nil {
		return nil, err
	}
	repositories := []Repository{}
	for _, installationID := range installations {
		remaining := maxUserRepositories - len(repositories)
		if remaining <= 0 {
			break
		}
		if installationID <= 0 {
			return nil, errors.New("github installation response missing ID")
		}
		path := "/user/installations/" + strconv.FormatInt(installationID, 10) + "/repositories?per_page=" + pageSize
		batch, err := collect(ctx, c, accessToken, path, remaining, func(page repositoriesPage) []Repository {
			result := make([]Repository, len(page.Repositories))
			for index, repository := range page.Repositories {
				result[index] = repository.repository(installationID)
			}
			return result
		})
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, batch...)
	}
	return repositories, nil
}
