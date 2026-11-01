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

// migrateTo resets the database to schema version and returns a connection.
func migrateTo(t *testing.T, databaseURL string, version int64) *pgx.Conn {
	t.Helper()
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
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
	if _, err := provider.UpTo(ctx, version); err != nil {
		t.Fatal(err)
	}
	return conn
}

// TestEmailIdentityMigration upgrades a schema 11 database holding a Google
// account, an email-named password account and the operator's legacy handle admin.
func TestEmailIdentityMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databaseURL := pgtest.URL(t)
	conn := migrateTo(t, databaseURL, 11)
	if _, err := conn.Exec(ctx, `
		INSERT INTO diffx.users(id, name, created_at) VALUES ('google', 'gname', now()), ('emailed', 'p@example.com', now()), ('legacy', 'owner', now());
		INSERT INTO diffx.remote_accounts(user_id, username, password_hash, email) VALUES
			('google', 'gname', '', NULL), ('emailed', 'p@example.com', 'hash', 'p@example.com'), ('legacy', 'owner', 'hash', NULL);
		INSERT INTO diffx.google_identities(sub, user_id, email, updated_at) VALUES ('subject', 'google', 'g@example.com', now());
		INSERT INTO diffx.admin_account(user_id) VALUES ('legacy')`); err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.Open(ctx, databaseURL, pgstore.DefaultRetention)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for id, want := range map[string]string{"google": "g@example.com", "emailed": "p@example.com", "legacy": "flexdinesh@gmail.com"} {
		var email, name string
		if err := conn.QueryRow(ctx, `SELECT a.email, u.name FROM diffx.remote_accounts a JOIN diffx.users u ON u.id = a.user_id WHERE a.user_id = $1`, id).Scan(&email, &name); err != nil || email != want || name != want {
			t.Fatalf("%s: email %q, name %q, %v", id, email, name, err)
		}
	}
	if credential, err := store.AccountPassword(ctx, "flexdinesh@gmail.com"); err != nil || credential.Account.ID != "legacy" || !credential.Account.Admin {
		t.Fatalf("legacy admin by email: %+v %v", credential.Account, err)
	}
	var usernameColumn bool
	var floor int64
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'diffx' AND table_name = 'remote_accounts' AND column_name = 'username'),
		(SELECT floor FROM diffx.schema_compatibility)`).Scan(&usernameColumn, &floor); err != nil {
		t.Fatal(err)
	}
	// The previous release reads the dropped column, so it must refuse this schema.
	if usernameColumn || floor != 12 {
		t.Fatalf("username column %v, floor %d", usernameColumn, floor)
	}
}

// Only one legacy handle account takes the operator's email; a second fails
// the upgrade instead of sharing it.
func TestEmailIdentityMigrationRefusesSecondHandle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databaseURL := pgtest.URL(t)
	conn := migrateTo(t, databaseURL, 11)
	if _, err := conn.Exec(ctx, `
		INSERT INTO diffx.users(id, name, created_at) VALUES ('first', 'first', now()), ('second', 'second', now());
		INSERT INTO diffx.remote_accounts(user_id, username, password_hash) VALUES ('first', 'first', 'hash'), ('second', 'second', 'hash')`); err != nil {
		t.Fatal(err)
	}
	if store, err := pgstore.Open(ctx, databaseURL, pgstore.DefaultRetention); err == nil {
		store.Close()
		t.Fatal("two handle accounts shared an email")
	}
}

// TestSettingsMigrationEndsAdminInvites upgrades a schema 9 database holding
// a pending admin invite, as the previous release left it.
func TestSettingsMigrationEndsAdminInvites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databaseURL := pgtest.URL(t)
	conn := migrateTo(t, databaseURL, 9)
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
	if adminColumn || floor < 10 {
		t.Fatalf("admin column %v, floor %d", adminColumn, floor)
	}
	if settings, err := store.AppSettings(ctx); err != nil || !settings.Signup || !settings.PasswordSignup {
		t.Fatalf("seeded settings: %+v %v", settings, err)
	}
}
