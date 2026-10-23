package reviewstore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// sidecars move with their database so a backup opens directly with SQLite.
var sidecars = []string{"-journal", "-wal", "-shm"}

// backupSuffix matches files Reset creates beside a database.
var backupSuffix = regexp.MustCompile(`^\.schema\d+-\d{8}T\d{6}\.\d{3}Z\.bak(-journal|-wal|-shm)?$`)

// Reset moves current or older diffx state at path to a timestamped backup
// beside it, then removes earlier backups. It refuses newer or unrecognized
// state and returns "" when no state exists. Opening a store never resets;
// only entry points that own an explicit reset decision call Reset.
func Reset(path string) (string, error) {
	if path == "" || path == ":memory:" {
		return "", errors.New("reset requires a persistent state database")
	}
	name, lock, err := acquireState(path)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := validateStateFile(name); err != nil {
		return "", err
	}
	version, err := resettableVersion(name)
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.schema%d-%s.bak", name, version, time.Now().UTC().Format("20060102T150405.000Z"))
	// Sidecars move first: a journal left beside a new database could be replayed into it.
	var moved []string
	restore := func() {
		for _, suffix := range moved {
			_ = os.Rename(backup+suffix, name+suffix)
		}
	}
	for _, suffix := range sidecars {
		if err := os.Rename(name+suffix, backup+suffix); err == nil {
			moved = append(moved, suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			restore()
			return "", err
		}
	}
	if err := os.Rename(name, backup); err != nil {
		restore()
		return "", err
	}
	if err := pruneBackups(name, backup); err != nil {
		return backup, fmt.Errorf("remove earlier state backups: %w", err)
	}
	return backup, nil
}

// resettableVersion reads the stored schema, refusing newer schemas and
// unversioned databases that already contain tables.
func resettableVersion(path string) (int, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	if version > schemaVersion {
		return 0, newerStateError(path, version)
	}
	if version == 0 {
		var existing string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 1`).Scan(&existing)
		if err == nil {
			return 0, unversionedStateError(path)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	return version, db.Close()
}

// pruneBackups keeps only the newest backup of the database at name.
func pruneBackups(name, backup string) error {
	base, current := filepath.Base(name), filepath.Base(backup)
	entries, err := os.ReadDir(filepath.Dir(name))
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		file := entry.Name()
		suffix, ok := strings.CutPrefix(file, base)
		if !ok || !backupSuffix.MatchString(suffix) || strings.HasPrefix(file, current) {
			continue
		}
		if err := os.Remove(filepath.Join(filepath.Dir(name), file)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
