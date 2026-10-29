package collector

import (
	"context"

	"github.com/flexdinesh/diffx/internal/review"
)

// Observation describes collection work without exposing captured contents.
type Observation struct {
	Stage   string
	Scope   review.DiffMode
	Attempt int
	Files   int
	Commits int
}

// Observer lives beside its consumer. Composition owns recording and storage.
type Observer interface {
	BeginStage(Observation) func(error)
	Workload(Observation)
}

type observerKey struct{}
type factoryKey struct{}

// ObservationFactory creates an independent recorder for each checkout or
// discovery attempt. Its completion may annotate an error, preserving Unwrap.
type ObservationFactory func(context.Context, string) (context.Context, func(error) error)

func WithObservationFactory(ctx context.Context, factory ObservationFactory) context.Context {
	return context.WithValue(ctx, factoryKey{}, factory)
}

func WithObserver(ctx context.Context, observer Observer) context.Context {
	return context.WithValue(ctx, observerKey{}, observer)
}

func observeCollection(ctx context.Context, path string) (context.Context, func(error) error) {
	if factory, ok := ctx.Value(factoryKey{}).(ObservationFactory); ok {
		return factory(ctx, path)
	}
	return ctx, func(err error) error { return err }
}

func observeStage(ctx context.Context, event Observation) func(error) {
	if observer, ok := ctx.Value(observerKey{}).(Observer); ok {
		return observer.BeginStage(event)
	}
	return func(error) {}
}

func observeWorkload(ctx context.Context, event Observation) {
	if observer, ok := ctx.Value(observerKey{}).(Observer); ok {
		observer.Workload(event)
	}
}

func observeValue[T any](ctx context.Context, event Observation, read func() (T, error)) (T, error) {
	finish := observeStage(ctx, event)
	value, err := read()
	finish(err)
	return value, err
}
