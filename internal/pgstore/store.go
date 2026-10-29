// Package pgstore is the server's Postgres persistence: every server replica
// shares its state, so no process owns the database.
package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema holds every diffx table, so the server can share a database.
const Schema = "diffx"

// MinimumServerVersion is PostgreSQL 18; migrations may use its features.
const MinimumServerVersion = 180000

const DefaultRetention = 7 * 24 * time.Hour

var ErrNotFound = reviewdata.ErrNotFound

type Store struct {
	pool      *pgxpool.Pool
	queries   *pgdb.Queries
	config    *pgxpool.Config
	retention time.Duration
}

// ErrInvalidURL never carries the connection string, which holds credentials.
var ErrInvalidURL = errors.New("invalid Postgres connection string")

// Config parses a connection string and pins diffx's session settings.
// Pool sizing comes from URL parameters such as pool_max_conns.
func Config(url string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, ErrInvalidURL
	}
	runtime := config.ConnConfig.RuntimeParams
	runtime["search_path"] = Schema
	if runtime["application_name"] == "" {
		runtime["application_name"] = "diffx-server"
	}
	return config, nil
}

// Open migrates the database to this binary's schema and returns a store.
// It refuses servers older than PostgreSQL 18 and schemas this binary cannot run against.
func Open(ctx context.Context, url string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		return nil, errors.New("retention must be positive")
	}
	config, err := Config(url)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := checkServer(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, queries: pgdb.New(pool), config: config, retention: retention}, nil
}

// OpenWaiting retries Open while the server is unreachable, so replicas may
// start before their database. Other failures, such as refused schemas or
// credentials, return immediately.
func OpenWaiting(ctx context.Context, url string, retention time.Duration, patience time.Duration) (*Store, error) {
	deadline := time.Now().Add(patience)
	delay := 250 * time.Millisecond
	for {
		store, err := Open(ctx, url, retention)
		var connect *pgconn.ConnectError
		if err == nil || !errors.As(err, &connect) || connectFailure(connect) || time.Now().Add(delay).After(deadline) {
			return store, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(2*delay, 5*time.Second)
	}
}

// connectFailure reports a server that answered but refused the session, which retrying cannot fix.
func connectFailure(connect *pgconn.ConnectError) bool {
	var server *pgconn.PgError
	return errors.As(connect, &server)
}

func checkServer(ctx context.Context, pool *pgxpool.Pool) error {
	var version int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&version); err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	if version < MinimumServerVersion {
		return fmt.Errorf("Postgres server version %d is unsupported; diffx-server requires PostgreSQL 18 or newer", version)
	}
	return nil
}

// Ping reports whether the database accepts queries.
func (store *Store) Ping(ctx context.Context) error { return store.pool.Ping(ctx) }

func (store *Store) Close() error {
	store.pool.Close()
	return nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
