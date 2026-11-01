package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/jackc/pgx/v5"
)

var _ contextservice.Store = (*Store)(nil)

// ErrNUL rejects text Postgres cannot store. JSON-decoded requests are valid
// UTF-8, so NUL is the only unrepresentable text; previews are bytea and may hold it.
var ErrNUL = review.Error(400, "text values must not contain NUL")

// millis truncates to the protocol's millisecond precision, so stored times
// and keyset cursors compare exactly.
func millis(at time.Time) time.Time { return time.UnixMilli(at.UnixMilli()) }

func noRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// IngestContext atomically commits an immutable observation and its retry
// identity, and announces the binding on contextChannel. Checkout paths are labels; this method never accesses a producer filesystem.
func (store *Store) IngestContext(ctx context.Context, ownerID string, request ingestion.Request) (reviewdata.Binding, error) {
	if request.SubmissionID == "" || request.Metadata.SourceID == "" {
		return reviewdata.Binding{}, errors.New("submission and source identity required")
	}
	if len(request.Scopes) == 0 {
		return reviewdata.Binding{}, errors.New("observation scopes required")
	}
	if err := storable(request); err != nil {
		return reviewdata.Binding{}, err
	}
	_, hash, err := ingestion.Payload(request)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	var binding reviewdata.Binding
	err = store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		binding, err = store.ingest(ctx, queries, ownerID, request, hash)
		if err != nil {
			return err
		}
		return notifyContext(ctx, queries, ownerID, binding.ContextID)
	})
	if err != nil {
		return reviewdata.Binding{}, err
	}
	return binding, nil
}

// storable rejects NUL in every value stored as text or jsonb.
func storable(request ingestion.Request) error {
	values := []any{request.SubmissionID, request.Metadata}
	for _, scope := range request.Scopes {
		values = append(values, scope.Snapshot)
	}
	for _, commit := range request.Commits {
		values = append(values, commit.Commit, commit.Snapshot)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if escapedNUL(encoded) {
		return ErrNUL
	}
	return nil
}

// escapedNUL reports a \u0000 escape in encoded JSON, skipping escaped backslashes.
func escapedNUL(encoded []byte) bool {
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '\\' {
			continue
		}
		if i+6 <= len(encoded) && string(encoded[i:i+6]) == `\u0000` {
			return true
		}
		i++
	}
	return false
}

