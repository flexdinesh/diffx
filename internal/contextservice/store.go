package contextservice

import (
	"context"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

// Store preserves atomic ingestion; callers cannot partially publish an observation.
type Store interface {
	IngestContext(context.Context, string, ingestion.Request) (reviewdata.Binding, error)
	ObservationSnapshot(context.Context, string, string, review.DiffMode) (review.RepositoryDiff, error)
	ObservationPatch(context.Context, string, string, review.DiffMode, string, string) (review.FilePatch, error)
	ObservationContexts(context.Context, string, int, int64, string, ingestion.Filter) ([]reviewdata.ContextInfo, error)
	Context(context.Context, string, string, time.Time) (reviewdata.ContextInfo, error)
	ContextBinding(context.Context, string, string, time.Time) (reviewdata.Binding, error)
	Contexts(context.Context, string, int, int64, string, time.Time) ([]reviewdata.ContextInfo, error)
	ContextCount(context.Context, string, ingestion.Selection, time.Time) (int, error)
	DeleteContext(context.Context, string, string) error
}
