package reviewservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/session"
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

type Service struct {
	session session.Session
	store   MutationStore
}

func New(active session.Session, store MutationStore) *Service {
	service := &Service{session: active, store: store}
	return service
}

// ListComments returns the stream's comments placed in this context. Only open
// comments that render on current lines are actionable.
func (service *Service) ListComments(ctx context.Context, includeResolved bool) ([]Comment, error) {
	if err := service.requireComments(); err != nil {
		return nil, err
	}
	storedComments, err := service.store.Comments(ctx, service.session.ContextID)
	if err != nil {
		return nil, err
	}
	selected := make([]review.ReviewComment, 0, len(storedComments))
	for _, comment := range storedComments {
		if includeResolved || comment.Status == "open" {
			selected = append(selected, comment)
		}
	}
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

func (service *Service) ResolveComment(ctx context.Context, commentID string) (Resolution, error) {
	if err := service.requireComments(); err != nil {
		return Resolution{}, err
	}
	comments, err := service.store.Comments(ctx, service.session.ContextID)
	if err != nil {
		return Resolution{}, err
	}
	for _, comment := range comments {
		if comment.ID != commentID {
			continue
		}
		if comment.Status != "resolved" {
			comment.Status = "resolved"
			replaced, err := service.store.ReplaceComment(ctx, service.session.ContextID, comment)
			if err != nil {
				return Resolution{}, err
			}
			if !replaced {
				return Resolution{}, &CommentNotFoundError{CommentID: commentID}
			}
		}
		return Resolution{CommentID: comment.ID, Status: "resolved"}, nil
	}
	return Resolution{}, &CommentNotFoundError{CommentID: commentID}
}

func (service *Service) requireComments() error {
	if !service.session.Capabilities.Review.Comments.Enabled() {
		return session.NotEnabled(session.ReviewComments)
	}
	return nil
}
