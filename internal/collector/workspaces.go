package collector

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/google/uuid"
)

const collectionWorkers = 4

// Failure names a discovered checkout that could not be collected.
type Failure struct {
	Path   string
	Detail string
}

// Batch is one collection of every checkout discovered under the workspaces.
type Batch struct {
	Failures    []Failure
	Diagnostics []string
}

// CollectWorkspaces discovers the workspaces again and collects each checkout
// independently with its own submission ID. A failed checkout never blocks the
// others or becomes an empty observation. Finding no checkout is an error.
// publish runs serially as requests finish. Its failure cancels and joins all
// remaining collection before returning. Four workers and a bounded result
// buffer limit completed requests waiting to publish.
func CollectWorkspaces(ctx context.Context, workspaces Workspaces, options Options, publish func(ingestion.Request) error) (Batch, error) {
	options, err := defaults(options)
	if err != nil {
		return Batch{}, err
	}
	// Collection runs concurrently; callbacks observe one source at a time.
	var observed sync.Mutex
	if notify := options.OnSource; notify != nil {
		options.OnSource = func(metadata ingestion.Metadata, comparison diffsource.Comparison) {
			observed.Lock()
			defer observed.Unlock()
			notify(metadata, comparison)
		}
	}
	type outcome struct {
		path    string
		request ingestion.Request
		err     error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan DiscoverySource)
	outcomes := make(chan outcome, collectionWorkers)
	type discovered struct {
		value Discovery
		err   error
	}
	discoveryDone := make(chan discovered, 1)
	go func() {
		value, err := discoverWorkspaces(ctx, workspaces, options.SourceID, options.Base, func(source DiscoverySource) error {
			select {
			case jobs <- source:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(jobs)
		discoveryDone <- discovered{value, err}
	}()
	var workers sync.WaitGroup
	for range collectionWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for source := range jobs {
				if ctx.Err() != nil {
					return
				}
				settings := options
				settings.SubmissionID, settings.TriggerRoot = uuid.NewString(), source.InputPath
				request, err := Collect(ctx, source.Path, settings)
				select {
				case outcomes <- outcome{source.Path, request, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(outcomes)
	}()
	batch := Batch{}
	var publishError error
	for result := range outcomes {
		if ctx.Err() != nil {
			continue
		}
		if result.err != nil {
			batch.Failures = append(batch.Failures, Failure{Path: result.path, Detail: result.err.Error()})
			continue
		}
		if err := publish(result.request); err != nil {
			publishError = err
			cancel()
		}
	}
	discovery := <-discoveryDone
	batch.Diagnostics = discovery.value.Diagnostics
	if publishError != nil {
		return batch, publishError
	}
	if err := ctx.Err(); err != nil {
		return batch, err
	}
	if discovery.err != nil {
		return batch, discovery.err
	}
	if len(discovery.value.Sources) == 0 {
		detail := "no Git checkouts found in " + strings.Join(workspaces.Paths, ", ")
		if len(batch.Diagnostics) > 0 {
			detail += ": " + strings.Join(batch.Diagnostics, "; ")
		}
		return batch, errors.New(detail)
	}
	return batch, nil
}
