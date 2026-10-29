package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewservice"
)

var _ reviewservice.CommitStore = (*Store)(nil)

// putObservationCommits stores a new observation's captured commits in its
// transaction. Commit diffs expire with the observation's scopes.
func putObservationCommits(ctx context.Context, queries *pgdb.Queries, ownerID, contextID string, repositoryID *string, request ingestion.Request, now, expires time.Time) error {
	if len(request.Commits) == 0 {
		return nil
	}
	if err := queries.InsertCommitRange(ctx, pgdb.InsertCommitRangeParams{ContextID: contextID, Total: int64(request.CommitsTotal)}); err != nil {
		return err
	}
	for ordinal, commit := range request.Commits {
		data, err := json.Marshal(commit.Commit)
		if err != nil {
			return err
		}
		row := pgdb.InsertCommitParams{ContextID: contextID, Ordinal: int32(ordinal), Oid: commit.OID, Data: data}
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
			if err := queries.InsertDiff(ctx, pgdb.InsertDiffParams{ID: id, OwnerID: ownerID, Mode: string(review.DiffCommit), CreatedAt: now, ExpiresAt: expires}); err != nil {
				return err
			}
			snapshot.ID, snapshot.VersionID = id, version
			snapshot.LocationID, snapshot.RepositoryID = nil, repositoryID
			if err := pinObservationVersion(ctx, queries, snapshot, commit.Patches, now); err != nil {
				return err
			}
			row.DiffID, row.VersionID = &id, &version
			row.FileCount = int64(len(snapshot.Files))
			for _, file := range snapshot.Files {
				row.Additions += int64(file.Additions)
				row.Deletions += int64(file.Deletions)
			}
		}
		if err := queries.InsertCommit(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// ObservationCommits lists an unexpired observation's captured commits,
// oldest first, with the count of omitted older commits.
func (store *Store) ObservationCommits(ctx context.Context, ownerID, contextID string) ([]review.CommitSummary, review.CommitRange, error) {
	total, err := store.queries.ObservationCommitTotal(ctx, pgdb.ObservationCommitTotalParams{OwnerID: ownerID, ContextID: contextID, Now: time.Now()})
	if err != nil {
		return nil, review.CommitRange{}, noRows(err)
	}
	rows, err := store.queries.ObservationCommits(ctx, contextID)
	if err != nil {
		return nil, review.CommitRange{}, err
	}
	commits := make([]review.CommitSummary, 0, len(rows))
	for _, row := range rows {
		summary := review.CommitSummary{FileCount: int(row.FileCount), Additions: int(row.Additions), Deletions: int(row.Deletions)}
		if err := json.Unmarshal(row.Data, &summary.Commit); err != nil {
			return nil, review.CommitRange{}, err
		}
		summary.Merge = summary.Commit.Merge()
		commits = append(commits, summary)
	}
	omitted := 0
	if total != nil {
		omitted = max(int(*total)-len(commits), 0)
	}
	return commits, review.CommitRange{Count: len(commits), Omitted: omitted}, nil
}

// ObservationCommitSnapshot reads a captured commit's diff. Merge commits and
// commits outside the observation are not found.
func (store *Store) ObservationCommitSnapshot(ctx context.Context, ownerID, contextID, oid string) (review.RepositoryDiff, error) {
	raw, err := store.queries.ObservationCommitManifest(ctx, pgdb.ObservationCommitManifestParams{OwnerID: ownerID, ContextID: contextID, Oid: oid, Now: time.Now()})
	if err != nil {
		return review.RepositoryDiff{}, noRows(err)
	}
	var snapshot review.RepositoryDiff
	err = json.Unmarshal(raw, &snapshot)
	return snapshot, err
}

// ObservationCommitFileVersion reads only indexed identities of a commit's file.
func (store *Store) ObservationCommitFileVersion(ctx context.Context, ownerID, contextID, oid, fileID string) (reviewdata.FileVersion, error) {
	row, err := store.queries.ObservationCommitFileVersion(ctx, pgdb.ObservationCommitFileVersionParams{FileID: fileID, OwnerID: ownerID, ContextID: contextID, Oid: oid, Now: time.Now()})
	if err != nil {
		return reviewdata.FileVersion{}, noRows(err)
	}
	return reviewdata.FileVersion{DiffID: row.DiffID, VersionID: row.VersionID, Fingerprint: row.Fingerprint}, nil
}

func (store *Store) ObservationCommitPatch(ctx context.Context, ownerID, contextID, oid, fileID, fileVersion string) (review.FilePatch, error) {
	raw, err := store.queries.ObservationCommitPatch(ctx, pgdb.ObservationCommitPatchParams{OwnerID: ownerID, ContextID: contextID, Oid: oid, FileID: fileID, Fingerprint: fileVersion, Now: time.Now()})
	if err != nil {
		return review.FilePatch{}, noRows(err)
	}
	var patch review.FilePatch
	err = json.Unmarshal(raw, &patch)
	return patch, err
}
