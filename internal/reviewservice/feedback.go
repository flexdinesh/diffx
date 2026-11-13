package reviewservice

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

// MinCommentPrefix is the shortest comment ID prefix a reference may use.
const MinCommentPrefix = 8

// maxCandidates bounds the reviews or comments an ambiguous reference lists.
const maxCandidates = 20

// Permalink is a review's live path: its stream's, or the context's when it
// has no stream, optionally opening one comment.
func Permalink(streamID, contextID, commentID string) string {
	path := "/contexts/" + url.PathEscape(contextID)
	if streamID != "" {
		path = "/streams/" + url.PathEscape(streamID)
	}
	if commentID != "" {
		path += "?comment=" + url.QueryEscape(commentID)
	}
	return path
}

// FeedbackQuery names the review whose feedback to find. Ref wins over
// checkout hints; without either, Default applies.
type FeedbackQuery struct {
	// Ref is a review or comment link (/streams/{id} or /contexts/{id},
	// optionally with ?comment={id}), a comment ID or a unique comment ID
	// prefix of at least MinCommentPrefix characters.
	Ref string
	// Checkout hints select stream heads; Repository is a repository name
	// or remote URL.
	Repository, Branch, Worktree, Hostname string
	// Default is the context used without a ref or hints, if any.
	Default         string
	IncludeResolved bool
}

// Feedback is a review's threads, or the candidate heads of ambiguous
// checkout hints with no review chosen.
type Feedback struct {
	Context contextservice.Context
	// Focus is the comment a reference named; Threads then holds only it
	// and OtherOpen counts the review's other open threads.
	Focus      string
	Threads    []Comment
	OtherOpen  int
	Candidates []contextservice.Context
}

// FindFeedback resolves a reference, checkout hints or the default to one
// review of the owner and returns its threads. Stream links and comment IDs
// follow the stream's latest head; context links pin that context.
func (catalog *Catalog) FindFeedback(ctx context.Context, query FeedbackQuery) (Feedback, error) {
	contextID, focus := query.Default, ""
	var err error
	switch {
	case strings.TrimSpace(query.Ref) != "":
		contextID, focus, err = catalog.resolveRef(ctx, strings.TrimSpace(query.Ref))
	case query.Repository != "" || query.Branch != "" || query.Worktree != "" || query.Hostname != "":
		var heads []contextservice.Context
		heads, err = catalog.checkoutHeads(ctx, query)
		if err == nil && len(heads) != 1 {
			if len(heads) == 0 {
				return Feedback{}, review.Error(404, "No review matches those checkout hints; use list_contexts to search reviews")
			}
			return Feedback{Candidates: heads}, nil
		}
		if err == nil {
			contextID = heads[0].ID
		}
	case contextID == "":
		return Feedback{}, review.Error(400, "Pass ref (a diffx review or comment link, or a comment ID) or checkout hints (repository, branch, worktree, hostname)")
	}
	if err != nil {
		return Feedback{}, err
	}
	service, err := catalog.Review(ctx, contextID)
	if err != nil {
		return Feedback{}, err
	}
	threads, others, err := service.Threads(ctx, focus, query.IncludeResolved)
	if err != nil {
		return Feedback{}, err
	}
	presented, err := catalog.provider.Get(ctx, contextID)
	if err != nil {
		return Feedback{}, err
	}
	return Feedback{Context: presented, Focus: focus, Threads: threads, OtherOpen: others}, nil
}

// resolveRef returns the context a reference names and the comment it focuses.
func (catalog *Catalog) resolveRef(ctx context.Context, ref string) (string, string, error) {
	if !strings.Contains(ref, "/") {
		if len(ref) < MinCommentPrefix {
			return "", "", review.Error(400, "Comment ID prefixes need at least %d characters", MinCommentPrefix)
		}
		return catalog.commentHead(ctx, ref)
	}
	unrecognized := review.Error(400, "Unrecognized diffx link; pass a /streams/{id} or /contexts/{id} link, or a comment ID")
	link, err := url.Parse(ref)
	if err != nil {
		return "", "", unrecognized
	}
	segments := strings.Split(strings.Trim(link.EscapedPath(), "/"), "/")
	if len(segments) < 2 {
		return "", "", unrecognized
	}
	kind := segments[len(segments)-2]
	id, err := url.PathUnescape(segments[len(segments)-1])
	if err != nil || id == "" || kind != "streams" && kind != "contexts" {
		return "", "", unrecognized
	}
	focus := link.Query().Get("comment")
	if kind == "contexts" {
		return id, focus, nil
	}
	head, err := catalog.streamHead(ctx, id)
	return head.ID, focus, err
}

