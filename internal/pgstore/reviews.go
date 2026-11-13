package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewservice"
	"github.com/jackc/pgx/v5"
)

var (
	_ reviewservice.CommentStore  = (*Store)(nil)
	_ reviewservice.MutationStore = (*Store)(nil)
)

// reviewFor resolves a retained context's scope to its stream review.
func reviewFor(ctx context.Context, queries *pgdb.Queries, contextID string, mode review.DiffMode) (string, string, error) {
	row, err := queries.ScopeReview(ctx, pgdb.ScopeReviewParams{Mode: string(mode), ContextID: contextID, Now: time.Now()})
	if err != nil {
		return "", "", noRows(err)
	}
	return row.DiffID, row.ReviewID, nil
}

func versionFor(ctx context.Context, queries *pgdb.Queries, diffID, versionID string) error {
	if versionID == "" {
		return nil
	}
	exists, err := queries.VersionInDiff(ctx, pgdb.VersionInDiffParams{ID: versionID, DiffID: diffID})
	if err != nil {
		return err
	}
	if exists != 1 {
		return errors.New("version belongs to a different diff")
	}
	return nil
}

// Comments returns the stream's comments with their replies while the context is retained.
func (store *Store) Comments(ctx context.Context, contextID string) ([]review.ReviewComment, error) {
	var comments []review.ReviewComment
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		replyRows, err := queries.StreamReplies(ctx, contextID)
		if err != nil {
			return err
		}
		replies := make(map[string][]review.CommentReply)
		for _, row := range replyRows {
			var reply review.CommentReply
			if err := json.Unmarshal(row.Data, &reply); err != nil {
				return err
			}
			replies[row.CommentID] = append(replies[row.CommentID], reply)
		}
		rows, err := queries.Comments(ctx, pgdb.CommentsParams{ContextID: contextID, Now: time.Now()})
		if err != nil {
			return err
		}
		comments = make([]review.ReviewComment, 0, len(rows))
		for _, row := range rows {
			comment, err := review.StoredComment(row.Data, replies[row.ID])
			if err != nil {
				return err
			}
			comments = append(comments, comment)
		}
		return nil
	})
	return comments, err
}

// PutComment stores a comment in the context's stream review. New references
// must name this context's snapshot; an existing comment may keep the snapshot
// it was written against.
func (store *Store) PutComment(ctx context.Context, contextID string, comment review.ReviewComment) error {
	_, err := store.writeComment(ctx, contextID, comment, false)
	return err
}

// ReplaceComment updates an existing stream comment and reports false when it
// no longer exists, so an edit racing a delete cannot recreate the comment.
func (store *Store) ReplaceComment(ctx context.Context, contextID string, comment review.ReviewComment) (bool, error) {
	return store.writeComment(ctx, contextID, comment, true)
}

