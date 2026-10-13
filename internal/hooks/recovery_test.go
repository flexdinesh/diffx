package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

func recoveryFixture(t *testing.T, target Target) (Engine, string, Event, pending) {
	t.Helper()
	engine, job := fixture(t)
	event := Event{Path: filepath.Join(t.TempDir(), "removed-checkout"), ConfigFile: "/collector-config.json", Routing: "old-credential", RecoveryRouting: "server-and-policy", Agent: "test", RunID: "original-session"}
	if err := engine.Schedule(event); err != nil {
		t.Fatal(err)
	}
	directory, err := engine.jobPath(*job)
	if err != nil {
		t.Fatal(err)
	}
	saved := pending{
		state:   state{Target: target, Fingerprint: "immutable-contents", At: time.Now()},
		Request: ingestion.Request{SubmissionID: "possibly-committed", Metadata: ingestion.Metadata{Root: event.Path, SourceID: "original-source", Branch: "original-branch", RunID: event.RunID}},
	}
	for _, name := range []string{"capture.pending.json", key(target.Destination, target.SourceID) + ".pending.json"} {
		if err := writeJSON(filepath.Join(directory, name), saved); err != nil {
			t.Fatal(err)
		}
	}
	engine.Expand = func(context.Context, Event) ([]Event, error) {
		t.Fatal("recovery attempted workspace discovery")
		return nil, nil
	}
	engine.Normalize = func(_ context.Context, event Event) (Event, error) {
		t.Fatal("recovery attempted checkout normalization")
		return event, nil
	}
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		t.Fatal("recovery recollected immutable payload")
		return ingestion.Request{}, "", nil
	}
	return engine, *job, event, saved
}

func TestCredentialRotationRecoversExactBoundCaptureWithoutCheckout(t *testing.T) {
	target := Target{Destination: "remote:server:owner", Identity: "database:owner", SourceID: "source"}
	engine, originalJob, event, saved := recoveryFixture(t, target)
	var launches []string
	engine.Launch = func(job string) error { launches = append(launches, job); return nil }
	engine.Resolve = func(_ context.Context, received Event) (Target, error) {
		if received.Routing != "rotated-credential" || received.RunID != event.RunID {
			t.Fatalf("wrong recovery credential or session: %+v", received)
		}
		return target, nil
	}
	deliveries := 0
	engine.Deliver = func(_ context.Context, receivedTarget Target, request ingestion.Request) (string, error) {
		deliveries++
		if receivedTarget != target || !reflect.DeepEqual(request, saved.Request) {
			t.Fatalf("recovery changed target or payload: target=%+v request=%+v", receivedTarget, request)
		}
		return "committed-context", nil
	}
	event.Routing, event.Retry = "rotated-credential", true
	if err := engine.Retry(event); err != nil || len(launches) != 1 || launches[0] == originalJob {
		t.Fatalf("recovery worker not isolated: launches=%v err=%v", launches, err)
	}
	if err := engine.Run(t.Context(), launches[0]); err != nil {
		t.Fatal(err)
	}
	// A detached launcher can start the same job twice. The second worker must
	// recognize the completed recovery after its immutable files were removed.
	if err := engine.Run(t.Context(), launches[0]); err != nil {
		t.Fatalf("completed recovery could not be replayed safely: %v", err)
	}
	if deliveries != 1 {
		t.Fatalf("deliveries=%d", deliveries)
	}
	directory, _ := engine.jobPath(originalJob)
	for _, name := range []string{"capture.pending.json", key(target.Destination, target.SourceID) + ".pending.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("acknowledged capture retained: %s %v", name, err)
		}
	}
	var ack state
	if err := readJSON(filepath.Join(directory, key(target.Destination, target.SourceID)+".ack.json"), &ack); err != nil || ack.Target != target || ack.Fingerprint != saved.Fingerprint || ack.ContextID != "committed-context" {
		t.Fatalf("recovery acknowledgement=%+v err=%v", ack, err)
	}
	launches = nil
	if err := engine.Schedule(event); err != nil || len(launches) != 1 || launches[0] == originalJob {
		t.Fatalf("fresh credential reused original scheduling job: launches=%v err=%v", launches, err)
	}
}