// streamHead returns the latest retained head of the owner's stream.
func (catalog *Catalog) streamHead(ctx context.Context, streamKey string) (contextservice.Context, error) {
	id, err := catalog.store.StreamContext(ctx, catalog.provider.UserID(), streamKey)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return contextservice.Context{}, review.Error(404, "Review stream not found")
	}
	if err != nil {
		return contextservice.Context{}, err
	}
	found, err := catalog.provider.Get(ctx, id)
	if err != nil {
		return contextservice.Context{}, err
	}
	if latest := found.LatestContextID; latest != nil && *latest != "" && *latest != found.ID {
		if head, err := catalog.provider.Get(ctx, *latest); err == nil {
			return head, nil
		}
	}
	return found, nil
}

// commentHead finds the one comment a full ID or unique prefix names among
// the owner's visible streams and returns its stream's head.
func (catalog *Catalog) commentHead(ctx context.Context, ref string) (string, string, error) {
	streams, err := catalog.store.CommentStreams(ctx, catalog.provider.UserID(), ref, maxCandidates+1)
	if err != nil {
		return "", "", err
	}
	type match struct {
		commentID string
		head      contextservice.Context
	}
	matches := make([]match, 0, len(streams))
	for _, stream := range streams {
		head, err := catalog.streamHead(ctx, stream.StreamKey)
		var request *review.RequestError
		if errors.As(err, &request) && (request.Status == 404 || request.Status == 410) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if stream.CommentID == ref {
			return head.ID, ref, nil
		}
		matches = append(matches, match{commentID: stream.CommentID, head: head})
	}
	switch len(matches) {
	case 0:
		return "", "", review.Error(404, "No review comment matches %q", ref)
	case 1:
		return matches[0].head.ID, matches[0].commentID, nil
	}
	var message strings.Builder
	fmt.Fprintf(&message, "Comment ID prefix %q matches several comments:\n", ref)
	for index, candidate := range matches {
		if index == maxCandidates {
			message.WriteString("- and more\n")
			break
		}
		path := ""
		if comments, err := catalog.store.Comments(ctx, candidate.head.ID); err == nil {
			for _, comment := range comments {
				if comment.ID == candidate.commentID {
					path = comment.Path
				}
			}
		}
		fmt.Fprintf(&message, "- %s in %s at %s\n", candidate.commentID, describe(candidate.head), path)
	}
	message.WriteString("Pass more characters of the comment ID, or the comment's link.")
	return "", "", review.Error(409, "%s", message.String())
}

// describe names a review by repository, branch and worktree.
func describe(head contextservice.Context) string {
	description := head.Name
	if head.Branch != nil && *head.Branch != "" {
		description += " (" + *head.Branch
		if head.WorktreeName != nil && *head.WorktreeName != "" {
			description += ", worktree " + *head.WorktreeName
		}
		description += ")"
	}
	return description
}

// checkoutHeads lists the latest heads matching checkout hints, at most
// maxCandidates+1 of them.
func (catalog *Catalog) checkoutHeads(ctx context.Context, query FeedbackQuery) ([]contextservice.Context, error) {
	filter := ingestion.Filter{Branch: query.Branch, Worktree: query.Worktree, Hostname: query.Hostname, Latest: true}
	heads := make([]contextservice.Context, 0)
	cursor := ""
	for {
		page, err := catalog.provider.ListFiltered(ctx, 500, cursor, filter)
		if err != nil {
			return nil, err
		}
		for _, head := range page.Contexts {
			if repositoryMatches(head, query.Repository) {
				heads = append(heads, head)
			}
			if len(heads) > maxCandidates {
				return heads, nil
			}
		}
		if page.NextCursor == nil {
			return heads, nil
		}
		cursor = *page.NextCursor
	}
}

