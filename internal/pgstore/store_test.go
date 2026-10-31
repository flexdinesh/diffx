package pgstore_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestOpenMigratesOnceAndReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.URL(t)
	for range 2 {
		store, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenRefusesSchemaRequiringNewerBinary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE diffx.schema_compatibility SET floor=1000000`); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	_, err = pgstore.Open(ctx, url, pgstore.DefaultRetention)
	var failure *pgstore.SchemaError
	if !errors.As(err, &failure) || failure.Floor != 1000000 {
		t.Fatalf("Open = %v, want schema refusal", err)
	}
}

func TestOpenNeverEchoesInvalidConnectionString(t *testing.T) {
	t.Parallel()
	_, err := pgstore.Open(context.Background(), "postgres://user:hunter2@[bad", pgstore.DefaultRetention)
	if !errors.Is(err, pgstore.ErrInvalidURL) {
		t.Fatalf("Open = %v", err)
	}
}

func TestConcurrentOpensMigrateSafely(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	url := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	// Start from an empty database, as fresh replicas do.
	if _, err := conn.Exec(ctx, `DROP SCHEMA diffx CASCADE`); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			store, err := pgstore.Open(ctx, url, pgstore.DefaultRetention)
			if err == nil {
				err = store.Close()
			}
			errs <- err
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenWaitingRetriesOnlyUnreachableServers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	started := time.Now()
	_, err = pgstore.OpenWaiting(ctx, "postgres://diffx:diffx@"+address+"/diffx?connect_timeout=1", pgstore.DefaultRetention, 2*time.Second)
	if err == nil || time.Since(started) < time.Second {
		t.Fatalf("unreachable server: %v after %s", err, time.Since(started))
	}
	refused := pgtest.URL(t)
	parsed, err := url.Parse(refused)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("diffx", "wrong")
	started = time.Now()
	_, err = pgstore.OpenWaiting(ctx, parsed.String(), pgstore.DefaultRetention, time.Minute)
	if err == nil || time.Since(started) > 10*time.Second {
		t.Fatalf("refused credentials: %v after %s", err, time.Since(started))
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("error echoes password: %v", err)
	}
}

// TestSettingsMigrationEndsAdminInvites upgrades a schema 9 database holding
// a pending admin invite, as the previous release left it.
func TestSettingsMigrationEndsAdminInvites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databaseURL := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `DROP SCHEMA diffx CASCADE; CREATE SCHEMA diffx`); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = pgstore.Schema
	db := stdlib.OpenDB(*config)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("migrations"), goose.WithTableName(pgstore.Schema+".goose_db_version"), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO diffx.account_invites(id, token_hash, email, admin, created_at, expires_at) VALUES
		('admin', 'hash-admin', 'a@example.com', true, now(), now() + interval '1 day'),
		('member', 'hash-member', 'm@example.com', false, now(), now() + interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.Open(ctx, databaseURL, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var adminRevoked, memberRevoked bool
	if err := conn.QueryRow(ctx, `SELECT
		(SELECT revoked_at IS NOT NULL FROM diffx.account_invites WHERE id = 'admin'),
		(SELECT revoked_at IS NOT NULL FROM diffx.account_invites WHERE id = 'member')`).Scan(&adminRevoked, &memberRevoked); err != nil {
		t.Fatal(err)
	}
	if !adminRevoked || memberRevoked {
		t.Fatalf("admin invite revoked %v, member invite revoked %v", adminRevoked, memberRevoked)
	}
	var adminColumn bool
	var floor int64
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'diffx' AND table_name = 'account_invites' AND column_name = 'admin'),
		(SELECT floor FROM diffx.schema_compatibility)`).Scan(&adminColumn, &floor); err != nil {
		t.Fatal(err)
	}
	// The previous release reads the dropped column, so it must refuse this schema.
	if adminColumn || floor != 10 {
		t.Fatalf("admin column %v, floor %d", adminColumn, floor)
	}
	if settings, err := store.AppSettings(ctx); err != nil || !settings.Signup || !settings.PasswordSignup {
		t.Fatalf("seeded settings: %+v %v", settings, err)
	}
}
