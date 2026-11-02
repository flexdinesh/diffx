package contextservice

import (
	"context"
	"slices"
	"sync"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

func (service *Service) Ingest(ctx context.Context, input ingestion.Request) (Publication, error) {
	if err := ctx.Err(); err != nil {
		return Publication{}, err
	}
	if err := ingestion.Validate(input); err != nil {
		return Publication{}, review.Error(400, "%v", err)
	}
	binding, err := service.store.IngestContext(ctx, service.user.ID, input)
	if err != nil {
		return Publication{}, requestError(err)
	}
	service.changed(binding.ContextID)
	item, err := service.Get(ctx, binding.ContextID)
	if err != nil {
		return Publication{}, err
	}
	snapshot, err := service.store.ObservationSnapshot(ctx, service.user.ID, binding.ContextID, review.DiffAll)
	if err != nil {
		return Publication{}, requestError(err)
	}
	return Publication{Context: item, Snapshot: snapshot}, nil
}

type storedSource struct {
	store    Store
	ownerID  string
	id       string
	metadata ingestion.Metadata
	kind     string
	binding  reviewdata.Binding
}

func (source *storedSource) Root() string { return source.metadata.Root }
func (source *storedSource) Kind() string { return source.kind }
func (source *storedSource) Support() review.Support {
	scopes := []review.DiffMode{}
	for _, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged, review.DiffUnstaged, review.DiffUncommitted, review.DiffCommitted} {
		if _, ok := source.binding.DiffIDs[mode]; ok {
			scopes = append(scopes, mode)
		}
	}
	return storedSupport(scopes, source.kind == "local")
}
func (source *storedSource) Snapshot(ctx context.Context, mode review.DiffMode) (review.RepositoryDiff, error) {
	if err := ctx.Err(); err != nil {
		return review.RepositoryDiff{}, err
	}
	value, err := source.store.ObservationSnapshot(ctx, source.ownerID, source.id, mode)
	return value, requestError(err)
}
func (source *storedSource) Patch(ctx context.Context, mode review.DiffMode, file review.ChangedFile, expected *string) (review.FilePatch, error) {
	if err := ctx.Err(); err != nil {
		return review.FilePatch{}, err
	}
	value, err := source.store.ObservationPatch(ctx, source.ownerID, source.id, mode, file.ID, file.Fingerprint)
	return value, requestError(err)
}
func (source *storedSource) Contents(ctx context.Context, mode review.DiffMode, file review.ChangedFile, expected *string) (review.FileContents, error) {
	preview, err := source.Patch(ctx, mode, file, expected)
	if err != nil {
		return review.FileContents{}, err
	}
	if preview.Contents == nil {
		return review.FileContents{}, review.Error(404, "Full file contents were not collected")
	}
	return *preview.Contents, nil
}

func storedCapabilities(scopes []review.DiffMode, contents bool, policies reviewsession.Policies) reviewsession.Capabilities {
	return reviewsession.ResolveCapabilities(storedSupport(scopes, contents), policies)
}

func storedSupport(scopes []review.DiffMode, contents bool) review.Support {
	return review.Support{Scopes: scopes, StagingMetadata: slices.Contains(scopes, review.DiffStaged) || slices.Contains(scopes, review.DiffUnstaged), FileContents: contents}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Notifications are hints. Reconnecting clients always reload the durable catalog.
type events struct {
	mu        sync.Mutex
	listeners map[chan string]struct{}
	closed    bool
}

func (broker *events) close() {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.closed = true
	for listener := range broker.listeners {
		close(listener)
	}
	broker.listeners = nil
}

func (broker *events) publish(id string) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	for listener := range broker.listeners {
		select {
		case listener <- id:
		default:
		}
	}
}

// Subscribe delivers changed context IDs until unsubscribed; the channel closes
// when the service closes.
func (service *Service) Subscribe() (<-chan string, func()) {
	channel := make(chan string, 1)
	service.events.mu.Lock()
	if service.events.closed {
		service.events.mu.Unlock()
		close(channel)
		return channel, func() {}
	}
	if service.events.listeners == nil {
		service.events.listeners = make(map[chan string]struct{})
	}
	service.events.listeners[channel] = struct{}{}
	service.events.mu.Unlock()
	return channel, func() {
		service.events.mu.Lock()
		delete(service.events.listeners, channel)
		service.events.mu.Unlock()
	}
}