func (store *Store) ingest(ctx context.Context, queries *pgdb.Queries, ownerID string, request ingestion.Request, hash string) (reviewdata.Binding, error) {
	sequence, id, err := recordSubmission(ctx, queries, ownerID, request, hash)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	now := millis(time.Now())
	if id != "" {
		return observationBinding(ctx, queries, ownerID, id, now)
	}
	retention, err := store.retentionOf(ctx, queries, ownerID)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	expires := now.Add(retention)
	identityRequest := request
	identityRequest.Metadata.BranchID = ingestion.BranchIdentity(request.Metadata)
	identity, err := ingestion.ContentIdentity(identityRequest)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	if identity != "" {
		// Concurrent fresh submissions of one identity decide reuse one at a time.
		if err := queries.LockIdentity(ctx, pgdb.LockIdentityParams{OwnerID: ownerID, IdentityHash: identity}); err != nil {
			return reviewdata.Binding{}, err
		}
		id, err = queries.ReusableObservation(ctx, pgdb.ReusableObservationParams{OwnerID: ownerID, IdentityHash: identity, Now: now})
		if err == nil {
			if err := queries.RefreshContext(ctx, pgdb.RefreshContextParams{Now: now, ID: id}); err != nil {
				return reviewdata.Binding{}, err
			}
			if err := queries.ExtendObservation(ctx, pgdb.ExtendObservationParams{ExpiresAt: expires, ContextID: id}); err != nil {
				return reviewdata.Binding{}, err
			}
			if err := putObservationSubmission(ctx, queries, ownerID, id, hash, request, sequence); err != nil {
				return reviewdata.Binding{}, err
			}
			return observationBinding(ctx, queries, ownerID, id, now)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return reviewdata.Binding{}, err
		}
		// Expired identity may still await pruning. It must not hide a new review.
		if err := queries.ForgetIdentity(ctx, pgdb.ForgetIdentityParams{OwnerID: ownerID, IdentityHash: identity}); err != nil {
			return reviewdata.Binding{}, err
		}
	}
	seen := make(map[review.DiffMode]bool)
	for _, scope := range request.Scopes {
		if _, err := review.ParseDiffMode(string(scope.Snapshot.Mode)); err != nil {
			return reviewdata.Binding{}, err
		}
		if seen[scope.Snapshot.Mode] {
			return reviewdata.Binding{}, errors.New("duplicate observation scope")
		}
		seen[scope.Snapshot.Mode] = true
	}
	if !seen[review.DiffAll] {
		return reviewdata.Binding{}, errors.New("all observation scope required")
	}
	id, err = newID()
	if err != nil {
		return reviewdata.Binding{}, err
	}
	stream := ingestion.StreamKey(id, request.Metadata)
	// Adding to a stream excludes ending it, so its review state survives.
	if err := queries.LockStream(ctx, pgdb.LockStreamParams{OwnerID: ownerID, StreamKey: stream}); err != nil {
		return reviewdata.Binding{}, err
	}
	var repositoryID *string
	if request.Metadata.RepositoryKey != "" {
		repoID, err := newID()
		if err != nil {
			return reviewdata.Binding{}, err
		}
		if err := queries.InsertRepository(ctx, pgdb.InsertRepositoryParams{ID: repoID, OwnerID: ownerID, RepositoryKey: request.Metadata.RepositoryKey}); err != nil {
			return reviewdata.Binding{}, err
		}
		repoID, err = queries.RepositoryID(ctx, pgdb.RepositoryIDParams{OwnerID: ownerID, RepositoryKey: request.Metadata.RepositoryKey})
		if err != nil {
			return reviewdata.Binding{}, err
		}
		repositoryID = &repoID
	}
	binding := reviewdata.Binding{ContextID: id, RepositoryID: repositoryID, DiffIDs: make(map[review.DiffMode]string)}
	versions := make(map[review.DiffMode]string)
	// The all-scope diff anchors the observation; extra scopes have
	// independent review identities associated through observation_scopes.
	for _, scope := range request.Scopes {
		snapshot := scope.Snapshot
		diffID := id
		if snapshot.Mode != review.DiffAll {
			if diffID, err = newID(); err != nil {
				return reviewdata.Binding{}, err
			}
		}
		versionID, err := newID()
		if err != nil {
			return reviewdata.Binding{}, err
		}
		if err := queries.InsertDiff(ctx, pgdb.InsertDiffParams{ID: diffID, OwnerID: ownerID, Mode: string(snapshot.Mode), CreatedAt: now, ExpiresAt: expires}); err != nil {
			return reviewdata.Binding{}, err
		}
		snapshot.ID, snapshot.VersionID = diffID, versionID
		snapshot.LocationID, snapshot.RepositoryID = nil, repositoryID
		if err := pinObservationVersion(ctx, queries, snapshot, scope.Patches, now); err != nil {
			return reviewdata.Binding{}, err
		}
		binding.DiffIDs[snapshot.Mode] = diffID
		versions[snapshot.Mode] = versionID
	}
	for mode := range binding.DiffIDs {
		reviewID, err := newID()
		if err != nil {
			return reviewdata.Binding{}, err
		}
		if err := queries.EnsureReview(ctx, pgdb.EnsureReviewParams{ID: reviewID, OwnerID: ownerID, StreamKey: stream, Mode: string(mode)}); err != nil {
			return reviewdata.Binding{}, err
		}
	}
	var submittedFrom *string
	if request.Metadata.Root != "" {
		submittedFrom = &request.Metadata.Root
	}
	if err := queries.InsertContext(ctx, pgdb.InsertContextParams{ID: id, OwnerID: ownerID, Now: now, SubmittedFrom: submittedFrom}); err != nil {
		return reviewdata.Binding{}, err
	}
	metadata, err := json.Marshal(request.Metadata)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	if err := queries.InsertObservation(ctx, pgdb.InsertObservationParams{
		ContextID: id, OwnerID: ownerID, SourceID: request.Metadata.SourceID, SubmissionID: request.SubmissionID, PayloadHash: hash,
		RepositoryID: repositoryID, Metadata: metadata, StreamKey: stream, ComparisonPolicy: ingestion.ComparisonPolicy(request.Metadata),
	}); err != nil {
		return reviewdata.Binding{}, err
	}
	for mode, diffID := range binding.DiffIDs {
		if err := queries.InsertScope(ctx, pgdb.InsertScopeParams{ContextID: id, Mode: string(mode), DiffID: diffID, VersionID: versions[mode]}); err != nil {
			return reviewdata.Binding{}, err
		}
	}
	if err := putObservationCommits(ctx, queries, ownerID, id, repositoryID, request, now, expires); err != nil {
		return reviewdata.Binding{}, err
	}
	if err := putObservationSubmission(ctx, queries, ownerID, id, hash, request, sequence); err != nil {
		return reviewdata.Binding{}, err
	}
	if identity != "" {
		if err := queries.InsertIdentity(ctx, pgdb.InsertIdentityParams{OwnerID: ownerID, IdentityHash: identity, ContextID: id}); err != nil {
			return reviewdata.Binding{}, err
		}
	}
	return binding, nil
}

