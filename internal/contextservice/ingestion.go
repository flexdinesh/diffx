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
type events[T any] struct {
	mu        sync.Mutex
	listeners map[chan T]struct{}
	closed    bool
}

func (broker *events[T]) close() {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	broker.closed = true
	for listener := range broker.listeners {
		close(listener)
	}
	broker.listeners = nil
}

func (broker *events[T]) publish(value T) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	for listener := range broker.listeners {
		select {
		case listener <- value:
		default:
		}
	}
}

func (broker *events[T]) subscribe(size int) (<-chan T, func()) {
	channel := make(chan T, size)
	broker.mu.Lock()
	if broker.closed {
		broker.mu.Unlock()
		close(channel)
		return channel, func() {}
	}
	if broker.listeners == nil {
		broker.listeners = make(map[chan T]struct{})
	}
	broker.listeners[channel] = struct{}{}
	broker.mu.Unlock()
	return channel, func() {
		broker.mu.Lock()
		delete(broker.listeners, channel)
		broker.mu.Unlock()
	}
}

// Subscribe delivers changed context IDs until unsubscribed; the channel closes
// when the service closes.
func (service *Service) Subscribe() (<-chan string, func()) {
	return service.events.subscribe(1)
}

// CommentChange names the review stream whose comments, replies or
// resolutions changed, and the context the change went through. StreamID is
// empty for piped captures, whose context is their stream.
type CommentChange struct {
	ContextID string `json:"contextId"`
	StreamID  string `json:"streamId"`
}

// SubscribeComments delivers comment changes until unsubscribed; the channel
// closes when the service closes. Each change names its stream, so a few are
// buffered rather than coalesced.
func (service *Service) SubscribeComments() (<-chan CommentChange, func()) {
	return service.comments.subscribe(16)
}
