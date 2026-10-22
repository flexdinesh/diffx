package reviewservice

import (
	"context"
	"errors"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

func (service *Service) fileVersion(ctx context.Context, mode review.DiffMode, fileID string) (reviewdata.FileVersion, error) {
	if err := ctx.Err(); err != nil {
		return reviewdata.FileVersion{}, err
	}
	if !service.session.Capabilities.Diff.Scopes.Allows(mode) {
		return reviewdata.FileVersion{}, reviewsession.NotEnabled(reviewsession.DiffScopes)
	}
	version, err := service.store.ObservationFileVersion(ctx, service.session.User.ID, service.session.ContextID, mode, fileID)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return reviewdata.FileVersion{}, review.Error(404, "Diff not found")
	}
	return version, err
}

func (service *Service) storedPatch(ctx context.Context, mode review.DiffMode, fileID, fingerprint string) (review.FilePatch, error) {
	patch, err := service.store.ObservationPatch(ctx, service.session.User.ID, service.session.ContextID, mode, fileID, fingerprint)
	if errors.Is(err, reviewdata.ErrNotFound) {
		return review.FilePatch{}, review.Error(404, "File not in observation")
	}
	return patch, err
}

// Patch reads a stored file preview without materializing the scope manifest.
func (service *Service) Patch(ctx context.Context, mode review.DiffMode, fileID string) (review.FilePatch, error) {
	version, err := service.fileVersion(ctx, mode, fileID)
	if err != nil {
		return review.FilePatch{}, err
	}
	if version.Fingerprint == nil {
		return review.FilePatch{}, review.Error(404, "File is not in the stored diff")
	}
	return service.storedPatch(ctx, mode, fileID, *version.Fingerprint)
}

// CurrentPatch validates the caller's immutable scope and file references before
// reading the preview. Scope conflicts take precedence over missing file errors.
func (service *Service) CurrentPatch(ctx context.Context, mode review.DiffMode, diffID, versionID, fileID, fileVersion string) (review.FilePatch, error) {
	version, err := service.fileVersion(ctx, mode, fileID)
	if err != nil {
		return review.FilePatch{}, err
	}
	if version.DiffID != diffID || version.VersionID != versionID {
		return review.FilePatch{}, review.Error(409, "Diff changed. Refresh to load the latest version.")
	}
	if version.Fingerprint == nil {
		return review.FilePatch{}, review.Error(404, "File is not in the current diff")
	}
	if *version.Fingerprint != fileVersion {
		return review.FilePatch{}, review.Error(409, "File changed. Refresh to load the latest version.")
	}
	return service.storedPatch(ctx, mode, fileID, *version.Fingerprint)
}

func (service *Service) CurrentContents(ctx context.Context, mode review.DiffMode, diffID, versionID, fileID, fileVersion string) (review.FileContents, error) {
	if !service.session.Capabilities.Files.Contents.Enabled() {
		return review.FileContents{}, reviewsession.NotEnabled(reviewsession.FilesContents)
	}
	patch, err := service.CurrentPatch(ctx, mode, diffID, versionID, fileID, fileVersion)
	if err != nil {
		return review.FileContents{}, err
	}
	if patch.Contents == nil {
		return review.FileContents{}, review.Error(404, "Full file contents were not collected")
	}
	return *patch.Contents, nil
}
