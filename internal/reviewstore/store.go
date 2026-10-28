package reviewstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

const schemaVersion = 15

// additiveSchema is the oldest schema that opens by upgrading in place. Each
// later schema only creates tables and indexes, so the idempotent initialization
// statements migrate it in the same transaction that records the new version.
const additiveSchema = 11

const DefaultRetention = 7 * 24 * time.Hour

var ErrNotFound = reviewdata.ErrNotFound

type User = reviewdata.User

type Binding = reviewdata.Binding

// Store runs application operations on one SQL backend. Its dialect supplies
// backend differences; release gives up database ownership after closing.
type Store struct {
	db        *sql.DB
	dialect   dialect
	release   func() error
	retention time.Duration
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func Open(path string) (*Store, error) {
	return OpenSQLite(path, DefaultRetention)
}

// OpenWithRetention opens SQLite state and configures expiry for new
// observations. Existing expiry and replay identities remain unchanged.
func OpenWithRetention(path string, retention time.Duration) (*Store, error) {
	return OpenSQLite(path, retention)
}

// StateError reports state this binary refuses to open. Version is the stored
// schema; it is zero for unversioned or unrecognized files.
type StateError struct {
	Path    string
	Version int
	reason  string
}

// Older reports diffx state written by an earlier schema that cannot upgrade in place.
func (failure *StateError) Older() bool {
	return failure.Version > 0 && failure.Version < additiveSchema
}

// Supported is the schema this binary opens.
func (failure *StateError) Supported() int { return schemaVersion }

func (failure *StateError) Error() string {
	return fmt.Sprintf("state database %q: %s\n  Existing data preserved; automatic reset is disabled.\n  To start fresh: stop processes using this database, back up %q (including SQLite sidecar files), then retry your command with --state /path/to/new-state.db (choose an unused path).\n  Fresh state has no saved reviews or server accounts.", failure.Path, failure.reason, filepath.Dir(failure.Path))
}

func newerStateError(path string, version int) error {
	return &StateError{Path: path, Version: version, reason: fmt.Sprintf("diffx state schema %d is newer than supported schema %d; to keep existing data, use a diffx binary supporting schema %d", version, schemaVersion, version)}
}

func unversionedStateError(path string) error {
	return &StateError{Path: path, reason: "unversioned state database; check that --state selects the intended diffx database"}
}

// initialize checks the recorded schema and creates the current one in one
// transaction. Opening never resets data.
func (store *Store) initialize(path string) error {
	transaction, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	version, err := store.dialect.schemaVersion(transaction)
	if err != nil {
		return err
	}
	if version > schemaVersion {
		return newerStateError(path, version)
	}
	if version > 0 && version < additiveSchema {
		return &StateError{Path: path, Version: version, reason: fmt.Sprintf("diffx state schema %d is older than supported schema %d; to keep existing data, use a diffx binary supporting schema %d", version, schemaVersion, version)}
	}
	if version == 0 {
		existing, err := store.dialect.hasTables(transaction)
		if err != nil {
			return err
		}
		if existing {
			return unversionedStateError(path)
		}
	}
	if err := store.dialect.createSchema(transaction); err != nil {
		return err
	}
	if err := store.dialect.setSchemaVersion(transaction, schemaVersion); err != nil {
		return err
	}
	return transaction.Commit()
}

func initializeReviews(transaction *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, os_uid TEXT NOT NULL UNIQUE, name TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS diffs (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, kind TEXT NOT NULL, mode TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER)`,
		`CREATE INDEX IF NOT EXISTS diffs_owner_expiry ON diffs(owner_id, kind, expires_at)`,
		`CREATE TABLE IF NOT EXISTS diff_versions (id TEXT PRIMARY KEY, diff_id TEXT NOT NULL REFERENCES diffs(id) ON DELETE CASCADE, revision TEXT NOT NULL, manifest TEXT NOT NULL, created_at INTEGER NOT NULL, UNIQUE(diff_id, revision))`,
		`CREATE TABLE IF NOT EXISTS diff_files (version_id TEXT NOT NULL REFERENCES diff_versions(id) ON DELETE CASCADE, file_id TEXT NOT NULL, path TEXT NOT NULL, fingerprint TEXT NOT NULL, patch TEXT NOT NULL, patch_digest TEXT NOT NULL, PRIMARY KEY(version_id, file_id))`,
		// Review state belongs to a stream and scope, spanning its snapshots. It
		// ends when the stream's last observation is pruned or deleted.
		`CREATE TABLE IF NOT EXISTS reviews (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, stream_key TEXT NOT NULL, mode TEXT NOT NULL, UNIQUE(owner_id, stream_key, mode))`,
		`CREATE TABLE IF NOT EXISTS comments (id TEXT PRIMARY KEY, review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE, version_id TEXT REFERENCES diff_versions(id) ON DELETE SET NULL, file_id TEXT NOT NULL, data TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS comments_review_created ON comments(review_id, created_at)`,
		// Marks carry to snapshots whose stored file patch has the same digest.
		`CREATE TABLE IF NOT EXISTS marks (review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE, file_id TEXT NOT NULL, version_id TEXT REFERENCES diff_versions(id) ON DELETE SET NULL, patch_digest TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(review_id, file_id))`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) Close() error {
	return errors.Join(store.db.Close(), store.release())
}

func (store *Store) User(osUID, name string) (User, error) {
	if osUID == "" || name == "" {
		return User{}, errors.New("current process user is unavailable")
	}
	id, err := newID()
	if err != nil {
		return User{}, err
	}
	_, err = store.db.Exec(`INSERT INTO users(id, os_uid, name, created_at) VALUES(?, ?, ?, ?) ON CONFLICT(os_uid) DO UPDATE SET name=excluded.name`, id, osUID, name, time.Now().UnixMilli())
	if err != nil {
		return User{}, err
	}
	var result User
	err = store.db.QueryRow(`SELECT id, name FROM users WHERE os_uid=?`, osUID).Scan(&result.ID, &result.Name)
	return result, err
}

func ensureReview(transaction *sql.Tx, ownerID, streamKey string, mode review.DiffMode) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = transaction.Exec(`INSERT INTO reviews(id, owner_id, stream_key, mode) VALUES(?, ?, ?, ?) ON CONFLICT(owner_id, stream_key, mode) DO NOTHING`, id, ownerID, streamKey, mode)
	return err
}

// endStreams deletes review state of streams without any remaining observation.
// It runs in the transaction that removed observations.
func endStreams(transaction *sql.Tx) error {
	_, err := transaction.Exec(`DELETE FROM reviews WHERE NOT EXISTS (SELECT 1 FROM observations o WHERE o.owner_id=reviews.owner_id AND o.stream_key=reviews.stream_key)`)
	return err
}

func (store *Store) PruneExpired(now time.Time) error {
	transaction, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec(`DELETE FROM diffs WHERE id IN (SELECT s.diff_id FROM observation_scopes s JOIN contexts c ON c.id=s.context_id JOIN diffs d ON d.id=c.anchor_diff_id WHERE d.expires_at<=?
		UNION SELECT k.diff_id FROM observation_commits k JOIN contexts c ON c.id=k.context_id JOIN diffs d ON d.id=c.anchor_diff_id WHERE d.expires_at<=?)`, now.UnixMilli(), now.UnixMilli()); err != nil {
		return err
	}
	if _, err := transaction.Exec(`DELETE FROM diffs WHERE expires_at<=?`, now.UnixMilli()); err != nil {
		return err
	}
	if err := endStreams(transaction); err != nil {
		return err
	}
	if err := pruneGitHub(transaction, now.UnixMilli()); err != nil {
		return err
	}
	if err := prunePush(transaction, now.UnixMilli()); err != nil {
		return err
	}
	return transaction.Commit()
}
