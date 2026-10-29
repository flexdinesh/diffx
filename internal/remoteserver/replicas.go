package remoteserver

import (
	"context"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

// changeFeed delivers changes committed by any replica, including this one.
// Deliveries are hints; after a gap it delivers pgstore.Resync.
type changeFeed interface {
	Run(context.Context, func(pgstore.Notification)) error
}

// replicate applies committed changes to this replica: owners' subscribers
// hear about their contexts, removed accounts lose their services and streams,
// and the worker wakes for queued jobs. A resync makes every active owner's
// clients reload their catalog and lets the worker look for missed jobs.
func replicate(feed changeFeed, applications *registry, wake chan<- struct{}) serverapp.Task {
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return func(ctx context.Context) error {
		return feed.Run(ctx, func(change pgstore.Notification) {
			switch change.Kind {
			case pgstore.ContextChanged:
				applications.notify(change.OwnerID, change.ContextID)
			case pgstore.AccountRemoved:
				_ = applications.remove(change.OwnerID)
			case pgstore.JobQueued:
				signal()
			case pgstore.Resync:
				applications.resync()
				signal()
			}
		})
	}
}

// notify reaches only owners with services here; others have no subscribers.
func (registry *registry) notify(ownerID, contextID string) {
	registry.mu.Lock()
	app, ok := registry.applications[ownerID]
	registry.mu.Unlock()
	if ok {
		app.service.Notify(contextID)
	}
}

func (registry *registry) resync() {
	registry.mu.Lock()
	apps := make([]application, 0, len(registry.applications))
	for _, app := range registry.applications {
		apps = append(apps, app)
	}
	registry.mu.Unlock()
	for _, app := range apps {
		app.service.Notify("")
	}
}