func (store *Store) writeComment(ctx context.Context, contextID string, comment review.ReviewComment, existing bool) (bool, error) {
	if strings.ContainsRune(comment.ID+comment.Path+comment.DiffID+comment.VersionID, 0) {
		return false, ErrNUL
	}
	written := false
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		diffID, reviewID, err := reviewFor(ctx, queries, contextID, comment.Scope)
		if err != nil {
			return err
		}
		if comment.DiffID == "" {
			comment.DiffID = diffID
		}
		if comment.DiffID == diffID {
			if err := versionFor(ctx, queries, diffID, comment.VersionID); err != nil {
				return err
			}
		} else {
			raw, err := queries.StoredComment(ctx, pgdb.StoredCommentParams{ID: comment.ID, ReviewID: reviewID})
			if errors.Is(err, pgx.ErrNoRows) {
				if existing {
					return nil
				}
				return errors.New("comment belongs to a different diff")
			}
			if err != nil {
				return err
			}
			var stored review.ReviewComment
			if err := json.Unmarshal(raw, &stored); err != nil {
				return err
			}
			if stored.DiffID != comment.DiffID || stored.VersionID != comment.VersionID {
				return errors.New("comment belongs to a different diff")
			}
		}
		raw, err := review.StorableComment(comment)
		if err != nil {
			return err
		}
		var changed int64
		if existing {
			changed, err = queries.UpdateComment(ctx, pgdb.UpdateCommentParams{VersionID: comment.VersionID, FileID: comment.Path, Data: raw, ID: comment.ID, ReviewID: reviewID})
		} else {
			changed, err = queries.InsertComment(ctx, pgdb.InsertCommentParams{ID: comment.ID, ReviewID: reviewID, VersionID: comment.VersionID, FileID: comment.Path, Data: raw, CreatedAt: int64(comment.CreatedAt)})
		}
		if err != nil {
			return err
		}
		if changed != 1 {
			if existing {
				return nil
			}
			return errors.New("comment ID belongs to another review")
		}
		// Imported threads keep their replies; existing replies are never overwritten.
		if !existing {
			for _, reply := range comment.Replies {
				if containsNUL(reply.ID) {
					return ErrNUL
				}
				raw, err := json.Marshal(reply)
				if err != nil {
					return err
				}
				if err := queries.ImportReply(ctx, pgdb.ImportReplyParams{CommentID: comment.ID, ID: reply.ID, Data: raw, CreatedAt: int64(reply.CreatedAt)}); err != nil {
					return err
				}
			}
		}
		written = true
		return notifyComments(ctx, queries, contextID)
	})
	return written, err
}

// lockComment locks a stream comment for a thread write; found is false when
// the comment no longer exists.
func lockComment(ctx context.Context, queries *pgdb.Queries, contextID, commentID string) (review.ReviewComment, bool, error) {
	raw, err := queries.LockStreamComment(ctx, pgdb.LockStreamCommentParams{ID: commentID, ContextID: contextID})
	if errors.Is(err, pgx.ErrNoRows) {
		return review.ReviewComment{}, false, nil
	}
	if err != nil {
		return review.ReviewComment{}, false, err
	}
	comment, err := review.StoredComment(raw, nil)
	return comment, err == nil, err
}

func containsNUL(values ...string) bool {
	return strings.ContainsRune(strings.Join(values, ""), 0)
}

func insertReply(ctx context.Context, queries *pgdb.Queries, commentID string, reply review.CommentReply) error {
	if containsNUL(reply.ID) {
		return ErrNUL
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	inserted, err := queries.InsertReply(ctx, pgdb.InsertReplyParams{CommentID: commentID, ID: reply.ID, Data: raw, CreatedAt: int64(reply.CreatedAt)})
	if err == nil && inserted != 1 {
		err = errors.New("reply was not stored")
	}
	return err
}

// AddReply appends a reply to a stream comment and reports false when the
// comment no longer exists.
func (store *Store) AddReply(ctx context.Context, contextID, commentID string, reply review.CommentReply) (bool, error) {
	if containsNUL(commentID) {
		return false, ErrNUL
	}
	added := false
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		if _, found, err := lockComment(ctx, queries, contextID, commentID); err != nil || !found {
			return err
		}
		if err := insertReply(ctx, queries, commentID, reply); err != nil {
			return err
		}
		added = true
		return notifyComments(ctx, queries, contextID)
	})
	return added, err
}

// EditReply replaces a reply's body and reports false when it no longer exists.
func (store *Store) EditReply(ctx context.Context, contextID, commentID, replyID, body string, editedAt float64) (bool, error) {
	if containsNUL(commentID, replyID) {
		return false, ErrNUL
	}
	edited := false
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		raw, err := queries.LockStreamReply(ctx, pgdb.LockStreamReplyParams{CommentID: commentID, ID: replyID, ContextID: contextID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var reply review.CommentReply
		if err := json.Unmarshal(raw, &reply); err != nil {
			return err
		}
		reply.Body, reply.EditedAt = body, editedAt
		updated, err := json.Marshal(reply)
		if err != nil {
			return err
		}
		if err := queries.SetReplyData(ctx, pgdb.SetReplyDataParams{Data: updated, CommentID: commentID, ID: replyID}); err != nil {
			return err
		}
		edited = true
		return notifyComments(ctx, queries, contextID)
	})
	return edited, err
}