func TestCrossRoutingRecoveryFencesAuthenticatedTarget(t *testing.T) {
	original := Target{Destination: "remote:server:owner", Identity: "database:owner", SourceID: "source"}
	for _, test := range []struct {
		name   string
		target Target
	}{
		{"different account", Target{Destination: "remote:server:other-owner", Identity: "database:other-owner", SourceID: original.SourceID}},
		{"replacement database", Target{Destination: original.Destination, Identity: "replacement:owner", SourceID: original.SourceID}},
		{"different source", Target{Destination: original.Destination, Identity: original.Identity, SourceID: "other-source"}},
		{"unknown account", Target{Destination: original.Destination, SourceID: original.SourceID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, originalJob, event, saved := recoveryFixture(t, original)
			var launches []string
			engine.Launch = func(job string) error { launches = append(launches, job); return nil }
			engine.Resolve = func(context.Context, Event) (Target, error) { return test.target, nil }
			engine.Deliver = func(context.Context, Target, ingestion.Request) (string, error) {
				t.Fatal("recovery delivered into a different target")
				return "", nil
			}
			event.Routing, event.Retry = "rotated-credential", true
			if err := engine.Retry(event); err != nil || len(launches) != 1 {
				t.Fatalf("recovery scheduling: launches=%v err=%v", launches, err)
			}
			if err := engine.Run(t.Context(), launches[0]); err == nil {
				t.Fatal("target mismatch accepted")
			}
			directory, _ := engine.jobPath(originalJob)
			for _, name := range []string{"capture.pending.json", key(original.Destination, original.SourceID) + ".pending.json"} {
				var retained pending
				if err := readJSON(filepath.Join(directory, name), &retained); err != nil || !reflect.DeepEqual(retained.Request, saved.Request) || retained.Target != original {
					t.Fatalf("mismatched target lost immutable capture: %+v %v", retained, err)
				}
			}
		})
	}
}

func TestRecoveryPreservesOriginalEventScheduledDuringDelivery(t *testing.T) {
	target := Target{Destination: "remote:server:owner", Identity: "database:owner", SourceID: "source"}
	engine, originalJob, originalEvent, saved := recoveryFixture(t, target)
	originalEvent.Resolved = true
	var launches []string
	engine.Launch = func(job string) error { launches = append(launches, job); return nil }
	engine.Resolve = func(context.Context, Event) (Target, error) { return target, nil }
	deliveries := 0
	engine.Deliver = func(ctx context.Context, _ Target, request ingestion.Request) (string, error) {
		deliveries++
		if !reflect.DeepEqual(request, saved.Request) {
			t.Fatalf("recovery changed immutable request: %+v", request)
		}
		// A trigger arrives while recovery owns the original worker lock. Its
		// launched worker coalesces, leaving the new event for an original retry.
		if err := engine.Schedule(originalEvent); err != nil {
			t.Fatal(err)
		}
		if err := engine.Run(ctx, originalJob); err != nil {
			t.Fatalf("original worker did not coalesce: %v", err)
		}
		return "recovered-context", nil
	}
	recoveryEvent := originalEvent
	recoveryEvent.Routing, recoveryEvent.Retry = "rotated-credential", true
	if err := engine.Retry(recoveryEvent); err != nil || len(launches) != 1 {
		t.Fatalf("recovery scheduling: launches=%v err=%v", launches, err)
	}
	recoveryJob := launches[0]
	if err := engine.Run(t.Context(), recoveryJob); err != nil {
		t.Fatal(err)
	}
	directory, _ := engine.jobPath(originalJob)
	var status Activity
	if err := readJSON(filepath.Join(directory, "status.json"), &status); err != nil || status.Status != "queued" {
		t.Fatalf("recovery discarded newer original event: status=%+v err=%v", status, err)
	}
	if deliveries != 1 || len(launches) != 2 || launches[1] != originalJob {
		t.Fatalf("recovery relaunched original environment: deliveries=%d launches=%v", deliveries, launches)
	}
	// Resume in the original credential environment. Its new queued event must
	// still collect and publish, independently of the completed saved recovery.
	engine.Normalize = func(_ context.Context, event Event) (Event, error) { return event, nil }
	collections := 0
	engine.Collect = func(_ context.Context, event Event, _ string) (ingestion.Request, string, error) {
		collections++
		if event.Routing != originalEvent.Routing {
			t.Fatalf("new collection used replacement credential: %+v", event)
		}
		return ingestion.Request{SubmissionID: "newly-scheduled"}, "new-contents", nil
	}
	engine.Deliver = func(_ context.Context, _ Target, request ingestion.Request) (string, error) {
		deliveries++
		if request.SubmissionID != "newly-scheduled" {
			t.Fatalf("original retry replayed completed recovery: %+v", request)
		}
		return "new-context", nil
	}
	launches = nil
	originalEvent.Retry = true
	if err := engine.Retry(originalEvent); err != nil || len(launches) != 1 || launches[0] != originalJob {
		t.Fatalf("new original event not retried: launches=%v err=%v", launches, err)
	}
	if err := engine.Run(t.Context(), originalJob); err != nil {
		t.Fatal(err)
	}
	if collections != 1 || deliveries != 2 {
		t.Fatalf("new original event lost: collections=%d deliveries=%d", collections, deliveries)
	}
	if err := readJSON(filepath.Join(directory, "status.json"), &status); err != nil || status.Status != "complete" || status.ContextID != "new-context" {
		t.Fatalf("original completion: status=%+v err=%v", status, err)
	}
}

