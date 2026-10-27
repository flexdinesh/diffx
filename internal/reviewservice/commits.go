package reviewservice

import (
	"context"
	"errors"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

// CommitStore reads an owner's captured branch commits. Commit diffs are a
// read-only lens: they carry no comments or reviewed marks.
type CommitStore interface {
	ObservationCommits(context.Context, string, string) ([]review.CommitSummary, review.CommitRange, error)
	ObservationCommitSnapshot(context.Context, string, string, string) (review.RepositoryDiff, error)
	ObservationCommitFileVersion(context.Context, string, string, string, string) (reviewdata.FileVersion, error)
	ObservationCommitPatch(context.Context, string, string, string, string, string) (review.FilePatch, error)
}

// CommitList is the context's captured commits, oldest first.
type CommitList struct {
	Commits []review.CommitSummary `json:"commits"`
	Omitted int                    `json:"omitted"`
}

func (service *Service) Commits(ctx context.Context) (CommitList, error) {
	commits, summary, err := service.store.ObservationCommits(ctx, service.session.User.ID, service.session.ContextID)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return CommitList{}, review.Error(404, "Context not found")
	}
	if err != nil {
		return CommitList{}, err
	}
	return CommitList{Commits: commits, Omitted: summary.Omitted}, nil
}

// CommitSnapshot reads a captured commit's diff against its first parent.
func (service *Service) CommitSnapshot(ctx context.Context, oid string) (review.RepositoryDiff, error) {
	snapshot, err := service.store.ObservationCommitSnapshot(ctx, service.session.User.ID, service.session.ContextID, oid)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return review.RepositoryDiff{}, review.Error(404, "Commit diff not captured")
	}
	return snapshot, err
}

// CommitPatch validates the file reference before reading its stored preview.
func (service *Service) CommitPatch(ctx context.Context, oid, fileID, fileVersion string) (review.FilePatch, error) {
	if err := ctx.Err(); err != nil {
		return review.FilePatch{}, err
	}
	version, err := service.store.ObservationCommitFileVersion(ctx, service.session.User.ID, service.session.ContextID, oid, fileID)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return review.FilePatch{}, review.Error(404, "Commit diff not captured")
	}
	if err != nil {
		return review.FilePatch{}, err
	}
	if version.Fingerprint == nil {
		return review.FilePatch{}, review.Error(404, "File is not in the commit")
	}
	if fileVersion != "" && *version.Fingerprint != fileVersion {
		return review.FilePatch{}, review.Error(409, "File changed. Refresh to load the latest version.")
	}
	patch, err := service.store.ObservationCommitPatch(ctx, service.session.User.ID, service.session.ContextID, oid, fileID, *version.Fingerprint)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return review.FilePatch{}, review.Error(404, "File not in commit")
	}
	return patch, err
}

func (service *Service) CommitContents(ctx context.Context, oid, fileID, fileVersion string) (review.FileContents, error) {
	if !service.session.Capabilities.Files.Contents.Enabled() {
		return review.FileContents{}, reviewsession.NotEnabled(reviewsession.FilesContents)
	}
	patch, err := service.CommitPatch(ctx, oid, fileID, fileVersion)
	if err != nil {
		return review.FileContents{}, err
	}
	if patch.Contents == nil {
		return review.FileContents{}, review.Error(404, "Full file contents were not collected")
	}
	return *patch.Contents, nil
}
