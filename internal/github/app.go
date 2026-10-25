package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const tokenRefreshMargin = 5 * time.Minute

type Token struct {
	Value     string
	ExpiresAt time.Time
}

// ParsePrivateKey accepts PKCS#1 ("RSA PRIVATE KEY") or PKCS#8 ("PRIVATE KEY") PEM.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("github app private key must be PEM encoded")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid github app private key: %w", err)
		}
		return key, nil
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid github app private key: %w", err)
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("github app private key must be RSA")
		}
		return key, nil
	}
	return nil, fmt.Errorf("unsupported github app private key PEM type %q", block.Type)
}

// appJWT signs an RS256 app token; iat is backdated for clock drift and exp stays under GitHub's 10m cap.
func (c *Client) appJWT(now time.Time) (string, error) {
	claims, err := json.Marshal(struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}{now.Add(-60 * time.Second).Unix(), now.Add(9 * time.Minute).Unix(), strconv.FormatInt(c.app.ID, 10)})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.app.PrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("sign github app JWT failed")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (c *Client) AppSlug(ctx context.Context) (string, error) {
	jwt, err := c.appJWT(time.Now())
	if err != nil {
		return "", err
	}
	var app struct {
		Slug string `json:"slug"`
	}
	if _, err := c.get(ctx, c.apiURL+"/app", jwt, &app); err != nil {
		return "", err
	}
	if app.Slug == "" {
		return "", errors.New("github app response missing slug")
	}
	return app.Slug, nil
}

// InstallationToken mints a token narrowed to repositoryIDs and permissions when given.
func (c *Client) InstallationToken(ctx context.Context, installationID int64, repositoryIDs []int64, permissions map[string]string) (Token, error) {
	if installationID <= 0 {
		return Token{}, errors.New("github installation ID is required")
	}
	jwt, err := c.appJWT(time.Now())
	if err != nil {
		return Token{}, err
	}
	body := struct {
		RepositoryIDs []int64           `json:"repository_ids,omitempty"`
		Permissions   map[string]string `json:"permissions,omitempty"`
	}{repositoryIDs, permissions}
	target := c.apiURL + "/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	request, err := c.newRequest(ctx, http.MethodPost, target, jwt, body)
	if err != nil {
		return Token{}, err
	}
	var response struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if _, err := c.send(request, &response); err != nil {
		return Token{}, err
	}
	if response.Token == "" || response.ExpiresAt.IsZero() {
		return Token{}, errors.New("github installation token response incomplete")
	}
	return Token{Value: response.Token, ExpiresAt: response.ExpiresAt}, nil
}

// installationBearer reuses an installation-wide token until tokenRefreshMargin before expiry.
func (c *Client) installationBearer(ctx context.Context, installationID int64) (string, error) {
	c.mu.Lock()
	token, ok := c.tokens[installationID]
	c.mu.Unlock()
	if ok && time.Until(token.ExpiresAt) > tokenRefreshMargin {
		return token.Value, nil
	}
	token, err := c.InstallationToken(ctx, installationID, nil, nil)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tokens[installationID] = token
	c.mu.Unlock()
	return token.Value, nil
}

// forgetRejected drops a cached token GitHub no longer accepts.
func (c *Client) forgetRejected(installationID int64, err error) error {
	if errors.Is(err, ErrUnauthorized) {
		c.mu.Lock()
		delete(c.tokens, installationID)
		c.mu.Unlock()
	}
	return err
}
