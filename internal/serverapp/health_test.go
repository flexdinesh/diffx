package serverapp_test

import (
	"errors"
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

func TestHealthIdentitySurvivesRestartAndRequiresAuthentication(t *testing.T) {
	databaseURL := pgtest.URL(t)
	first := start(t, databaseURL, true)
	if _, err := testsupport.NewClient(first.server.URL, "wrong").Health(t.Context()); err == nil {
		t.Fatal("unauthenticated health accepted")
	}
	health, err := testsupport.NewClient(first.server.URL, first.token).Health(t.Context())
	if err != nil || health.StateID != first.owner {
		t.Fatalf("health %#v, %v", health, err)
	}
	first.close()
	second := start(t, databaseURL, true)
	restarted, err := testsupport.NewClient(second.server.URL, second.token).Health(t.Context())
	if err != nil || restarted != health {
		t.Fatalf("persistent identity changed on restart: %#v, %v", restarted, err)
	}
	replacement := start(t, "", true)
	replaced, err := testsupport.NewClient(replacement.server.URL, replacement.token).Health(t.Context())
	if err != nil || replaced.StateID == health.StateID {
		t.Fatalf("replacement retained old identity: %#v, %v", replaced, err)
	}
}

func TestIngestionRejectsChangedDestinationBeforeSaving(t *testing.T) {
	input := collectGit(t)
	destination := start(t, "", true)
	client := testsupport.NewClient(destination.server.URL, destination.token)
	_, err := client.SubmitTo(t.Context(), input, "old-database")
	var problem *ingestion.Problem
	if !errors.As(err, &problem) || problem.Status != 409 {
		t.Fatalf("replacement destination accepted: %v", err)
	}
	if _, err := client.SubmitTo(t.Context(), input, destination.owner); err != nil {
		t.Fatalf("valid destination rejected: %v", err)
	}
}