// DeleteReply removes one reply of a stream comment.
func (store *Store) DeleteReply(ctx context.Context, contextID, commentID, replyID string) (bool, error) {
	if containsNUL(commentID, replyID) {
		return false, ErrNUL
	}
	deleted := false
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		count, err := queries.DeleteStreamReply(ctx, pgdb.DeleteStreamReplyParams{CommentID: commentID, ID: replyID, ContextID: contextID})
		if err != nil || count != 1 {
			return err
		}
		deleted = true
		return notifyComments(ctx, queries, contextID)
	})
	return deleted, err
}

// ResolveComment resolves a stream comment and appends an optional note in one
// transaction. An already resolved comment keeps its resolution and still
// gains the note. It reports false when the comment no longer exists.
func (store *Store) ResolveComment(ctx context.Context, contextID, commentID string, by review.CommentAuthor, at float64, note *review.CommentReply) (bool, error) {
	if containsNUL(commentID) {
		return false, ErrNUL
	}
	resolved := false
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		comment, found, err := lockComment(ctx, queries, contextID, commentID)
		if err != nil || !found {
			return err
		}
		if comment.Status != "resolved" {
			comment.Status, comment.ResolvedBy, comment.ResolvedAt = "resolved", &by, at
			raw, err := review.StorableComment(comment)
			if err != nil {
				return err
			}
			if err := queries.SetCommentData(ctx, pgdb.SetCommentDataParams{Data: raw, ID: commentID}); err != nil {
				return err
			}
		}
		if note != nil {
			if err := insertReply(ctx, queries, commentID, *note); err != nil {
				return err
			}
		}
		resolved = true
		return notifyComments(ctx, queries, contextID)
	})
	return resolved, err
}

func (store *Store) ImportComments(ctx context.Context, contextID string, comments []review.ReviewComment) ([]review.ReviewComment, error) {
	for _, comment := range comments {
		if !comment.Valid() {
			return nil, errors.New("invalid comment")
		}
	}
	existing, err := store.Comments(ctx, contextID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing)+len(comments))
	for _, comment := range existing {
		seen[comment.ID] = true
	}
	for _, comment := range comments {
		// Imports preserve existing IDs and never overwrite local edits.
		if !seen[comment.ID] {
			if err := store.PutComment(ctx, contextID, comment); err != nil {
				return nil, err
			}
			seen[comment.ID] = true
		}
	}
	return store.Comments(ctx, contextID)
}

func (store *Store) DeleteComments(ctx context.Context, contextID string, selected map[string]bool) (int, error) {
	ids := make([]string, 0, len(selected))
	for id, include := range selected {
		if include {
			ids = append(ids, id)
		}
	}
	var deleted int64
	err := store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		var err error
		deleted, err = queries.DeleteStreamComments(ctx, pgdb.DeleteStreamCommentsParams{Ids: ids, ContextID: contextID})
		if err != nil || deleted == 0 {
			return err
		}
		return notifyComments(ctx, queries, contextID)
	})
	return int(deleted), err
}

func (store *Store) DeleteComment(ctx context.Context, contextID, commentID string) (bool, error) {
	deleted, err := store.DeleteComments(ctx, contextID, map[string]bool{commentID: true})
	return deleted == 1, err
}

// Marks returns stream marks whose file patch is unchanged in this context,
// projected onto this context's snapshot.
func (store *Store) Marks(ctx context.Context, contextID string, scope review.DiffMode) ([]review.ReviewMark, error) {
	rows, err := store.queries.Marks(ctx, pgdb.MarksParams{Mode: string(scope), ContextID: contextID, Now: time.Now()})
	if err != nil {
		return nil, err
	}
	marks := make([]review.ReviewMark, 0, len(rows))
	for _, row := range rows {
		marks = append(marks, review.ReviewMark{Scope: scope, DiffID: row.DiffID, VersionID: row.VersionID, FileID: row.FileID, FileVersion: row.Fingerprint})
	}
	return marks, nil
}

