//go:build !postgres

package storetest

import (
	"testing"

	"github.com/flexdinesh/diffx/internal/reviewstore"
)

// Open returns a private in-memory SQLite store.
func Open(testing.TB) (*reviewstore.Store, error) {
	return reviewstore.Open("")
}
