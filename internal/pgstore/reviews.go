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

// Comments returns the stream's comments while the context is retained.
func (store *Store) Comments(ctx context.Context, contextID string) ([]review.ReviewComment, error) {
	rows, err := store.queries.Comments(ctx, pgdb.CommentsParams{ContextID: contextID, Now: time.Now()})
	if err != nil {
		return nil, err
	}
	comments := make([]review.ReviewComment, 0, len(rows))
	for _, raw := range rows {
		var comment review.ReviewComment
		if err := json.Unmarshal(raw, &comment); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, nil
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
		comment.Placement = nil
		raw, err := json.Marshal(comment)
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
		written = true
		return nil
	})
	return written, err
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
	deleted, err := store.queries.DeleteStreamComments(ctx, pgdb.DeleteStreamCommentsParams{Ids: ids, ContextID: contextID})
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
