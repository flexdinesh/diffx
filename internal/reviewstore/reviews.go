package reviewstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

// streamReviews selects the review IDs of a context's stream, across scopes.
const streamReviews = `SELECT r.id FROM reviews r JOIN observations o ON o.owner_id=r.owner_id AND o.stream_key=r.stream_key WHERE o.context_id=?`

// reviewFor resolves a retained context's scope to its stream review.
func (store *Store) reviewFor(transaction *sql.Tx, contextID string, mode review.DiffMode) (string, string, error) {
	var diffID, reviewID string
	err := transaction.QueryRow(`SELECT s.diff_id, r.id FROM observations o JOIN observation_scopes s ON s.context_id=o.context_id AND s.mode=? JOIN diffs d ON d.id=s.diff_id
		JOIN reviews r ON r.owner_id=o.owner_id AND r.stream_key=o.stream_key AND r.mode=s.mode
		WHERE o.context_id=? AND (d.expires_at IS NULL OR d.expires_at>?)`, mode, contextID, time.Now().UnixMilli()).Scan(&diffID, &reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return diffID, reviewID, err
}

func versionFor(transaction *sql.Tx, diffID, versionID string) error {
	if versionID == "" {
		return nil
	}
	var exists int
	if err := transaction.QueryRow(`SELECT COUNT(*) FROM diff_versions WHERE id=? AND diff_id=?`, versionID, diffID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return errors.New("version belongs to a different diff")
	}
	return nil
}

// Comments returns the stream's comments while the context is retained.
func (store *Store) Comments(contextID string) ([]review.ReviewComment, error) {
	rows, err := store.db.Query(`SELECT c.data FROM comments c JOIN reviews r ON r.id=c.review_id
		JOIN observations o ON o.owner_id=r.owner_id AND o.stream_key=r.stream_key
		JOIN contexts x ON x.id=o.context_id JOIN diffs d ON d.id=x.anchor_diff_id
		WHERE o.context_id=? AND (d.expires_at IS NULL OR d.expires_at>?) ORDER BY c.created_at, c.id`, contextID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := make([]review.ReviewComment, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var comment review.ReviewComment
		if err := json.Unmarshal([]byte(raw), &comment); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

// PutComment stores a comment in the context's stream review. New references
// must name this context's snapshot; an existing comment may keep the snapshot
// it was written against.
func (store *Store) PutComment(contextID string, comment review.ReviewComment) error {
	_, err := store.writeComment(contextID, comment, false)
	return err
}

// ReplaceComment updates an existing stream comment and reports false when it
// no longer exists, so an edit racing a delete cannot recreate the comment.
func (store *Store) ReplaceComment(contextID string, comment review.ReviewComment) (bool, error) {
	return store.writeComment(contextID, comment, true)
}

func (store *Store) writeComment(contextID string, comment review.ReviewComment, existing bool) (bool, error) {
	transaction, err := store.db.Begin()
	if err != nil {
		return false, err
	}
	defer transaction.Rollback()
	diffID, reviewID, err := store.reviewFor(transaction, contextID, comment.Scope)
	if err != nil {
		return false, err
	}
	if comment.DiffID == "" {
		comment.DiffID = diffID
	}
	if comment.DiffID == diffID {
		if err := versionFor(transaction, diffID, comment.VersionID); err != nil {
			return false, err
		}
	} else {
		var raw string
		err := transaction.QueryRow(`SELECT data FROM comments WHERE id=? AND review_id=?`, comment.ID, reviewID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			if existing {
				return false, nil
			}
			return false, errors.New("comment belongs to a different diff")
		}
		if err != nil {
			return false, err
		}
		var stored review.ReviewComment
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return false, err
		}
		if stored.DiffID != comment.DiffID || stored.VersionID != comment.VersionID {
			return false, errors.New("comment belongs to a different diff")
		}
	}
	comment.Placement = nil
	raw, err := json.Marshal(comment)
	if err != nil {
		return false, err
	}
	// Pruned snapshot versions leave the reference in data only.
	var result sql.Result
	if existing {
		result, err = transaction.Exec(`UPDATE comments SET version_id=(SELECT id FROM diff_versions WHERE id=?), file_id=?, data=? WHERE id=? AND review_id=?`, comment.VersionID, comment.Path, string(raw), comment.ID, reviewID)
	} else {
		result, err = transaction.Exec(`INSERT INTO comments(id, review_id, version_id, file_id, data, created_at) VALUES(?, ?, (SELECT id FROM diff_versions WHERE id=?), ?, ?, ?) ON CONFLICT(id) DO UPDATE SET version_id=excluded.version_id, file_id=excluded.file_id, data=excluded.data WHERE comments.review_id=excluded.review_id`, comment.ID, reviewID, comment.VersionID, comment.Path, string(raw), int64(comment.CreatedAt))
	}
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed != 1 {
		if existing {
			return false, nil
		}
		return false, errors.New("comment ID belongs to another review")
	}
	return true, transaction.Commit()
}

func (store *Store) ImportComments(contextID string, comments []review.ReviewComment) ([]review.ReviewComment, error) {
	for _, comment := range comments {
		if !comment.Valid() {
			return nil, errors.New("invalid comment")
		}
	}
	existing, err := store.Comments(contextID)
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
			if err := store.PutComment(contextID, comment); err != nil {
				return nil, err
			}
			seen[comment.ID] = true
		}
	}
	return store.Comments(contextID)
}

func (store *Store) DeleteComments(contextID string, selected map[string]bool) (int, error) {
	transaction, err := store.db.Begin()
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	deleted := 0
	for id, include := range selected {
		if !include {
			continue
		}
		result, err := transaction.Exec(`DELETE FROM comments WHERE id=? AND review_id IN (`+streamReviews+`)`, id, contextID)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += int(count)
	}
	return deleted, transaction.Commit()
}

func (store *Store) DeleteComment(contextID, commentID string) (bool, error) {
	deleted, err := store.DeleteComments(contextID, map[string]bool{commentID: true})
	return deleted == 1, err
}

// visibleMarks joins a context scope's files to stream marks with the same patch digest.
const visibleMarks = `FROM observations o JOIN observation_scopes s ON s.context_id=o.context_id AND s.mode=? JOIN diffs d ON d.id=s.diff_id
	JOIN reviews r ON r.owner_id=o.owner_id AND r.stream_key=o.stream_key AND r.mode=s.mode
	JOIN marks m ON m.review_id=r.id
	JOIN diff_files f ON f.version_id=s.version_id AND f.file_id=m.file_id AND f.patch_digest=m.patch_digest
	WHERE o.context_id=?`

// Marks returns stream marks whose file patch is unchanged in this context,
// projected onto this context's snapshot.
func (store *Store) Marks(contextID string, scope review.DiffMode) ([]review.ReviewMark, error) {
	rows, err := store.db.Query(`SELECT s.diff_id, s.version_id, m.file_id, f.fingerprint `+visibleMarks+` AND (d.expires_at IS NULL OR d.expires_at>?) ORDER BY m.file_id`, scope, contextID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	marks := make([]review.ReviewMark, 0)
	for rows.Next() {
		mark := review.ReviewMark{Scope: scope}
		if err := rows.Scan(&mark.DiffID, &mark.VersionID, &mark.FileID, &mark.FileVersion); err != nil {
			return nil, err
		}
		marks = append(marks, mark)
	}
	return marks, rows.Err()
}

func (store *Store) PutMark(contextID string, mark review.ReviewMark) error {
	transaction, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	diffID, reviewID, err := store.reviewFor(transaction, contextID, mark.Scope)
	if err != nil {
		return err
	}
	if mark.DiffID != "" && mark.DiffID != diffID {
		return errors.New("mark belongs to a different diff")
	}
	if err := versionFor(transaction, diffID, mark.VersionID); err != nil {
		return err
	}
	var digest string
	err = transaction.QueryRow(`SELECT f.patch_digest FROM observation_scopes s JOIN diff_files f ON f.version_id=s.version_id WHERE s.context_id=? AND s.mode=? AND f.file_id=?`, contextID, mark.Scope, mark.FileID).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
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
	var versionID any
	if mark.VersionID != "" {
		versionID = mark.VersionID
	}
	_, err = transaction.Exec(`INSERT INTO marks(review_id, file_id, version_id, patch_digest, data) VALUES(?, ?, ?, ?, ?) ON CONFLICT(review_id, file_id) DO UPDATE SET version_id=excluded.version_id, patch_digest=excluded.patch_digest, data=excluded.data`, reviewID, mark.FileID, versionID, digest, string(raw))
	if err != nil {
		return err
	}
	return transaction.Commit()
}

// DeleteMark and ClearMarks remove only marks visible in this context.
func (store *Store) DeleteMark(contextID string, scope review.DiffMode, fileID string) error {
	_, err := store.db.Exec(`DELETE FROM marks WHERE file_id=? AND (review_id, file_id) IN (SELECT m.review_id, m.file_id `+visibleMarks+`)`, fileID, scope, contextID)
	return err
}

func (store *Store) ClearMarks(contextID string, scope review.DiffMode) error {
	_, err := store.db.Exec(`DELETE FROM marks WHERE (review_id, file_id) IN (SELECT m.review_id, m.file_id `+visibleMarks+`)`, scope, contextID)
	return err
}

func (store *Store) StoredVersion(ownerID, diffID, versionID string, now time.Time) (review.RepositoryDiff, error) {
	var raw string
	err := store.db.QueryRow(`SELECT v.manifest FROM diff_versions v JOIN diffs d ON d.id=v.diff_id WHERE d.owner_id=? AND d.id=? AND v.id=? AND (d.expires_at IS NULL OR d.expires_at>?)`, ownerID, diffID, versionID, now.UnixMilli()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return review.RepositoryDiff{}, ErrNotFound
	}
	if err != nil {
		return review.RepositoryDiff{}, err
	}
	var snapshot review.RepositoryDiff
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return review.RepositoryDiff{}, fmt.Errorf("invalid stored diff version: %w", err)
	}
	return snapshot, nil
}

func VersionID(diffID, revision string) string {
	return reviewdata.VersionID(diffID, revision)
}
