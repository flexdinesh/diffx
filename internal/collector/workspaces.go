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
	Requests    []ingestion.Request
	Failures    []Failure
	Diagnostics []string
}

// CollectWorkspaces discovers the workspaces again and collects each checkout
// independently with its own submission ID. A failed checkout never blocks the
// others or becomes an empty observation. Finding no checkout is an error.
func CollectWorkspaces(ctx context.Context, paths []string, depth int, options Options) (Batch, error) {
	options, err := defaults(options)
	if err != nil {
		return Batch{}, err
	}
	discovery, err := DiscoverWorkspaces(ctx, paths, depth, options.SourceID, options.Base)
	if err != nil {
		return Batch{}, err
	}
	batch := Batch{Diagnostics: discovery.Diagnostics}
	if len(discovery.Sources) == 0 {
		detail := "no Git checkouts found in " + strings.Join(paths, ", ")
		if len(discovery.Diagnostics) > 0 {
			detail += ": " + strings.Join(discovery.Diagnostics, "; ")
		}
		return batch, errors.New(detail)
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
		request ingestion.Request
		err     error
	}
	outcomes := make([]outcome, len(discovery.Sources))
	slots := make(chan struct{}, collectionWorkers)
	var workers sync.WaitGroup
	for index, source := range discovery.Sources {
		workers.Add(1)
		go func() {
			defer workers.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			settings := options
			settings.SubmissionID, settings.TriggerRoot = uuid.NewString(), source.InputPath
			request, err := Collect(ctx, source.Path, settings)
			outcomes[index] = outcome{request, err}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	for index, result := range outcomes {
		if result.err != nil {
			batch.Failures = append(batch.Failures, Failure{Path: discovery.Sources[index].Path, Detail: result.err.Error()})
			continue
		}
		batch.Requests = append(batch.Requests, result.request)
	}
	return batch, nil
}
