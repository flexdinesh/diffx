package remoteserver

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

// GitHubSettings enables the GitHub App integration; nil disables its routes.
type GitHubSettings struct {
	AppID          int64
	ClientID       string
	ClientSecret   string
	PrivateKeyFile string
	WebhookSecret  string
	TokenKey       string
	PublicURL      string
	// InternalListen serves the fetcher protocol, authenticated by FetcherSecret.
	InternalListen string
	FetcherSecret  string
	// apiURL and webURL replace github.com in tests.
	apiURL, webURL string
}

// maxWebhookBytes is GitHub's documented webhook payload cap.
const maxWebhookBytes = 25 << 20

func composeGitHub(store *pgstore.Store, settings *GitHubSettings) (*githubsync.Service, error) {
	if settings == nil {
		return nil, nil
	}
	pem, err := os.ReadFile(settings.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("read GitHub App private key file: " + err.Error())
	}
	key, err := github.ParsePrivateKey(pem)
	if err != nil {
		return nil, err
	}
	tokenKey, err := base64.StdEncoding.DecodeString(settings.TokenKey)
	if err != nil {
		return nil, errors.New("GitHub token key must be standard base64")
	}
	var options []github.Option
	if settings.apiURL != "" {
		options = append(options, github.WithAPIURL(settings.apiURL), github.WithWebURL(settings.webURL))
	}
	client, err := github.New(github.App{ID: settings.AppID, ClientID: settings.ClientID, ClientSecret: settings.ClientSecret, PrivateKey: key, WebhookSecret: []byte(settings.WebhookSecret)}, options...)
	if err != nil {
		return nil, err
	}
	return githubsync.New(store.GitHub(), client, githubsync.Options{PublicURL: settings.PublicURL, TokenKey: tokenKey})
}

// reconcileLock admits one replica's reconcile at a time, reporting whether it ran.
type reconcileLock interface {
	ReconcileAlone(ctx context.Context, reconcile func(context.Context) error) (bool, error)
}

// reconcileAtStartup recovers missed deliveries once, then holds its task slot
// for the server lifetime. Failures are logged and Resync remains available.
func reconcileAtStartup(reconcile func(context.Context) error, lock reconcileLock) serverapp.Task {
	return func(ctx context.Context) error {
		reconcileOnce(ctx, reconcile, lock)
		<-ctx.Done()
		return nil
	}
}

// reconcileOnce skips while another replica reconciles: refresh intent is
// coalesced, so that replica's pass recovers the same deliveries.
func reconcileOnce(ctx context.Context, reconcile func(context.Context) error, lock reconcileLock) {
	if _, err := lock.ReconcileAlone(ctx, reconcile); err != nil && ctx.Err() == nil {
		log.Printf("GitHub reconcile: %v", err)
	}
}

// githubPublic serves the routes GitHub itself calls: signed webhooks and the
// OAuth callback, a cross-site top-level navigation whose state binds it to
// the session that started it.
func (remote *transport) githubPublic(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/v2/github/webhooks":
		remote.githubWebhook(w, r)
		return true
	case "/api/v2/github/callback":
		remote.githubCallback(w, r)
		return true
	}
	return false
}

func (remote *transport) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if remote.github == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		problem(w, 405, "Method not allowed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		problem(w, 413, "Webhook payload too large")
		return
	}
	err = remote.github.Webhook(r.Context(), r.Header.Get("X-GitHub-Event"), r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-Hub-Signature-256"), body)
	switch {
	case errors.Is(err, githubsync.ErrSignature):
		problem(w, 401, "Invalid webhook signature")
	case err != nil:
		log.Printf("GitHub webhook: %v", err)
		problem(w, 500, "Webhook not applied")
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

func (remote *transport) githubCallback(w http.ResponseWriter, r *http.Request) {
	if remote.github == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		problem(w, 405, "Method not allowed")
		return
	}
	p, err := remote.authenticate(r)
	if err != nil || p.session == nil {
		http.Redirect(w, r, "/settings?github=signin", http.StatusSeeOther)
		return
	}
	query := r.URL.Query()
	if query.Get("error") != "" || query.Get("code") == "" {
		http.Redirect(w, r, "/settings?github=cancelled", http.StatusSeeOther)
		return
	}
	if err := remote.github.Complete(r.Context(), p.account.ID, query.Get("state"), query.Get("code")); err != nil {
		log.Printf("GitHub authorization: %v", err)
		http.Redirect(w, r, "/settings?github=failed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?github=connected", http.StatusSeeOther)
}

// githubAccount serves integration management. Like account routes it needs a
// browser session, because it handles GitHub user credentials.
func (remote *transport) githubAccount(w http.ResponseWriter, r *http.Request, p principal) {
	if remote.github == nil {
		problem(w, 404, "GitHub integration is not configured")
		return
	}
	ctx, owner := r.Context(), p.account.ID
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/github/")
	switch {
	case path == "connection" && r.Method == http.MethodGet:
		status, err := remote.github.Status(ctx, owner)
		githubResult(w, status, err)
	case path == "connection" && r.Method == http.MethodPost:
		url, err := remote.github.Connect(ctx, owner)
		githubResult(w, struct {
			AuthorizeURL string `json:"authorizeUrl"`
		}{url}, err)
	case path == "connection" && r.Method == http.MethodDelete:
		githubResult(w, nil, remote.github.Disconnect(ctx, owner))
	case path == "repositories" && r.Method == http.MethodGet:
		repositories, err := remote.github.Repositories(ctx, owner)
		githubResult(w, struct {
			Repositories []githubsync.Repository `json:"repositories"`
		}{repositories}, err)
	case path == "resync" && r.Method == http.MethodPost:
		githubResult(w, nil, remote.github.Resync(ctx, owner))
	case strings.HasPrefix(path, "follows/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(path, "follows/"), 10, 64)
		if err != nil || id <= 0 {
			problem(w, 404, "Repository not found")
			return
		}
		switch r.Method {
		case http.MethodPut:
			githubResult(w, nil, remote.github.Follow(ctx, owner, id))
		case http.MethodDelete:
			githubResult(w, nil, remote.github.Unfollow(ctx, owner, id))
		case http.MethodPatch:
			var input struct {
				Notify *bool `json:"notify"`
			}
			if !decodeJSON(w, r, &input) {
				return
			}
			if input.Notify == nil {
				problem(w, 400, "notify is required")
				return
			}
			githubResult(w, nil, remote.github.SetNotify(ctx, owner, id, *input.Notify))
		default:
			problem(w, 405, "Method not allowed")
		}
	case path == "connection" || path == "repositories" || path == "resync":
		problem(w, 405, "Method not allowed")
	default:
		http.NotFound(w, r)
	}
}

func githubResult(w http.ResponseWriter, value any, err error) {
	var api *github.Error
	switch {
	case err == nil && value == nil:
		w.WriteHeader(http.StatusNoContent)
	case err == nil:
		writeJSON(w, 200, value)
	case errors.Is(err, githubsync.ErrNotConnected):
		problem(w, 409, err.Error())
	case errors.Is(err, githubsync.ErrReconnect):
		problem(w, 409, githubsync.ErrReconnect.Error())
	case errors.Is(err, githubsync.ErrForbidden):
		problem(w, 403, err.Error())
	case errors.Is(err, githubsync.ErrNotFound):
		problem(w, 404, "Repository not followed")
	case errors.As(err, &api) && api.Status == 429:
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "GitHub rate limit reached; try again later")
	default:
		log.Printf("GitHub integration: %v", err)
		problem(w, 502, "GitHub request failed")
	}
}
