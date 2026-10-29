package diffsource

import (
	"context"
	"testing"
	"time"
)

func TestCommandDiagnosticsDistinguishDeadlineOwners(t *testing.T) {
	expired := time.Now().Add(-time.Second)
	parent, cancel := context.WithDeadline(t.Context(), expired)
	defer cancel()
	inherited, cancelInherited := context.WithTimeout(parent, 30*time.Second)
	defer cancelInherited()
	inherited = WithCollectionDeadline(inherited, parent)
	if got := deadlineKind(inherited, inherited); got != "caller" {
		t.Fatalf("caller deadline reported as %s", got)
	}
	collection, cancelCollection := context.WithDeadline(t.Context(), expired)
	defer cancelCollection()
	collection = WithCollectionDeadline(collection, t.Context())
	if got := deadlineKind(collection, collection); got != "collection" {
		t.Fatalf("collection deadline reported as %s", got)
	}
	if got := deadlineKind(t.Context(), parent); got != "command" {
		t.Fatalf("command deadline reported as %s", got)
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if got := deadlineKind(cancelled, cancelled); got != "cancelled" {
		t.Fatalf("cancellation reported as %s", got)
	}
	if got := deadlineKind(t.Context(), t.Context()); got != "none" {
		t.Fatalf("successful command reported as %s", got)
	}
}