func (store *Store) PutMark(ctx context.Context, contextID string, mark review.ReviewMark) error {
	if strings.ContainsRune(mark.FileID+mark.DiffID+mark.VersionID, 0) {
		return ErrNUL
	}
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		diffID, reviewID, err := reviewFor(ctx, queries, contextID, mark.Scope)
		if err != nil {
			return err
		}
		if mark.DiffID != "" && mark.DiffID != diffID {
			return errors.New("mark belongs to a different diff")
		}
		if err := versionFor(ctx, queries, diffID, mark.VersionID); err != nil {
			return err
		}
		digest, err := queries.ScopeFileDigest(ctx, pgdb.ScopeFileDigestParams{ContextID: contextID, Mode: string(mark.Scope), FileID: mark.FileID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("mark file is not in the diff")
		}
		if err != nil {
			return err
		}
		mark.DiffID = diffID
		raw, err := json.Marshal(mark)
		if err != nil {
			return err
		}
		var versionID *string
		if mark.VersionID != "" {
			versionID = &mark.VersionID
		}
		return queries.UpsertMark(ctx, pgdb.UpsertMarkParams{ReviewID: reviewID, FileID: mark.FileID, VersionID: versionID, PatchDigest: digest, Data: raw})
	})
}

// DeleteMark and ClearMarks remove only marks visible in this context.
func (store *Store) DeleteMark(ctx context.Context, contextID string, scope review.DiffMode, fileID string) error {
	return store.queries.DeleteVisibleMarks(ctx, pgdb.DeleteVisibleMarksParams{Mode: string(scope), ContextID: contextID, FileID: &fileID})
}

func (store *Store) ClearMarks(ctx context.Context, contextID string, scope review.DiffMode) error {
	return store.queries.DeleteVisibleMarks(ctx, pgdb.DeleteVisibleMarksParams{Mode: string(scope), ContextID: contextID})
}

func (store *Store) StoredVersion(ctx context.Context, ownerID, diffID, versionID string, now time.Time) (review.RepositoryDiff, error) {
	row, err := store.queries.StoredVersion(ctx, pgdb.StoredVersionParams{OwnerID: ownerID, DiffID: diffID, VersionID: versionID, Now: now})
	if err != nil {
		return review.RepositoryDiff{}, noRows(err)
	}
	snapshot, err := decodeSnapshot(row.Manifest, row.Metadata, row.Modes)
	if err != nil {
		return review.RepositoryDiff{}, fmt.Errorf("invalid stored diff version: %w", err)
	}
	return snapshot, nil
}

// CommentStreams lists up to limit of the owner's comments whose ID starts
// with prefix, ordered by ID, with their stream keys.
func (store *Store) CommentStreams(ctx context.Context, ownerID, prefix string, limit int) ([]reviewdata.CommentStream, error) {
	rows, err := store.queries.CommentStreams(ctx, pgdb.CommentStreamsParams{OwnerID: ownerID, Prefix: prefix, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	streams := make([]reviewdata.CommentStream, 0, len(rows))
	for _, row := range rows {
		streams = append(streams, reviewdata.CommentStream{CommentID: row.CommentID, StreamKey: row.StreamKey})
	}
	return streams, nil
}

// StreamContext returns the most recently submitted retained context of the
// owner's stream.
func (store *Store) StreamContext(ctx context.Context, ownerID, streamKey string) (string, error) {
	id, err := store.queries.StreamContext(ctx, pgdb.StreamContextParams{OwnerID: ownerID, StreamKey: streamKey, Now: time.Now()})
	return id, noRows(err)
}
