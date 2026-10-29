// Package pgtest gives each test its own migrated Postgres database.
//
// DIFFX_TEST_DATABASE_URL (a postgres:// URL whose role may create databases)
// selects a running server; otherwise one container starts per test process
// and Ryuk removes it when the process exits. A template database migrated by
// the embedded schema is created once per schema and cloned for every test.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the pinned server for tests and development.
const Image = "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5"

// EnvURL names the variable selecting an existing server.
const EnvURL = "DIFFX_TEST_DATABASE_URL"

// templateLock serializes template creation across test processes sharing a server.
const templateLock = 7_469_000_100

var shared struct {
	once     sync.Once
	admin    *url.URL
	template string
	err      error
	// clone serializes CREATE DATABASE in this process; the template must
	// have no other sessions while it is copied.
	clone sync.Mutex
}

// URL returns a connection string for a fresh migrated database that is
// dropped when the test ends.
func URL(t testing.TB) string {
	t.Helper()
	shared.once.Do(func() { shared.admin, shared.template, shared.err = setup(context.Background()) })
	if shared.err != nil {
		t.Fatalf("test Postgres unavailable (start Docker or set %s): %v", EnvURL, shared.err)
	}
	name := "diffx_test_" + random()
	if err := admin(func(ctx context.Context, conn *pgx.Conn) error {
		shared.clone.Lock()
		defer shared.clone.Unlock()
		_, err := conn.Exec(ctx, `CREATE DATABASE `+name+` TEMPLATE `+shared.template)
		return err
	}); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if err := admin(func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `DROP DATABASE `+name+` WITH (FORCE)`)
			return err
		}); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	return database(shared.admin, name)
}

// Store opens a store on a fresh database, closed when the test ends.
func Store(t testing.TB) *pgstore.Store {
	t.Helper()
	store, err := pgstore.Open(context.Background(), URL(t), pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func setup(ctx context.Context) (*url.URL, string, error) {
	raw := os.Getenv(EnvURL)
	if raw == "" {
		container, err := postgres.Run(ctx, Image, postgres.WithDatabase("diffx"), postgres.WithUsername("diffx"), postgres.WithPassword("diffx"), postgres.BasicWaitStrategies())
		if err != nil {
			return nil, "", err
		}
		if raw, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			return nil, "", err
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, "", errors.New(EnvURL + " must be a postgres:// URL")
	}
	digest, err := pgstore.MigrationDigest()
	if err != nil {
		return nil, "", err
	}
	template := "diffx_template_" + digest
	shared.admin = parsed
	err = admin(func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(templateLock)); err != nil {
			return err
		}
		defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, int64(templateLock))
		var ready bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1 AND datistemplate)`, template).Scan(&ready); err != nil || ready {
			return err
		}
		// An interrupted earlier attempt may have left an unmigrated database.
		if _, err := conn.Exec(ctx, `DROP DATABASE IF EXISTS `+template+` WITH (FORCE)`); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, `CREATE DATABASE `+template); err != nil {
			return err
		}
		store, err := pgstore.Open(ctx, database(parsed, template), pgstore.DefaultRetention)
		if err != nil {
			return err
		}
		if err := store.Close(); err != nil {
			return err
		}
		_, err = conn.Exec(ctx, `ALTER DATABASE `+template+` IS_TEMPLATE true`)
		return err
	})
	return parsed, template, err
}

func admin(run func(context.Context, *pgx.Conn) error) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, shared.admin.String())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return run(ctx, conn)
}

func database(admin *url.URL, name string) string {
	selected := *admin
	selected.Path = "/" + name
	return selected.String()
}

func random() string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}
