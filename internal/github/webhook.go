package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type PullRequestEvent struct {
	Action         string
	InstallationID int64
	Repository     Repository
	Number         int
}

// InstallationEvent.Action is created, deleted, suspend, unsuspend or new_permissions_accepted.
type InstallationEvent struct {
	Action         string
	InstallationID int64
}

type InstallationRepositoriesEvent struct {
	Action         string
	InstallationID int64
	Added          []Repository
	Removed        []Repository
}

// AuthorizationEvent reports a user revoking the app (github_app_authorization).
type AuthorizationEvent struct {
	Action string
	UserID int64
}

// VerifyWebhook checks an X-Hub-Signature-256 header in constant time.
func (c *Client) VerifyWebhook(body []byte, signature256 string) bool {
	encoded, ok := strings.CutPrefix(signature256, "sha256=")
	if !ok {
		return false
	}
	signature, err := hex.DecodeString(encoded)
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, c.app.WebhookSecret)
	mac.Write(body)
	return hmac.Equal(signature, mac.Sum(nil))
}

type webhookPayload struct {
	Action       string `json:"action"`
	Number       int    `json:"number"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository          repositoryJSON   `json:"repository"`
	RepositoriesAdded   []repositoryJSON `json:"repositories_added"`
	RepositoriesRemoved []repositoryJSON `json:"repositories_removed"`
	Sender              struct {
		ID int64 `json:"id"`
	} `json:"sender"`
}

// ParseEvent decodes a verified webhook body by X-GitHub-Event name. It returns
// PullRequestEvent, InstallationEvent, InstallationRepositoriesEvent or
// AuthorizationEvent, and (nil, nil) for events diffx ignores, including ping.
func ParseEvent(event string, body []byte) (any, error) {
	switch event {
	case "pull_request", "installation", "installation_repositories", "github_app_authorization":
	default:
		return nil, nil
	}
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid %s webhook: %w", event, err)
	}
	if payload.Action == "" {
		return nil, fmt.Errorf("invalid %s webhook: missing action", event)
	}
	if event == "github_app_authorization" {
		if payload.Sender.ID <= 0 {
			return nil, fmt.Errorf("invalid %s webhook: missing sender", event)
		}
		return AuthorizationEvent{Action: payload.Action, UserID: payload.Sender.ID}, nil
	}
	installationID := payload.Installation.ID
	if installationID <= 0 {
		return nil, fmt.Errorf("invalid %s webhook: missing installation", event)
	}
	switch event {
	case "pull_request":
		if payload.Number <= 0 || payload.Repository.ID <= 0 {
			return nil, fmt.Errorf("invalid %s webhook: missing pull request or repository", event)
		}
		return PullRequestEvent{Action: payload.Action, InstallationID: installationID,
			Repository: payload.Repository.repository(installationID), Number: payload.Number}, nil
	case "installation":
		return InstallationEvent{Action: payload.Action, InstallationID: installationID}, nil
	}
	return InstallationRepositoriesEvent{Action: payload.Action, InstallationID: installationID,
		Added:   repositories(payload.RepositoriesAdded, installationID),
		Removed: repositories(payload.RepositoriesRemoved, installationID)}, nil
}

func repositories(items []repositoryJSON, installationID int64) []Repository {
	result := make([]Repository, len(items))
	for index, item := range items {
		result[index] = item.repository(installationID)
	}
	return result
}
