package pgstore

import (
	"context"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
)

// putSessionAssociation records observation, never ownership or authorship.
func putSessionAssociation(ctx context.Context, queries *pgdb.Queries, ownerID, contextID string, metadata ingestion.Metadata) error {
	session := ingestion.AgentSession{Harness: metadata.Agent, ID: metadata.RunID}
	if metadata.AgentSession != nil {
		session = *metadata.AgentSession
	}
	if session.ID == "" || session.Harness == "" {
		return nil
	}
	namedAt := int64(0)
	if session.Name != "" {
		namedAt = metadata.CollectedAt
	}
	if err := queries.UpsertAgentSession(ctx, pgdb.UpsertAgentSessionParams{OwnerID: ownerID, SourceID: metadata.SourceID, Harness: session.Harness, SessionID: session.ID, Name: session.Name, NamedAt: namedAt}); err != nil {
		return err
	}
	return queries.UpsertObservationSession(ctx, pgdb.UpsertObservationSessionParams{ContextID: contextID, OwnerID: ownerID, SourceID: metadata.SourceID, Harness: session.Harness, SessionID: session.ID, ObservedAt: metadata.CollectedAt})
}
