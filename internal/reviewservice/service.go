package reviewservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

var ErrCommentNotFound = errors.New("review comment not found")

type CommentNotFoundError struct {
	CommentID string
}

func (err *CommentNotFoundError) Error() string {
	return fmt.Sprintf("review comment %q not found", err.CommentID)
}

func (err *CommentNotFoundError) Unwrap() error {
	return ErrCommentNotFound
}

type CommentStore interface {
	Comments(context.Context, string) ([]review.ReviewComment, error)
	PutComment(context.Context, string, review.ReviewComment) error
	ReplaceComment(context.Context, string, review.ReviewComment) (bool, error)
}

type Comment struct {
	review.ReviewComment
	Applicability review.Applicability `json:"applicability"`
	Actionable    bool                 `json:"actionable"`
}

type Resolution struct {
	CommentID string `json:"comment_id"`
	Status    string `json:"status"`
}

// CommentEvents hears committed comment, reply and resolution changes of a
// context's stream. Compositions whose store announces changes ignore it.
type CommentEvents interface {
	CommentsChanged(contextID, streamID string)
}

type Service struct {
	session reviewsession.Session
	store   MutationStore
	events  CommentEvents
}

func New(active reviewsession.Session, store MutationStore) *Service {
	service := &Service{session: active, store: store}
	return service
}

// changed announces a committed comment change of this context's stream.
func (service *Service) changed() {
	if service.events != nil {
		service.events.CommentsChanged(service.session.ContextID, service.session.StreamID)
	}
}

// ParseAuthorFilter accepts an empty filter or a comment author kind.
func ParseAuthorFilter(value string) (string, error) {
	if value != "" && value != review.AuthorUser && value != review.AuthorAgent {
		return "", review.Error(400, "Invalid author filter")
	}
	return value, nil
}

// writtenBy reports whether a comment's author matches a parsed filter.
func writtenBy(comment review.ReviewComment, author string) bool {
	return author == "" || comment.Author != nil && comment.Author.Kind == author
}

// ListComments returns the stream's comments placed in this context, optionally
// only those written by one author kind. Only open comments that render on
// current lines are actionable.
func (service *Service) ListComments(ctx context.Context, includeResolved bool, author string) ([]Comment, error) {
	return service.FilterComments(ctx, CommentFilter{IncludeResolved: includeResolved, Author: author})
}

// CommentFilter selects stream comments. Every set field must match.
type CommentFilter struct {
	IncludeResolved bool
	// Author is empty or a comment author kind; replies do not affect it.
	Author string
	Path   string
	// CommentID selects one comment, whatever its status.
	CommentID string
}

// FilterComments returns the stream's comments matching filter, placed in this
// context.
func (service *Service) FilterComments(ctx context.Context, filter CommentFilter) ([]Comment, error) {
	if err := service.requireComments(); err != nil {
		return nil, err
	}
	author, err := ParseAuthorFilter(filter.Author)
	if err != nil {
		return nil, err
	}
	storedComments, err := service.store.Comments(ctx, service.session.ContextID)
	if err != nil {
		return nil, err
	}
	selected := make([]review.ReviewComment, 0, len(storedComments))
	for _, comment := range storedComments {
		status := filter.IncludeResolved || filter.CommentID != "" || comment.Status == "open"
		if status && writtenBy(comment, author) && (filter.Path == "" || comment.Path == filter.Path) && (filter.CommentID == "" || comment.ID == filter.CommentID) {
			selected = append(selected, comment)
		}
	}
	return service.project(ctx, selected)
}

// project places stored comments in this context and marks the actionable ones.
func (service *Service) project(ctx context.Context, selected []review.ReviewComment) ([]Comment, error) {
	if err := service.place(ctx, selected); err != nil {
		return nil, err
	}
	comments := make([]Comment, 0, len(selected))
	for _, comment := range selected {
		applicability := comment.Placement.Applicability
		comments = append(comments, Comment{
			ReviewComment: comment,
			Applicability: applicability,
			Actionable:    comment.Status == "open" && applicability.Inline(),
		})
	}
	return comments, nil
}

// place locates stream comments in this context's snapshot of each comment's
// scope, loading each snapshot and file patch at most once.
func (service *Service) place(ctx context.Context, comments []review.ReviewComment) error {
	snapshots := make(map[review.DiffMode]*review.RepositoryDiff)
	patches := make(map[review.DiffMode]map[string]review.FilePatch)
	for index := range comments {
		mode := comments[index].Scope
		viewed, loaded := snapshots[mode]
		if !loaded {
			if service.session.Capabilities.Diff.Scopes.Allows(mode) {
				snapshot, err := service.Snapshot(ctx, mode)
				if err != nil {
					return err
				}
				viewed = &snapshot
			}
			snapshots[mode] = viewed
			patches[mode] = make(map[string]review.FilePatch)
		}
		cache := patches[mode]
		placement, err := review.Locate(comments[index], viewed, func(file review.ChangedFile) (review.FilePatch, error) {
			if patch, ok := cache[file.ID]; ok {
				return patch, nil
			}
			patch, err := service.session.Source.Patch(ctx, mode, file, viewed.Head)
			if err != nil {
				return review.FilePatch{}, err
			}
			cache[file.ID] = patch
			return patch, nil
		})
		if err != nil {
			return err
		}
		comments[index].Placement = &placement
	}
	return nil
}

// ResolveComment resolves a comment idempotently, recording its resolver.
// A non-empty note joins the thread as the resolver's reply in the same
// transaction, also when the comment was already resolved.
func (service *Service) ResolveComment(ctx context.Context, commentID string, author review.CommentAuthor, note string) (Resolution, error) {
	if err := service.requireComments(); err != nil {
		return Resolution{}, err
	}
	if !author.Valid() {
		return Resolution{}, review.Error(400, "Invalid comment author")
	}
	now := float64(time.Now().UnixMilli())
	var reply *review.CommentReply
	if note = strings.TrimSpace(note); note != "" {
		id, err := randomID()
		if err != nil {
			return Resolution{}, err
		}
		reply = &review.CommentReply{ID: id, Author: author, Body: note, CreatedAt: now}
	}
	resolved, err := service.store.ResolveComment(ctx, service.session.ContextID, commentID, author, now, reply)
	if err != nil {
		return Resolution{}, err
	}
	if !resolved {
		return Resolution{}, &CommentNotFoundError{CommentID: commentID}
	}
	service.changed()
	return Resolution{CommentID: commentID, Status: "resolved"}, nil
}

func (service *Service) requireComments() error {
	if !service.session.Capabilities.Review.Comments.Enabled() {
		return reviewsession.NotEnabled(reviewsession.ReviewComments)
	}
	return nil
}