// repositoryMatches compares a hint with a head's repository name, its remote
// name, or its remote URL in any of Git's URL forms.
func repositoryMatches(head contextservice.Context, hint string) bool {
	if hint == "" {
		return true
	}
	if head.Observation == nil {
		return false
	}
	if strings.EqualFold(hint, head.Name) || strings.EqualFold(hint, head.Observation.RepositoryName) {
		return true
	}
	key := remoteKey(hint)
	return key != "" && key == remoteKey(head.Observation.RemoteURL)
}

// remoteKey is a remote URL's lowercase host and repository path, so HTTPS,
// SSH and scp-like forms of one remote compare equal.
func remoteKey(remote string) string {
	host, path := "", ""
	if parsed, err := url.Parse(remote); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		host, path = parsed.Hostname(), parsed.Path
	} else if before, after, ok := strings.Cut(remote, ":"); ok && !strings.Contains(before, "/") {
		host, path = before[strings.LastIndex(before, "@")+1:], after
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}

// Threads returns this context's open threads, or every thread when
// includeResolved. With focus it returns only that thread, whatever its
// status, and counts the other open threads.
func (service *Service) Threads(ctx context.Context, focus string, includeResolved bool) ([]Comment, int, error) {
	if err := service.requireComments(); err != nil {
		return nil, 0, err
	}
	stored, err := service.store.Comments(ctx, service.session.ContextID)
	if err != nil {
		return nil, 0, err
	}
	selected := make([]review.ReviewComment, 0, len(stored))
	others := 0
	for _, comment := range stored {
		switch {
		case focus != "" && comment.ID == focus:
			selected = append(selected, comment)
		case focus != "":
			if comment.Status == "open" {
				others++
			}
		case includeResolved || comment.Status == "open":
			selected = append(selected, comment)
		}
	}
	if focus != "" && len(selected) == 0 {
		return nil, 0, &CommentNotFoundError{CommentID: focus}
	}
	comments, err := service.project(ctx, selected)
	return comments, others, err
}

// PathCommentInput places a new comment by path. An empty Scope selects the
// first enabled scope whose snapshot contains the file, preferring all.
// Target "file" comments on the whole file; otherwise Side, Start and End
// select lines.
type PathCommentInput struct {
	Path       string
	Scope      review.DiffMode
	Target     string
	Side       string
	Start, End int
	Body       string
}

// CreatePathComment creates a comment on a file of this context's current
// snapshot, capturing its code and anchor like every other comment.
func (service *Service) CreatePathComment(ctx context.Context, input PathCommentInput, author review.CommentAuthor) (review.ReviewComment, error) {
	if err := service.requireComments(); err != nil {
		return review.ReviewComment{}, err
	}
	scopes := []review.DiffMode{review.DiffAll}
	if input.Scope != "" {
		if _, err := review.ParseDiffMode(string(input.Scope)); err != nil {
			return review.ReviewComment{}, review.Error(400, "Invalid diff scope")
		}
		if !service.session.Capabilities.Diff.Scopes.Allows(input.Scope) {
			return review.ReviewComment{}, reviewsession.NotEnabled(reviewsession.DiffScopes)
		}
		scopes = []review.DiffMode{input.Scope}
	} else {
		for _, mode := range service.session.Capabilities.Diff.Scopes.Values {
			if mode != review.DiffAll {
				scopes = append(scopes, mode)
			}
		}
	}
	for _, mode := range scopes {
		if !service.session.Capabilities.Diff.Scopes.Allows(mode) {
			continue
		}
		snapshot, err := service.Snapshot(ctx, mode)
		if err != nil {
			return review.ReviewComment{}, err
		}
		for _, file := range snapshot.Files {
			if file.Path != input.Path {
				continue
			}
			side, start, end := input.Side, input.Start, input.End
			if input.Target == "file" {
				side, start, end = "additions", 0, 0
			}
			return service.CreateComment(ctx, CreateCommentInput{
				DiffID: snapshot.ID, VersionID: snapshot.VersionID, FileID: file.ID, Scope: mode, FileVersion: file.Fingerprint,
				Target: input.Target, Side: side, Start: start, End: end, Body: input.Body,
			}, author)
		}
	}
	return review.ReviewComment{}, review.Error(404, "File %q is not in this review's diff", input.Path)
}
