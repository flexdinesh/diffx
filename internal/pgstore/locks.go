package pgstore

// Advisory lock keys. Every key lives here so replicas and features never
// collide. Single bigint keys and (class, hash) pairs are separate Postgres
// key spaces; test harnesses use 7_469_000_100 and above.
const (
	// schemaLock serializes schema creation, which precedes goose's own lock.
	schemaLock = 7_469_000_001
	// adminSetupLock serializes setup issuance with admin creation.
	adminSetupLock = 7_469_000_010
	// pruneLock admits one replica's retention pass; others skip theirs.
	pruneLock = 7_469_000_020
	// githubReconcileLock admits one replica's startup reconciliation; others skip theirs.
	githubReconcileLock = 7_469_000_030
)

// Two-key classes; the second key hashes the locked value.
const (
	// inviteEmailLocks serializes invitation and acceptance per email.
	inviteEmailLocks = 7_469_011
	// endpointLockClass serializes device registration per push endpoint.
	endpointLockClass = 7_469_002
)

// Classes used literally in queries/content.sql:
//   - 7469101 serializes reuse decisions per owner and content identity.
//   - 7469102 serializes a stream's observation additions with its end.