func TestCrossRoutingRetryRequiresBoundOwnerAndMatchingPolicy(t *testing.T) {
	original := Target{Destination: "remote:server:owner", Identity: "database:owner", SourceID: "source"}
	for _, name := range []string{"unknown owner", "different recovery policy", "missing recovery policy", "different config"} {
		t.Run(name, func(t *testing.T) {
			target := original
			if name == "unknown owner" {
				target.Identity = ""
			}
			engine, originalJob, event, saved := recoveryFixture(t, target)
			var launches []string
			engine.Launch = func(job string) error { launches = append(launches, job); return nil }
			engine.Resolve = func(context.Context, Event) (Target, error) {
				t.Fatal("ineligible capture contacted replacement destination")
				return Target{}, nil
			}
			event.Routing, event.Retry = "rotated-credential", true
			switch name {
			case "different recovery policy":
				event.RecoveryRouting = "different-server-or-policy"
			case "missing recovery policy":
				event.RecoveryRouting = ""
			case "different config":
				event.ConfigFile = "/different-collector-config.json"
			}
			if err := engine.Retry(event); err != nil || len(launches) != 0 {
				t.Fatalf("ineligible capture recovered: launches=%v err=%v", launches, err)
			}
			directory, _ := engine.jobPath(originalJob)
			var retained pending
			if err := readJSON(filepath.Join(directory, "capture.pending.json"), &retained); err != nil || !reflect.DeepEqual(retained.Request, saved.Request) || retained.Target != target {
				t.Fatalf("ineligible capture lost: %+v %v", retained, err)
			}
		})
	}
}

func TestCrossRoutingRecoveryRetainsConflictingPayloads(t *testing.T) {
	target := Target{Destination: "remote:server:owner", Identity: "database:owner", SourceID: "source"}
	engine, originalJob, event, saved := recoveryFixture(t, target)
	directory, _ := engine.jobPath(originalJob)
	conflicting := saved
	conflicting.Request.Metadata.Branch = "different-contents"
	if err := writeJSON(filepath.Join(directory, "capture.pending.json"), conflicting); err != nil {
		t.Fatal(err)
	}
	var launches []string
	engine.Launch = func(job string) error { launches = append(launches, job); return nil }
	engine.Resolve = func(context.Context, Event) (Target, error) { return target, nil }
	engine.Deliver = func(context.Context, Target, ingestion.Request) (string, error) {
		t.Fatal("conflicting immutable captures delivered")
		return "", nil
	}
	event.Routing, event.Retry = "rotated-credential", true
	if err := engine.Retry(event); err != nil || len(launches) != 1 {
		t.Fatalf("recovery scheduling: launches=%v err=%v", launches, err)
	}
	if err := engine.Run(t.Context(), launches[0]); err == nil {
		t.Fatal("conflicting payloads accepted")
	}
	for name, expected := range map[string]pending{"capture.pending.json": conflicting, key(target.Destination, target.SourceID) + ".pending.json": saved} {
		var retained pending
		if err := readJSON(filepath.Join(directory, name), &retained); err != nil || !reflect.DeepEqual(retained.Request, expected.Request) {
			t.Fatalf("conflicting payload lost: %s %+v %v", name, retained, err)
		}
	}
}

