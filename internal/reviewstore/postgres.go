package reviewstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema_postgres.sql
var postgresSchema string

// postgresOwnerLock is the advisory lock key held by the one owning process of
// a database; advisory locks are scoped to their database.
const postgresOwnerLock int64 = 0x64696666780001

// OpenPostgres opens the database at url, taking the ownership advisory lock on
// a dedicated session. Queries share one other connection, so transactions run
// serially as they do on SQLite. Losing the lock session loses ownership.
func OpenPostgres(ctx context.Context, url string, retention time.Duration) (*Store, error) {
	if retention <= 0 {
		return nil, errors.New("retention must be positive")
	}
	config, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid postgres database URL")
	}
	name := "postgres://" + config.Host + ":" + strconv.Itoa(int(config.Port)) + "/" + config.Database
	db := sql.OpenDB(rebindingConnector{stdlib.GetConnector(*config)})
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	lock, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect %s: %w", name, err)
	}
	var acquired bool
	if err := lock.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(?)`, postgresOwnerLock).Scan(&acquired); err != nil || !acquired {
		_ = lock.Close()
		_ = db.Close()
		if err == nil {
			err = errors.New("another process owns it")
		}
		return nil, fmt.Errorf("state database %s is already in use or unavailable: %w", name, err)
	}
	store := &Store{db: db, dialect: postgresDialect{}, owned: lock.PingContext, retention: retention}
	// The lock session returns to the pool before closing the pool ends it,
	// which releases the advisory lock.
	store.close = func() error { return errors.Join(lock.Close(), db.Close()) }
	if err := store.initialize(name); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

// rebindingConnector rewrites the shared "?" placeholders to Postgres "$n".
type rebindingConnector struct{ driver.Connector }

func (connector rebindingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := connector.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	pgxConn, ok := conn.(*stdlib.Conn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("unexpected postgres driver connection")
	}
	return rebindingConn{pgxConn}, nil
}

type rebindingConn struct{ *stdlib.Conn }

func (conn rebindingConn) Prepare(query string) (driver.Stmt, error) {
	return conn.Conn.Prepare(rebind(query))
}

func (conn rebindingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return conn.Conn.PrepareContext(ctx, rebind(query))
}

func (conn rebindingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return conn.Conn.ExecContext(ctx, rebind(query), args)
}

func (conn rebindingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return conn.Conn.QueryContext(ctx, rebind(query), args)
}

// rebind numbers "?" placeholders outside quoted text and comments.
func rebind(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}
	var result strings.Builder
	result.Grow(len(query) + 16)
	number := 0
	var quote byte
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '-' && i+1 < len(query) && query[i+1] == '-':
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				end = len(query) - i
			}
			result.WriteString(query[i : i+end])
			i += end - 1
			continue
		case c == '?':
			number++
			result.WriteByte('$')
			result.WriteString(strconv.Itoa(number))
			continue
		}
		result.WriteByte(c)
	}
	return result.String()
}

type postgresDialect struct{}

func (postgresDialect) createSchema(tx *sql.Tx) error {
	for _, statement := range strings.Split(postgresSchema, "\n") {
		if statement == "" || strings.HasPrefix(statement, "--") {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (postgresDialect) schemaVersion(tx *sql.Tx) (int, error) {
	var recorded bool
	if err := tx.QueryRow(`SELECT to_regclass('diffx_schema') IS NOT NULL`).Scan(&recorded); err != nil || !recorded {
		return 0, err
	}
	var version int
	err := tx.QueryRow(`SELECT version FROM diffx_schema WHERE singleton=1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func (postgresDialect) setSchemaVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS diffx_schema (singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL)`); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO diffx_schema(singleton,version) VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET version=excluded.version`, version)
	return err
}

func (postgresDialect) hasTables(tx *sql.Tx) (bool, error) {
	var existing bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name<>'diffx_schema')`).Scan(&existing)
	return existing, err
}

func postgresPath(path string) string {
	return `'{` + strings.ReplaceAll(path, ".", ",") + `}'`
}

func (postgresDialect) jsonText(document, path string) string {
	return `(` + document + `::jsonb #>> ` + postgresPath(path) + `)`
}

func (postgresDialect) jsonInt(document, path string) string {
	return `((` + document + `::jsonb #>> ` + postgresPath(path) + `)::bigint)`
}

func (postgresDialect) jsonValue(document, path string) string {
	return `((` + document + `::jsonb #> ` + postgresPath(path) + `)::text)`
}

func (postgresDialect) jsonObject(pairs ...string) string {
	return `json_build_object(` + strings.Join(pairs, ",") + `)`
}

func (postgresDialect) jsonArray(element, order string) string {
	if order != "" {
		element += ` ORDER BY ` + order
	}
	return `COALESCE(json_agg(` + element + `)::text,'[]')`
}

func (postgresDialect) contains(text, substring string) string {
	return `strpos(` + text + `,` + substring + `)>0`
}

func (postgresDialect) nowMillis() string {
	return `(EXTRACT(EPOCH FROM now())*1000)::bigint`
}
