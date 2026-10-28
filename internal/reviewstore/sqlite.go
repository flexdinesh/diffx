package reviewstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/processlock"
	_ "modernc.org/sqlite"
)

// OpenSQLite opens a file database owned by this process, or a private
// in-memory database for an empty path or ":memory:". A lock beside the file
// keeps one owning process.
func OpenSQLite(path string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		return nil, errors.New("retention must be positive")
	}
	name := ":memory:"
	var lock *processlock.Lock
	if path != "" && path != ":memory:" {
		var err error
		name, lock, err = acquireState(path)
		if err != nil {
			return nil, err
		}
	}
	release := func() error { return nil }
	if lock != nil {
		release = lock.Close
	}
	success := false
	defer func() {
		if !success {
			_ = release()
		}
	}()
	if name != ":memory:" {
		if err := validateStateFile(name); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// The ownership lock file is held until Close.
	store := &Store{db: db, dialect: sqliteDialect{}, owned: func(context.Context) error { return nil }, retention: retention}
	store.close = func() error { return errors.Join(db.Close(), release()) }
	// Schema creation runs without foreign key enforcement; the pragma has no
	// effect inside a transaction.
	if _, err = db.Exec("PRAGMA foreign_keys=OFF; PRAGMA busy_timeout = 5000"); err == nil {
		err = store.initialize(name)
	}
	if err == nil {
		_, err = db.Exec("PRAGMA foreign_keys=ON")
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if name != ":memory:" {
		if err := os.Chmod(name, 0o600); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	success = true
	return store, nil
}

// acquireState canonicalizes aliases before taking the database ownership lock.
func acquireState(path string) (string, *processlock.Lock, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return "", nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", nil, err
	}
	name := filepath.Join(parent, filepath.Base(absolute))
	if existing, err := filepath.EvalSymlinks(name); err == nil {
		name = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	lock, err := processlock.TryAcquire(name + ".lock")
	if err != nil {
		return "", nil, fmt.Errorf("state database is already in use or unavailable: %w", err)
	}
	return name, lock, nil
}

func validateStateFile(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 16)
	count, err := io.ReadFull(file, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	if count == 0 || string(header) == "SQLite format 3\x00" {
		return nil
	}
	return &StateError{Path: path, reason: "unrecognized or unsupported diffx state file; check that --state selects the intended SQLite database"}
}

type sqliteDialect struct{}

func (sqliteDialect) createSchema(tx *sql.Tx) error {
	for _, initialize := range []func(*sql.Tx) error{
		initializeReviews, initializeCatalog, initializeIngestion, initializeQueue,
		initializeAuthentication, initializeGitHub, initializePush,
	} {
		if err := initialize(tx); err != nil {
			return err
		}
	}
	return nil
}

func (sqliteDialect) schemaVersion(tx *sql.Tx) (int, error) {
	var version int
	err := tx.QueryRow("PRAGMA user_version").Scan(&version)
	return version, err
}

func (sqliteDialect) setSchemaVersion(tx *sql.Tx, version int) error {
	_, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version))
	return err
}

func (sqliteDialect) hasTables(tx *sql.Tx) (bool, error) {
	var existing string
	err := tx.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 1`).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func sqlitePath(document, path string) string {
	return `json_extract(` + document + `,'$.` + path + `')`
}

func (sqliteDialect) jsonText(document, path string) string  { return sqlitePath(document, path) }
func (sqliteDialect) jsonInt(document, path string) string   { return sqlitePath(document, path) }
func (sqliteDialect) jsonValue(document, path string) string { return sqlitePath(document, path) }

func (sqliteDialect) jsonObject(pairs ...string) string {
	return `json_object(` + strings.Join(pairs, ",") + `)`
}

func (sqliteDialect) jsonArray(element, order string) string {
	if order == "" {
		return `json_group_array(` + element + `)`
	}
	return `json_group_array(` + element + ` ORDER BY ` + order + `)`
}

func (sqliteDialect) contains(text, substring string) string {
	return `instr(` + text + `,` + substring + `)>0`
}

func (sqliteDialect) nowMillis() string { return `CAST(strftime('%s','now') AS INTEGER)*1000` }
