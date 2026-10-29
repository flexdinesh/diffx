package main

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

func provisionCollector(t *testing.T, store *pgstore.Store) (reviewdata.User, string) {
	t.Helper()
	identity := identityservice.New(store)
	account := testsupport.RemoteAccount(t, store, "admin")
	token, err := identity.CreateToken(t.Context(), account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return reviewdata.User{ID: account.ID, Name: "admin"}, token.Secret
}
