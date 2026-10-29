package pgstore

import (
	"context"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
)

// NOTIFY channels. Every channel lives here so replicas and features never
// collide; channels are database-wide, not schema-scoped. Payloads are hints
// carrying opaque IDs that never contain a space. Each notification is sent in
// the transaction whose commit it announces, so a rollback sends none.
const (
	// contextChannel announces a committed observation binding or context
	// deletion. Payload: "<owner ID> <context ID>".
	contextChannel = "diffx_context"
	// accountChannel announces a disabled or deleted account. Payload: account ID.
	accountChannel = "diffx_account"
	// jobChannel announces an ingestion job ready to claim. Payload: empty.
	jobChannel = "diffx_job"
)

var channels = []string{contextChannel, accountChannel, jobChannel}

func notifyContext(ctx context.Context, queries *pgdb.Queries, ownerID, contextID string) error {
	return queries.Notify(ctx, pgdb.NotifyParams{Channel: contextChannel, Payload: ownerID + " " + contextID})
}

func notifyAccount(ctx context.Context, queries *pgdb.Queries, accountID string) error {
	return queries.Notify(ctx, pgdb.NotifyParams{Channel: accountChannel, Payload: accountID})
}

func notifyJob(ctx context.Context, queries *pgdb.Queries) error {
	return queries.Notify(ctx, pgdb.NotifyParams{Channel: jobChannel})
}
