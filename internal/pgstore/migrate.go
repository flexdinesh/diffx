package pgstore

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrations are expand/contract: each release's migrations keep the previous
// release running, so replicas upgrade one at a time. A migration that breaks
// older binaries raises schema_compatibility.floor to its own version; binaries
// that do not know that version then refuse the database instead of corrupting it.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// SchemaError reports a database written by a release this binary cannot run against.
type SchemaError struct {
	Version, Floor, Supported int64
}

func (failure *SchemaError) Error() string {
	return fmt.Sprintf("database schema %d requires a diffx-server supporting schema %d or newer; this binary supports schema %d. Existing data preserved; upgrade diffx-server", failure.Version, failure.Floor, failure.Supported)
}

func migrations() (fs.FS, error) { return fs.Sub(migrationFiles, "migrations") }

// MigrationDigest identifies the embedded migrations, so test harnesses can
// reuse a database migrated by this exact schema.
func MigrationDigest() (string, error) {
	files, err := migrations()
	if err != nil {
		return "", err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, name := range names {
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name + "\x00"))
		hash.Write(content)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)[:8]), nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(schemaLock)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+Schema)
		return err
	}); err != nil {
		return fmt.Errorf("create schema %s: %w", Schema, err)
	}
	files, err := migrations()
	if err != nil {
		return err
	}
	// Replicas starting together wait for the migrating one; probing every
	// second instead of goose's default five keeps their startup short.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker), goose.WithTableName(Schema+".goose_db_version"), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	sources := provider.ListSources()
	if len(sources) == 0 {
		return errors.New("no embedded migrations")
	}
	supported := sources[len(sources)-1].Version
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	// A newer release already migrated; its expand steps keep this binary working.
	if current < supported {
		if _, err := provider.Up(ctx); err != nil {
			return fmt.Errorf("migrate schema: %w", err)
		}
		current = supported
	}
	floor, err := pgdb.New(pool).SchemaFloor(ctx)
	if err != nil {
		return fmt.Errorf("read schema compatibility: %w", err)
	}
	if floor > supported {
		return &SchemaError{Version: current, Floor: floor, Supported: supported}
	}
	return nil
}
