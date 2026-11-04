package localserver

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/packages/localprotocol"
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
	Session    *localprotocol.Session
}

// RunForeground serializes discovery, replacement and publication. Shutdown
// uses the authenticated control endpoint, never an unverified operating-system PID.
func RunForeground(ctx context.Context, settings Settings, input Input, confirm func(Status) (bool, error), reset StateReset, ready func(Status, Published)) error {
	return runForeground(ctx, settings, input, func(status Status) (bool, error) {
		accepted, err := confirm(status)
		if err == nil && !accepted {
			err = errors.New("local instance kept running")
		}
		return false, err
	}, false, reset, func(status Status, published Published, _ bool) error {
		if ready != nil {
			ready(status, published)
		}
		return nil
	}, os.Stderr)
}

// runForeground keeps discovery, attachment, replacement and ownership under
// the same lifecycle lock. Attached callers never acquire database ownership.
func runForeground(ctx context.Context, settings Settings, input Input, decide func(Status) (bool, error), requireRunning bool, reset StateReset, ready func(Status, Published, bool) error, stderr io.Writer) error {
	client, err := NewClient()
	if err != nil {
		return err
	}
	lifecycle, err := client.lifecycleLock(ctx)
	if err != nil {
		return err
	}
	defer lifecycle.Close()
	status, err := client.probe(ctx)
	if err != nil {
		return err
	}
	if status.State != "stopped" {
		attach, err := decide(status)
		if err != nil {
			return err
		}
		if attach {
			if err := ready(status, Published{}, false); err != nil {
				return err
			}
			_ = lifecycle.Close()
			return client.waitInstance(ctx, status.InstanceID)
		}
		if err := client.stop(ctx); err != nil {
			return fmt.Errorf("existing instance could not stop safely: %w", err)
		}
	} else if requireRunning {
		return errors.New("no running local instance to attach")
	}
	owner, err := processlock.TryAcquire(OwnershipPath(client.RuntimeDirectory))
	if err != nil {
		return err
	}
	defer owner.Close()
	id := rand.Text()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var readinessError error
	err = runServer(ctx, cancel, settings, client.RuntimeDirectory, id, input, reset, func(status Status, published Published) {
		_ = lifecycle.Close()
		readinessError = ready(status, published, true)
		if readinessError != nil {
			cancel()
		}
	}, log.New(stderr, "diffx: ", log.LstdFlags))
	return errors.Join(readinessError, err)
}
