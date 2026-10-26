package main

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

func provisionCollector(t *testing.T, store *reviewstore.Store) (reviewstore.User, string) {
	t.Helper()
	identity := identityservice.New(store)
	account := testsupport.RemoteAccount(t, store, "admin")
	token, err := identity.CreateToken(t.Context(), account.ID, "collector", identityservice.PurposeCollector, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return reviewstore.User{ID: account.ID, Name: "admin"}, token.Secret
}
