//go:build postgres

package storetest

import (
	"context"
	"testing"

	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/testsupport/postgrestest"
)

// Open returns a store on a fresh Postgres database.
func Open(t testing.TB) (*reviewstore.Store, error) {
	return reviewstore.OpenPostgres(context.Background(), postgrestest.URL(t), reviewstore.DefaultRetention)
}
