package remoteserver

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
)

// publicPullRequests reports whether anyone may open public pull request
// previews: the GitHub integration can read public repositories and admins
// turned previews on.
func (remote *transport) publicPullRequests() bool {
	return remote.github != nil && remote.github.PublicAvailable() && remote.identity.Settings().PublicPullRequests
}

// publicRoutes serves /api/v2/public/: public pull request previews and the
// read-only routes of their shared snapshots. Credentials play no part, and
// everything else, including while previews are off, is not found.
func (remote *transport) publicRoutes(w http.ResponseWriter, r *http.Request) {
	if !remote.publicPullRequests() {
		authhttp.Problem(w, 404, "Not found")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v2/public/")
	if address, ok := strings.CutPrefix(rest, "pull-requests/"); ok {
		remote.publicPullRequest(w, r, address)
		return
	}
	if route, ok := strings.CutPrefix(rest, "contexts/"); ok && publicContextRoute(strings.Split(route, "/")) {
		remote.publicContext(w, r, route)
		return
	}
	authhttp.Problem(w, 404, "Not found")
}

// publicPullRequest serves {owner}/{repo}/{number}: GET status, PUT ensure,
// and POST {owner}/{repo}/{number}/refresh. Only fetches count toward the
// fetch limit.
func (remote *transport) publicPullRequest(w http.ResponseWriter, r *http.Request, address string) {
	parts := strings.Split(address, "/")
	refresh := len(parts) == 4 && parts[3] == "refresh"
	if len(parts) != 3 && !refresh {
		authhttp.Problem(w, 404, "Not found")
		return
	}
	number, err := strconv.Atoi(parts[2])
	if err != nil {
		authhttp.Problem(w, 400, "Pull request number must be a positive integer")
		return
	}
	ctx, owner, repo := r.Context(), parts[0], parts[1]
	var status githubsync.PublicPullRequest
	switch {
	case refresh && r.Method == http.MethodPost:
		status, err = remote.github.RefreshPublic(ctx, owner, repo, number, remote.fetches.Admit(r))
	case !refresh && r.Method == http.MethodGet:
		status, err = remote.github.PublicStatus(ctx, owner, repo, number)
	case !refresh && r.Method == http.MethodPut:
		status, err = remote.github.EnsurePublic(ctx, owner, repo, number, remote.fetches.Admit(r))
	default:
		authhttp.Problem(w, 405, "Method not allowed")
		return
	}
	var limited *authhttp.RateLimited
	var api *github.Error
	switch {
	case err == nil:
		authhttp.WriteJSON(w, 200, status)
	case errors.Is(err, githubsync.ErrInvalidPullRequest):
		authhttp.Problem(w, 400, "Invalid GitHub pull request address")
	case errors.Is(err, githubsync.ErrNotFound):
		authhttp.Problem(w, 404, "Public pull request not found")
	case errors.As(err, &limited):
		authhttp.TooManyRequests(w, limited.RetryAfter, "Fetch limit reached; try again later")
	case errors.Is(err, authhttp.ErrClientIP):
		authhttp.Problem(w, 400, "Client IP header missing or invalid")
	case errors.As(err, &api) && api.Status == http.StatusTooManyRequests:
		authhttp.TooManyRequests(w, time.Minute, "GitHub rate limit reached; try again later")
	case errors.As(err, &api):
		log.Printf("public pull request %s/%s#%d: %v", owner, repo, number, err)
		authhttp.Problem(w, 502, "GitHub request failed")
	default:
		log.Printf("public pull request %s/%s#%d: %v", owner, repo, number, err)
		authhttp.Problem(w, 500, "Server could not process request")
	}
}

// publicContextRoute admits the snapshot reads of a context route, split
// after /contexts/: metadata, session, diffs, versions, file previews and
// commits. Comments, marks, events and deletion are absent.
func publicContextRoute(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	switch len(parts) {
	case 1:
		return true
	case 2:
		return parts[1] == "session" || parts[1] == "commits"
	case 3:
		return parts[1] == "diffs" && parts[2] == "current"
	case 4:
		return parts[1] == "commits" && parts[3] == "diff"
	case 5:
		return parts[1] == "diffs" && parts[3] == "versions"
	case 6:
		return (parts[1] == "diffs" || parts[1] == "commits") && parts[3] == "files" && (parts[5] == "patch" || parts[5] == "contents")
	}
	return false
}

// publicContext serves a snapshot read through the public application,
// which resolves only the public owner's contexts, so account contexts are
// not found.
func (remote *transport) publicContext(w http.ResponseWriter, r *http.Request, route string) {
	// Snapshots are read-only: no deletion or other mutation exists here.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		authhttp.Problem(w, 404, "Not found")
		return
	}
	app, err := remote.applications.public()
	if err != nil {
		authhttp.IdentityError(w, err)
		return
	}
	scoped := r.Clone(r.Context())
	scoped.URL.Path, scoped.URL.RawPath = "/api/v2/contexts/"+route, ""
	app.handler.ServeHTTP(w, scoped)
}
