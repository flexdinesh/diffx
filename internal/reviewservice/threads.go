package reviewservice

import (
	"context"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
)

// AddReply appends author's reply to a comment's thread and returns the placed
// comment. Agents and the user may both reply; replies are one level deep.
func (service *Service) AddReply(ctx context.Context, commentID, body string, author review.CommentAuthor) (review.ReviewComment, error) {
	if err := service.requireComments(); err != nil {
		return review.ReviewComment{}, err
	}
	if !author.Valid() {
		return review.ReviewComment{}, review.Error(400, "Invalid comment author")
	}
	if body = strings.TrimSpace(body); body == "" {
		return review.ReviewComment{}, review.Error(400, "Invalid reply")
	}
	id, err := randomID()
	if err != nil {
		return review.ReviewComment{}, err
	}
	reply := review.CommentReply{ID: id, Author: author, Body: body, CreatedAt: float64(time.Now().UnixMilli())}
	added, err := service.store.AddReply(ctx, service.session.ContextID, commentID, reply)
	if err != nil {
		return review.ReviewComment{}, err
	}
	if !added {
		return review.ReviewComment{}, review.Error(404, "Comment not found")
	}
	service.changed()
	return service.placedComment(ctx, commentID)
}

// EditReply replaces the body of one of the user's own replies. Agent replies
// are append-only: a user edit would misattribute them.
func (service *Service) EditReply(ctx context.Context, commentID, replyID, body string) (review.ReviewComment, error) {
	if err := service.requireComments(); err != nil {
		return review.ReviewComment{}, err
	}
	if body = strings.TrimSpace(body); body == "" {
		return review.ReviewComment{}, review.Error(400, "Invalid reply")
	}
	reply, err := service.reply(ctx, commentID, replyID)
	if err != nil {
		return review.ReviewComment{}, err
	}
	if reply.Author.Kind != review.AuthorUser {
		return review.ReviewComment{}, review.Error(403, "Agent replies cannot be edited")
	}
	edited, err := service.store.EditReply(ctx, service.session.ContextID, commentID, replyID, body, float64(time.Now().UnixMilli()))
	if err != nil {
		return review.ReviewComment{}, err
	}
	if !edited {
		return review.ReviewComment{}, review.Error(404, "Reply not found")
	}
	service.changed()
	return service.placedComment(ctx, commentID)
}

// DeleteReply removes any reply of a comment's thread, including an agent's,
// because the stream belongs to the user.
func (service *Service) DeleteReply(ctx context.Context, commentID, replyID string) error {
	if err := service.requireComments(); err != nil {
		return err
	}
	deleted, err := service.store.DeleteReply(ctx, service.session.ContextID, commentID, replyID)
	if err != nil {
		return err
	}
	if !deleted {
		return review.Error(404, "Reply not found")
	}
	service.changed()
	return nil
}

// reply finds one reply of a stream comment.
func (service *Service) reply(ctx context.Context, commentID, replyID string) (review.CommentReply, error) {
	comments, err := service.store.Comments(ctx, service.session.ContextID)
	if err != nil {
		return review.CommentReply{}, err
	}
	for _, comment := range comments {
		if comment.ID != commentID {
			continue
		}
		for _, reply := range comment.Replies {
			if reply.ID == replyID {
				return reply, nil
			}
		}
		return review.CommentReply{}, review.Error(404, "Reply not found")
	}
	return review.CommentReply{}, review.Error(404, "Comment not found")
}
