package daemon

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/processlock"
)

// Input is the initially collected request and, for a checkout, how to
// collect it again on explicit user request.
type Input struct {
	Request ingestion.Request
	Collect func(context.Context) (ingestion.Request, error)
}

// RunForeground serializes discovery, replacement and publication. Shutdown
// uses the authenticated control endpoint, never an unverified operating-system PID.
func RunForeground(ctx context.Context, settings Settings, input Input, confirm func(Status) (bool, error), ready func(Status, *contextservice.Submission)) error {
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
	return runServer(ctx, cancel, settings, client.RuntimeDirectory, id, &input.Request, input.Collect, func(status Status, submitted *contextservice.Submission) {
		_ = lifecycle.Close()
		if ready != nil {
			ready(status, submitted)
		}
	}, log.New(os.Stderr, "diffx: ", log.LstdFlags))
}