func TestCaptureSurvivesDestinationFailureAndSourceRemoval(t *testing.T) {
	engine, job := fixture(t)
	available, healthy := true, false
	collections, deliveries := 0, 0
	engine.Normalize = func(_ context.Context, event Event) (Event, error) {
		if !available {
			return event, ErrUnavailable
		}
		return event, nil
	}
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		collections++
		return ingestion.Request{SubmissionID: "immutable"}, "contents", nil
	}
	engine.Resolve = func(context.Context, Event) (Target, error) {
		directory, _ := engine.jobPath(*job)
		var captured pending
		if err := readJSON(filepath.Join(directory, "capture.pending.json"), &captured); err != nil || captured.Request.SubmissionID != "immutable" {
			t.Fatalf("health contacted before durable capture: %#v %v", captured, err)
		}
		if !healthy {
			return Target{}, errors.New("server unavailable")
		}
		return Target{Destination: "local", Identity: "database", SourceID: "machine"}, nil
	}
	engine.Deliver = func(_ context.Context, _ Target, request ingestion.Request) (string, error) {
		deliveries++
		if request.SubmissionID != "immutable" {
			t.Fatal("retry replaced immutable submission")
		}
		return "context", nil
	}
	trigger(t, engine)
	if err := engine.Run(t.Context(), *job); err == nil {
		t.Fatal("destination failure ignored")
	}
	available, healthy = false, true
	if err := engine.Run(t.Context(), *job); err != nil {
		t.Fatal(err)
	}
	if collections != 1 || deliveries != 1 {
		t.Fatalf("collections=%d deliveries=%d", collections, deliveries)
	}
	statuses, err := engine.Statuses()
	if err != nil || len(statuses) != 1 || statuses[0].Status != "complete" || statuses[0].ContextID != "context" {
		t.Fatalf("delivery status: %#v %v", statuses, err)
	}
}

func TestRemovedSourceCannotReplayIntoReplacementDatabase(t *testing.T) {
	engine, job := fixture(t)
	available := true
	identity := "original"
	deliveries := 0
	engine.Normalize = func(_ context.Context, event Event) (Event, error) {
		if !available {
			return event, ErrUnavailable
		}
		return event, nil
	}
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		return ingestion.Request{SubmissionID: "possibly-committed"}, "contents", nil
	}
	engine.Resolve = func(context.Context, Event) (Target, error) {
		return Target{Destination: "local", Identity: identity, SourceID: "machine"}, nil
	}
	engine.Deliver = func(context.Context, Target, ingestion.Request) (string, error) {
		deliveries++
		return "", errors.New("receipt lost")
	}
	trigger(t, engine)
	_ = engine.Run(t.Context(), *job)
	available, identity = false, "replacement"
	if err := engine.Run(t.Context(), *job); err == nil || deliveries != 1 {
		t.Fatalf("replacement replayed ambiguous request: deliveries=%d error=%v", deliveries, err)
	}
}

func TestStableJobFollowsMoveAndBranchRename(t *testing.T) {
	engine, job := fixture(t)
	old := filepath.Join(t.TempDir(), "old")
	current := filepath.Join(t.TempDir(), "moved")
	first := Event{Path: old, InputPath: old, Identity: "stable-source", Branch: "feature", Base: "auto", Resolved: true}
	if err := engine.Schedule(first); err != nil {
		t.Fatal(err)
	}
	originalJob := *job
	renamed := first
	renamed.Path, renamed.Branch = current, "renamed"
	if err := engine.Schedule(renamed); err != nil || *job != originalJob {
		t.Fatalf("move changed job: %s %s %v", originalJob, *job, err)
	}
	engine.Normalize = func(_ context.Context, event Event) (Event, error) {
		if event.Path != current || event.Branch != "renamed" {
			t.Fatalf("stale source selected: %#v", event)
		}
		return event, nil
	}
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		return ingestion.Request{SubmissionID: "current"}, "contents", nil
	}
	engine.Deliver = func(context.Context, Target, ingestion.Request) (string, error) { return "context", nil }
	// A worker already holding the pre-move event must use the current registry.
	directory, _ := engine.jobPath(originalJob)
	if err := engine.sync(t.Context(), directory, first); err != nil {
		t.Fatal(err)
	}
}

