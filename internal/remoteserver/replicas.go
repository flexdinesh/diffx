package remoteserver

import (
	"context"
	"log"

	"github.com/flexdinesh/diffx/internal/identityservice"
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
// the worker wakes for queued jobs and settings reload. A resync makes every
// active owner's clients reload their catalog, lets the worker look for missed
// jobs and reloads settings.
func replicate(feed changeFeed, applications *registry, wake, reload chan<- struct{}) serverapp.Task {
	signal := func(channel chan<- struct{}) {
		select {
		case channel <- struct{}{}:
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
				signal(wake)
			case pgstore.SettingsChanged:
				signal(reload)
			case pgstore.Resync:
				applications.resync()
				signal(wake)
				signal(reload)
			}
		})
	}
}

// reloadSettings refreshes the settings snapshot on each signal. A failed
// reload keeps the last snapshot; the next change or resync retries.
func reloadSettings(identity *identityservice.Service, reload <-chan struct{}) serverapp.Task {
	return func(ctx context.Context) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-reload:
				if err := loadSettings(ctx, identity); err != nil && ctx.Err() == nil {
					log.Printf("reload settings: %v", err)
				}
			}
		}
	}
}

// loadSettings replaces the snapshot and warns when no one new can join.
func loadSettings(ctx context.Context, identity *identityservice.Service) error {
	settings, err := identity.LoadSettings(ctx)
	if err != nil {
		return err
	}
	if !identity.GoogleEnabled() && !settings.PasswordSignup {
		log.Printf("diffx-server: no one new can join: Google sign-in is not configured and password signup is off in the console's Settings")
	}
	return nil
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
