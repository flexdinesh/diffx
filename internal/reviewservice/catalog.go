package reviewservice

import (
	"context"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

// ContextProvider must resolve IDs within the authenticated user's catalog.
// Resolve validates ownership and expiry before any review storage is accessed.
type ContextProvider interface {
	ListFiltered(context.Context, int, string, ingestion.Filter) (contextservice.Page, error)
	Resolve(context.Context, string) (reviewsession.Session, error)
}

type Catalog struct {
	provider ContextProvider
	store    MutationStore
}

func NewCatalog(provider ContextProvider, store MutationStore) *Catalog {
	return &Catalog{provider: provider, store: store}
}
func (catalog *Catalog) ListContexts(ctx context.Context, limit int, cursor string, filter ingestion.Filter) (contextservice.Page, error) {
	return catalog.provider.ListFiltered(ctx, limit, cursor, filter)
}
func (catalog *Catalog) Review(ctx context.Context, id string) (*Service, error) {
	if id == "" {
		return nil, review.Error(400, "context_id required; use list_contexts to select a review")
	}
	active, err := catalog.provider.Resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	service := New(active, catalog.store)
	return service, nil
}
func (catalog *Catalog) GetDiff(ctx context.Context, id string, scope review.DiffMode) (review.RepositoryDiff, error) {
	service, err := catalog.Review(ctx, id)
	if err != nil {
		return review.RepositoryDiff{}, err
	}
	return service.Snapshot(ctx, scope)
}

func (catalog *Catalog) GetCommits(ctx context.Context, id string) (CommitList, error) {
	service, err := catalog.Review(ctx, id)
	if err != nil {
		return CommitList{}, err
	}
	return service.Commits(ctx)
}

func (catalog *Catalog) GetCommitDiff(ctx context.Context, id, oid string) (review.RepositoryDiff, error) {
	service, err := catalog.Review(ctx, id)
	if err != nil {
		return review.RepositoryDiff{}, err
	}
	return service.CommitSnapshot(ctx, oid)
}

func (catalog *Catalog) GetCommitPatch(ctx context.Context, id, oid, fileID string) (review.FilePatch, error) {
	service, err := catalog.Review(ctx, id)
	if err != nil {
		return review.FilePatch{}, err
	}
	return service.CommitPatch(ctx, oid, fileID, "")
}

func (catalog *Catalog) GetPatch(ctx context.Context, id string, scope review.DiffMode, fileID string) (review.FilePatch, error) {
	service, err := catalog.Review(ctx, id)
	if err != nil {
		return review.FilePatch{}, err
	}
	return service.Patch(ctx, scope, fileID)
}