func TestRetryIsRouteScopedAndSkipsCompletedJobs(t *testing.T) {
	engine, _ := fixture(t)
	var launches []string
	engine.Launch = func(key string) error { launches = append(launches, key); return nil }
	for _, route := range []string{"first", "second"} {
		if err := engine.Schedule(Event{Path: "/checkout", Routing: route}); err != nil {
			t.Fatal(err)
		}
	}
	launches = nil
	if err := engine.Retry(Event{Routing: "first"}); err != nil || len(launches) != 0 {
		t.Fatalf("just-scheduled job relaunched automatically: %v %v", launches, err)
	}
	if err := engine.Retry(Event{Routing: "first", Retry: true}); err != nil || len(launches) != 1 {
		t.Fatalf("retry routes: %v %v", launches, err)
	}
	var request queued
	directory, _ := engine.jobPath(launches[0])
	if err := readJSON(filepath.Join(directory, "request.json"), &request); err != nil || request.Event.Routing != "first" {
		t.Fatalf("wrong route replayed: %#v %v", request, err)
	}
	engine.RecordJob(launches[0], request.Event, Activity{Status: "waiting"})
	launches = nil
	if err := engine.Retry(Event{Routing: "first"}); err != nil || len(launches) != 0 {
		t.Fatalf("recently attempted job relaunched automatically: %v %v", launches, err)
	}
	if err := engine.Retry(Event{Routing: "first", Retry: true}); err != nil || len(launches) != 1 {
		t.Fatalf("explicit retry ignored backoff: %v %v", launches, err)
	}
	engine.RecordJob(launches[0], request.Event, Activity{Status: "complete"})
	launches = nil
	if err := engine.Retry(Event{Routing: "first", Retry: true}); err != nil || len(launches) != 0 {
		t.Fatalf("completed job relaunched: %v %v", launches, err)
	}
}

func TestLegacyPendingReplayDoesNotRequireDiscoveryOrCheckout(t *testing.T) {
	engine, job := fixture(t)
	engine.Expand = func(context.Context, Event) ([]Event, error) {
		t.Fatal("legacy immutable payload was sent through workspace discovery")
		return nil, nil
	}
	engine.Normalize = func(_ context.Context, event Event) (Event, error) { return event, ErrUnavailable }
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		t.Fatal("legacy removed source was collected")
		return ingestion.Request{}, "", nil
	}
	engine.Deliver = func(_ context.Context, _ Target, request ingestion.Request) (string, error) {
		if request.SubmissionID != "legacy" {
			t.Fatalf("legacy request replaced: %#v", request)
		}
		return "context", nil
	}
	trigger(t, engine)
	directory, _ := engine.jobPath(*job)
	target, _ := engine.Resolve(t.Context(), Event{})
	saved := pending{state: state{Target: target, Fingerprint: "legacy-state", At: time.Now()}, Request: ingestion.Request{SubmissionID: "legacy"}}
	if err := writeJSON(filepath.Join(directory, key(target.Destination, target.SourceID)+".pending.json"), saved); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(t.Context(), *job); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableSourceWithNoCaptureRemainsWaiting(t *testing.T) {
	engine, job := fixture(t)
	engine.Normalize = func(_ context.Context, event Event) (Event, error) { return event, ErrUnavailable }
	engine.Collect = func(context.Context, Event, string) (ingestion.Request, string, error) {
		t.Fatal("unavailable source was collected")
		return ingestion.Request{}, "", nil
	}
	engine.Deliver = func(context.Context, Target, ingestion.Request) (string, error) {
		t.Fatal("uncaptured edits were fabricated")
		return "", nil
	}
	trigger(t, engine)
	if err := engine.Run(t.Context(), *job); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing source error: %v", err)
	}
	directory, _ := engine.jobPath(*job)
	if _, err := os.Stat(filepath.Join(directory, "capture.pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fabricated capture: %v", err)
	}
}
