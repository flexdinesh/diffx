package pgstore

// Advisory lock keys. Every key lives here so replicas and features never
// collide. Single bigint keys and (class, hash) pairs are separate Postgres
// key spaces; test harnesses use 7_469_000_100 and above.
const (
	// schemaLock serializes schema creation, which precedes goose's own lock.
	schemaLock = 7_469_000_001
	// adminSetupLock serializes setup issuance with admin creation.
	adminSetupLock = 7_469_000_010
)

// Two-key classes; the second key hashes the locked value.
const (
	// inviteEmailLocks serializes invitation and acceptance per email.
	inviteEmailLocks = 7_469_011
	// endpointLockClass serializes device registration per push endpoint.
	endpointLockClass = 7_469_002
)
