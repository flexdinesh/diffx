//go:build postgres

package postgrestest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net/url"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image matches the deployed PostgreSQL major version.
const Image = "postgres:18-alpine"

var server struct {
	once  sync.Once
	admin *sql.DB
	url   *url.URL
	err   error
}

// start runs one container per test process; the testcontainers reaper
// removes it when the process exits.
func start() {
	ctx := context.Background()
	container, err := postgres.Run(ctx, Image, postgres.WithPassword("diffx-test-secret"), postgres.BasicWaitStrategies())
	if err != nil {
		server.err = err
		return
	}
	raw, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		server.err = err
		return
	}
	server.url, server.err = url.Parse(raw)
	if server.err == nil {
		server.admin, server.err = sql.Open("pgx", raw)
	}
}

// URL creates an empty database and returns its connection URL.
func URL(t testing.TB) string {
	t.Helper()
	server.once.Do(start)
	if server.err != nil {
		t.Fatalf("start postgres container: %v", server.err)
	}
	name := "test_" + strings.ToLower(rand.Text())
	if _, err := server.admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatalf("create postgres database: %v", err)
	}
	database := *server.url
	database.Path = "/" + name
	return database.String()
}
