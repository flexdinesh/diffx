package reviewstore

import "database/sql"

// dialect owns what differs between SQL backends: schema statements, version
// records and a few expressions. Operations, transactions and their semantic
// rules are shared by every backend. Paths name JSON object keys, such as
// "comparison.kind".
type dialect interface {
	// createSchema runs the idempotent creation statements of the current schema.
	createSchema(*sql.Tx) error
	// schemaVersion reads the recorded schema; zero means none is recorded.
	schemaVersion(*sql.Tx) (int, error)
	setSchemaVersion(*sql.Tx, int) error
	// hasTables reports whether the database holds any table besides version records.
	hasTables(*sql.Tx) (bool, error)
	// jsonText reads a JSON string, or NULL when absent.
	jsonText(document, path string) string
	// jsonInt reads a JSON integer, or NULL when absent.
	jsonInt(document, path string) string
	// jsonValue reads any JSON value as JSON text, or NULL when absent.
	jsonValue(document, path string) string
	// jsonObject builds a JSON object from alternating key literals and expressions.
	jsonObject(pairs ...string) string
	// jsonArray aggregates JSON values in order as JSON array text; no rows is "[]".
	jsonArray(element, order string) string
	// contains reports whether text contains a substring.
	contains(text, substring string) string
	// nowMillis is the database clock in Unix milliseconds.
	nowMillis() string
}
