package reviewstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

// putObservationCommits stores a new observation's captured commits in its
// transaction. Commit diffs expire with the observation's scopes.
func putObservationCommits(tx *sql.Tx, ownerID, contextID string, repositoryID *string, request ingestion.Request, now, expires int64) error {
	if len(request.Commits) == 0 {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO observation_commit_ranges(context_id,total) VALUES(?,?)`, contextID, request.CommitsTotal); err != nil {
		return err
	}
	for ordinal, commit := range request.Commits {
		data, err := json.Marshal(commit.Commit)
		if err != nil {
			return err
		}
		var diffID, versionID *string
		files, additions, deletions := 0, 0, 0
		if commit.Snapshot != nil {
			snapshot := *commit.Snapshot
			if snapshot.Mode != review.DiffCommit {
				return errors.New("commit diff must use the commit mode")
			}
			id, err := newID()
			if err != nil {
				return err
			}
			version, err := newID()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO diffs(id,owner_id,kind,mode,created_at,expires_at) VALUES(?,?,'observation',?,?,?)`, id, ownerID, review.DiffCommit, now, expires); err != nil {
				return err
			}
			snapshot.ID, snapshot.VersionID = id, version
			snapshot.LocationID, snapshot.RepositoryID = nil, repositoryID
			if err := pinObservationVersion(tx, snapshot, commit.Patches, now); err != nil {
				return err
			}
			diffID, versionID = &id, &version
			files = len(snapshot.Files)
			for _, file := range snapshot.Files {
				additions += file.Additions
				deletions += file.Deletions
			}
		}
		if _, err := tx.Exec(`INSERT INTO observation_commits(context_id,ordinal,oid,data,diff_id,version_id,file_count,additions,deletions) VALUES(?,?,?,?,?,?,?,?,?)`,
			contextID, ordinal, commit.OID, string(data), diffID, versionID, files, additions, deletions); err != nil {
			return err
		}
	}
	return nil
}

// ObservationCommits lists an unexpired observation's captured commits,
// oldest first, with the count of omitted older commits.
func (store *Store) ObservationCommits(ctx context.Context, ownerID, contextID string) ([]review.CommitSummary, review.CommitRange, error) {
	var total sql.NullInt64
	err := store.db.QueryRowContext(ctx, `SELECT r.total FROM observations o JOIN contexts c ON c.id=o.context_id JOIN diffs d ON d.id=c.anchor_diff_id
		LEFT JOIN observation_commit_ranges r ON r.context_id=o.context_id WHERE o.owner_id=? AND o.context_id=? AND d.expires_at>?`, ownerID, contextID, time.Now().UnixMilli()).Scan(&total)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, review.CommitRange{}, ErrNotFound
	}
	if err != nil {
		return nil, review.CommitRange{}, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT data,file_count,additions,deletions FROM observation_commits WHERE context_id=? ORDER BY ordinal`, contextID)
	if err != nil {
		return nil, review.CommitRange{}, err
	}
	defer rows.Close()
	commits := make([]review.CommitSummary, 0)
	for rows.Next() {
		var data string
		var summary review.CommitSummary
		if err := rows.Scan(&data, &summary.FileCount, &summary.Additions, &summary.Deletions); err != nil {
			return nil, review.CommitRange{}, err
		}
		if err := json.Unmarshal([]byte(data), &summary.Commit); err != nil {
			return nil, review.CommitRange{}, err
		}
		summary.Merge = summary.Commit.Merge()
		commits = append(commits, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, review.CommitRange{}, err
	}
	return commits, review.CommitRange{Count: len(commits), Omitted: max(int(total.Int64)-len(commits), 0)}, nil
}

// commitDiffJoin selects one captured commit's unexpired diff of an owner's observation.
const commitDiffJoin = `FROM observations o JOIN observation_commits k ON k.context_id=o.context_id
	JOIN diffs d ON d.id=k.diff_id JOIN diff_versions v ON v.id=k.version_id
	WHERE o.owner_id=? AND o.context_id=? AND k.oid=? AND d.expires_at>?`

// ObservationCommitSnapshot reads a captured commit's diff. Merge commits and
// commits outside the observation are not found.
func (store *Store) ObservationCommitSnapshot(ctx context.Context, ownerID, contextID, oid string) (review.RepositoryDiff, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `SELECT v.manifest `+commitDiffJoin, ownerID, contextID, oid, time.Now().UnixMilli()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return review.RepositoryDiff{}, ErrNotFound
	}
	if err != nil {
		return review.RepositoryDiff{}, err
	}
	var snapshot review.RepositoryDiff
	err = json.Unmarshal([]byte(raw), &snapshot)
	return snapshot, err
}

// ObservationCommitFileVersion reads only indexed identities of a commit's file.
func (store *Store) ObservationCommitFileVersion(ctx context.Context, ownerID, contextID, oid, fileID string) (reviewdata.FileVersion, error) {
	var version reviewdata.FileVersion
	err := store.db.QueryRowContext(ctx, `SELECT k.diff_id,k.version_id,f.fingerprint FROM observations o JOIN observation_commits k ON k.context_id=o.context_id
		JOIN diffs d ON d.id=k.diff_id LEFT JOIN diff_files f ON f.version_id=k.version_id AND f.file_id=?
		WHERE o.owner_id=? AND o.context_id=? AND k.oid=? AND d.expires_at>?`, fileID, ownerID, contextID, oid, time.Now().UnixMilli()).Scan(&version.DiffID, &version.VersionID, &version.Fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return reviewdata.FileVersion{}, ErrNotFound
	}
	return version, err
}

func (store *Store) ObservationCommitPatch(ctx context.Context, ownerID, contextID, oid, fileID, fileVersion string) (review.FilePatch, error) {
	var raw string
	err := store.db.QueryRowContext(ctx, `SELECT f.patch FROM observations o JOIN observation_commits k ON k.context_id=o.context_id
		JOIN diffs d ON d.id=k.diff_id JOIN diff_files f ON f.version_id=k.version_id
		WHERE o.owner_id=? AND o.context_id=? AND k.oid=? AND f.file_id=? AND f.fingerprint=? AND d.expires_at>?`, ownerID, contextID, oid, fileID, fileVersion, time.Now().UnixMilli()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return review.FilePatch{}, ErrNotFound
	}
	if err != nil {
		return review.FilePatch{}, err
	}
	var patch review.FilePatch
	err = json.Unmarshal([]byte(raw), &patch)
	return patch, err
}