// retentionOf is the store's retention, except for public pull request
// snapshots, which keep the admins' public retention read in the ingesting
// transaction.
func (store *Store) retentionOf(ctx context.Context, queries *pgdb.Queries, ownerID string) (time.Duration, error) {
	if ownerID != githubsync.PublicOwnerID {
		return store.retention, nil
	}
	days, err := queries.PublicRetentionDays(ctx)
	if err != nil {
		return 0, err
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

func putObservationSubmission(ctx context.Context, queries *pgdb.Queries, ownerID, contextID, hash string, request ingestion.Request, sequence int64) error {
	metadata, err := json.Marshal(request.Metadata)
	if err != nil {
		return err
	}
	if err := queries.InsertObservationSubmission(ctx, pgdb.InsertObservationSubmissionParams{OwnerID: ownerID, SourceID: request.Metadata.SourceID, SubmissionID: request.SubmissionID, ContextID: contextID, PayloadHash: hash, Metadata: metadata}); err != nil {
		return err
	}
	if err := queries.BindSubmission(ctx, pgdb.BindSubmissionParams{Sequence: sequence, ContextID: contextID}); err != nil {
		return err
	}
	if err := putSessionAssociation(ctx, queries, ownerID, contextID, request.Metadata); err != nil {
		return err
	}
	if request.Metadata.RepositoryKey == "" || request.Metadata.CheckoutKey == "" {
		return nil
	}
	return queries.AdvanceStreamHead(ctx, pgdb.AdvanceStreamHeadParams{
		OwnerID: ownerID, SourceID: request.Metadata.SourceID, RepositoryKey: request.Metadata.RepositoryKey, CheckoutKey: request.Metadata.CheckoutKey,
		Branch: ingestion.BranchIdentity(request.Metadata), ComparisonPolicy: ingestion.ComparisonPolicy(request.Metadata),
		ContextID: contextID, CollectedAt: request.Metadata.CollectedAt, ArrivalSequence: sequence,
	})
}

func pinObservationVersion(ctx context.Context, queries *pgdb.Queries, snapshot review.RepositoryDiff, patches map[string]review.FilePatch, now time.Time) error {
	manifest, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := queries.InsertDiffVersion(ctx, pgdb.InsertDiffVersionParams{ID: snapshot.VersionID, DiffID: snapshot.ID, Revision: snapshot.Revision, Manifest: manifest, Source: snapshot.Source, ComparisonKind: string(snapshot.ComparisonKind), CreatedAt: now}); err != nil {
		return err
	}
	files := pgdb.InsertDiffFilesParams{VersionID: snapshot.VersionID}
	for _, file := range snapshot.Files {
		patch, exists := patches[file.ID]
		if !exists {
			return errors.New("observation file patch missing")
		}
		encoded, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		files.FileIds = append(files.FileIds, file.ID)
		files.Paths = append(files.Paths, file.Path)
		files.Fingerprints = append(files.Fingerprints, file.Fingerprint)
		files.Patches = append(files.Patches, encoded)
		files.PatchDigests = append(files.PatchDigests, review.PatchDigest(patch))
	}
	if len(files.FileIds) == 0 {
		return nil
	}
	return queries.InsertDiffFiles(ctx, files)
}

func observationBinding(ctx context.Context, queries *pgdb.Queries, ownerID, contextID string, now time.Time) (reviewdata.Binding, error) {
	repositoryID, err := queries.ObservationRepository(ctx, pgdb.ObservationRepositoryParams{OwnerID: ownerID, ContextID: contextID, Now: now})
	if err != nil {
		return reviewdata.Binding{}, noRows(err)
	}
	scopes, err := queries.ObservationScopes(ctx, contextID)
	if err != nil {
		return reviewdata.Binding{}, err
	}
	binding := reviewdata.Binding{ContextID: contextID, RepositoryID: repositoryID, DiffIDs: make(map[review.DiffMode]string, len(scopes))}
	for _, scope := range scopes {
		binding.DiffIDs[review.DiffMode(scope.Mode)] = scope.DiffID
	}
	return binding, nil
}

func (store *Store) ObservationSnapshot(ctx context.Context, ownerID, contextID string, mode review.DiffMode) (review.RepositoryDiff, error) {
	row, err := store.queries.ObservationSnapshot(ctx, pgdb.ObservationSnapshotParams{OwnerID: ownerID, ContextID: contextID, Mode: string(mode), Now: time.Now()})
	if err != nil {
		return review.RepositoryDiff{}, noRows(err)
	}
	return decodeSnapshot(row.Manifest, row.Metadata, row.Modes)
}

// Normalize only responses. Stored manifests, submission bytes and retry hashes
// retain their original protocol 4 representation.
func decodeSnapshot(manifest, metadata []byte, scopes []string) (review.RepositoryDiff, error) {
	var snapshot review.RepositoryDiff
	if err := json.Unmarshal(manifest, &snapshot); err != nil {
		return review.RepositoryDiff{}, err
	}
	if snapshot.ComparisonKind == "" {
		var captured ingestion.Metadata
		if err := json.Unmarshal(metadata, &captured); err != nil {
			return review.RepositoryDiff{}, err
		}
		modes := make([]review.DiffMode, 0, len(scopes))
		for _, scope := range scopes {
			modes = append(modes, review.DiffMode(scope))
		}
		snapshot.ComparisonKind = ingestion.LegacyComparisonKind(snapshot.Mode, snapshot.Source, captured, modes)
	}
	return snapshot, nil
}

// ObservationFileVersion reads only indexed identities. Missing files retain the
// scope identity so callers can reject stale scope references before file errors.
func (store *Store) ObservationFileVersion(ctx context.Context, ownerID, contextID string, mode review.DiffMode, fileID string) (reviewdata.FileVersion, error) {
	row, err := store.queries.ObservationFileVersion(ctx, pgdb.ObservationFileVersionParams{FileID: fileID, OwnerID: ownerID, ContextID: contextID, Mode: string(mode), Now: time.Now()})
	if err != nil {
		return reviewdata.FileVersion{}, noRows(err)
	}
	return reviewdata.FileVersion{DiffID: row.DiffID, VersionID: row.VersionID, Fingerprint: row.Fingerprint}, nil
}

func (store *Store) ObservationPatch(ctx context.Context, ownerID, contextID string, mode review.DiffMode, fileID, fileVersion string) (review.FilePatch, error) {
	raw, err := store.queries.ObservationPatch(ctx, pgdb.ObservationPatchParams{OwnerID: ownerID, ContextID: contextID, Mode: string(mode), FileID: fileID, Fingerprint: fileVersion, Now: time.Now()})
	if err != nil {
		return review.FilePatch{}, noRows(err)
	}
	var patch review.FilePatch
	err = json.Unmarshal(raw, &patch)
	return patch, err
}
