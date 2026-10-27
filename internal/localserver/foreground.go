package localserver

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

// Collection, CollectionFailure and Published describe local collections of
// the selected checkouts for command entry points.
type (
	Collection        = serverapp.Collection
	CollectionFailure = serverapp.CollectionFailure
	Collect           = serverapp.Collect
	Published         = serverapp.Published
	Selection         = serverapp.Selection
)

const (
	SelectedCheckout    = serverapp.SelectedCheckout
	DiscoveredCheckouts = serverapp.DiscoveredCheckouts
)

// Input supplies a fixed capture or checkout collection to run in the
// background at startup and again on explicit user request. Selection names
// the scope Collect gathers.
type Input struct {
	Collection Collection
	Collect    Collect
	Selection  Selection
}

// RunForeground serializes discovery, replacement and publication. Shutdown
// uses the authenticated control endpoint, never an unverified operating-system PID.
func RunForeground(ctx context.Context, settings Settings, input Input, confirm func(Status) (bool, error), reset StateReset, ready func(Status, Published)) error {
	client, err := NewClient()
	if err != nil {
		return err
	}
	lifecycle, err := client.lifecycleLock(ctx)
	if err != nil {
		return err
	}
	defer lifecycle.Close()
	_, status, err := client.probe(ctx)
	if err != nil {
		return err
	}
	if status.State != "stopped" {
		accepted, err := confirm(status)
		if err != nil {
			return err
		}
		if !accepted {
			return errors.New("local instance kept running")
		}
		if err := client.stop(ctx); err != nil {
			return fmt.Errorf("existing instance could not stop safely: %w", err)
		}
	}
	owner, err := processlock.TryAcquire(OwnershipPath(client.RuntimeDirectory))
	if err != nil {
		return err
	}
	defer owner.Close()
	id := rand.Text()
	defer RemoveDescriptor(client.RuntimeDirectory, id)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return runServer(ctx, cancel, settings, client.RuntimeDirectory, id, input, reset, func(status Status, published Published) {
		_ = lifecycle.Close()
		if ready != nil {
			ready(status, published)
		}
	}, log.New(os.Stderr, "diffx: ", log.LstdFlags))
}
