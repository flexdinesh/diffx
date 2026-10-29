package pushservice_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/githubsync"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// pushEndpoint is a push service: respond decides each answer by call index.
type pushEndpoint struct {
	server  *httptest.Server
	mu      sync.Mutex
	calls   int
	bodies  [][]byte
	headers []http.Header
	respond func(call int, w http.ResponseWriter, r *http.Request)
}

func newEndpoint(t *testing.T, respond func(call int, w http.ResponseWriter, r *http.Request)) *pushEndpoint {
	t.Helper()
	endpoint := &pushEndpoint{respond: respond}
	endpoint.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		endpoint.mu.Lock()
		call := endpoint.calls
		endpoint.calls++
		endpoint.bodies, endpoint.headers = append(endpoint.bodies, body), append(endpoint.headers, r.Header.Clone())
		endpoint.mu.Unlock()
		endpoint.respond(call, w, r)
	}))
	t.Cleanup(endpoint.server.Close)
	return endpoint
}

func status(code int) func(int, http.ResponseWriter, *http.Request) {
	return func(_ int, w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func (endpoint *pushEndpoint) count() int {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	return endpoint.calls
}

type harness struct {
	store   *pgstore.Store
	service *pushservice.Service
	clock   *time.Time
	owner   identityservice.Account
	pulls   int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := pgtest.Store(t)
	clock := time.Now()
	h := &harness{store: store, clock: &clock, owner: testsupport.RemoteAccount(t, store, "alice")}
	h.service = h.restart(t)
	return h
}

// restart composes a new service over the same state, as a server restart does.
func (h *harness) restart(t *testing.T) *pushservice.Service {
	t.Helper()
	service, err := pushservice.New(t.Context(), h.store.Push(), pushservice.Options{PublicURL: "http://localhost:7980", Now: func() time.Time { return *h.clock }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func (h *harness) advance(d time.Duration) { *h.clock = h.clock.Add(d) }

func (h *harness) subscribe(t *testing.T, endpoint *pushEndpoint) testsupport.PushDevice {
	t.Helper()
	device := testsupport.NewPushDevice(t)
	if _, err := h.service.Subscribe(t.Context(), h.owner.ID, pushservice.SubscriptionInput{Endpoint: endpoint.server.URL, P256dh: device.P256dh(), Auth: device.AuthSecret(), Label: "Phone"}); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Millisecond)
	return device
}

func request(submission string) ingestion.Request {
	head := "1111111111111111111111111111111111111111"
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission,
		Metadata: ingestion.Metadata{SourceID: "source", Hostname: "host", RepositoryKey: "repo", RepositoryName: "app", CheckoutKey: "checkout",
			Root: "/app", Branch: "feature-" + submission, Head: &head, CollectedAt: 1, Trigger: "manual", CollectorVersion: "test"},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{Source: "local", Root: "/app", Branch: "feature-" + submission, Head: &head, Mode: review.DiffAll, Revision: "revision-" + submission,
				Files: []review.ChangedFile{{ID: "f", Path: "search.go", Status: "M", Fingerprint: "fp-" + submission}}},
			Patches: map[string]review.FilePatch{"f": {Patch: "@@ -1 +1 @@\n-a\n+b\n"}},
		}},
	}
}

// notify records a pull request notification for the owner the way a GitHub
// refresh does: admitted with its ingestion job in one transaction.
func (h *harness) notify(t *testing.T) {
	t.Helper()
	h.pulls++
	github, now := h.store.GitHub(), h.clock.UnixMilli()
	ref := githubsync.PullRequestRef{RepositoryID: 10, InstallationID: 5, FullName: "octo/app", Number: h.pulls}
	if err := github.RequestRefresh(t.Context(), ref, "", true, now); err != nil {
		t.Fatal(err)
	}
	lease, err := github.ClaimRefresh(t.Context(), "lease-"+strconv.Itoa(h.pulls), now, now+time.Hour.Milliseconds())
	if err != nil {
		t.Fatal(err)
	}
	if err := github.RecordLease(t.Context(), lease, []byte("{}"), now); err != nil {
		t.Fatal(err)
	}
	admission := githubsync.Admission{OwnerID: h.owner.ID, Request: request(strconv.Itoa(h.pulls)), Notification: &githubsync.Notification{Title: "octo/app #1", Body: "Add search\nNew PR by @mona"}}
	if err := github.CompleteRefresh(t.Context(), lease, "open", "head", []githubsync.Admission{admission}, now); err != nil {
		t.Fatal(err)
	}
}

// publish runs the queued ingestion job, returning the published stream.
func (h *harness) publish(t *testing.T) string {
	t.Helper()
	delivery, err := h.store.Queue().Claim(t.Context(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := h.store.IngestContext(t.Context(), delivery.OwnerID, delivery.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Queue().Complete(t.Context(), delivery, binding.ContextID); err != nil {
		t.Fatal(err)
	}
	item, err := h.store.Context(t.Context(), delivery.OwnerID, binding.ContextID, time.Now())
	if err != nil || item.StreamID == nil {
		t.Fatalf("published context %+v %v", item, err)
	}
	return *item.StreamID
}

func (h *harness) deliver(t *testing.T) {
	t.Helper()
	if err := h.service.DeliverDue(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// pending lists notifications due at the given offset from the clock.
func (h *harness) pending(t *testing.T, after time.Duration) []pushservice.Notification {
	t.Helper()
	due, err := h.store.Push().DueNotifications(t.Context(), h.clock.Add(after).UnixMilli(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return due
}

func TestDeliveryWaitsForPublicationThenSendsOnce(t *testing.T) {
	h := newHarness(t)
	endpoint := newEndpoint(t, status(201))
	device := h.subscribe(t, endpoint)
	h.notify(t)
	created := h.clock.UnixMilli()
	h.deliver(t)
	if endpoint.count() != 0 {
		t.Fatal("sent before the follower's snapshot was published")
	}
	if len(h.pending(t, 0)) != 0 || len(h.pending(t, 3*time.Second)) != 1 {
		t.Fatal("unpublished notification not rescheduled shortly")
	}
	stream := h.publish(t)
	h.advance(3 * time.Second)
	h.deliver(t)
	h.advance(time.Hour)
	h.deliver(t)
	if endpoint.count() != 1 {
		t.Fatalf("sent %d times", endpoint.count())
	}
	var payload map[string]any
	if err := json.Unmarshal(device.Decrypt(t, endpoint.bodies[0]), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"kind": "pull_request", "title": "octo/app #1", "body": "Add search\nNew PR by @mona", "url": "/streams/" + stream, "tag": stream, "timestamp": float64(created)}
	if len(payload) != len(want) {
		t.Fatalf("payload %v", payload)
	}
	for key, value := range want {
		if payload[key] != value {
			t.Fatalf("payload %s = %v, want %v", key, payload[key], value)
		}
	}
	header := endpoint.headers[0]
	if header.Get("Topic") != pushservice.Topic(stream) || len(header.Get("Topic")) != 32 || header.Get("TTL") != "86400" || header.Get("Urgency") != "normal" {
		t.Fatalf("headers %v", header)
	}
	devices, err := h.store.Push().Subscriptions(t.Context(), h.owner.ID)
	if err != nil || len(devices) != 1 || devices[0].LastSuccessAt == nil {
		t.Fatalf("last success not recorded: %+v %v", devices, err)
	}
}

func TestUndeliverableNotificationsAreDropped(t *testing.T) {
	for name, prepare := range map[string]func(*testing.T, *harness){
		"failed admission": func(t *testing.T, h *harness) {
			delivery, err := h.store.Queue().Claim(t.Context(), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.store.Queue().Fail(t.Context(), delivery, "rejected"); err != nil {
				t.Fatal(err)
			}
		},
		"expired": func(t *testing.T, h *harness) {
			h.publish(t)
			h.advance(25 * time.Hour)
		},
		"disabled account": func(t *testing.T, h *harness) {
			h.publish(t)
			if err := identityservice.New(h.store).DisableAccount(t.Context(), h.owner.ID); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			endpoint := newEndpoint(t, status(201))
			h.subscribe(t, endpoint)
			h.notify(t)
			prepare(t, h)
			h.deliver(t)
			if endpoint.count() != 0 || len(h.pending(t, 48*time.Hour)) != 0 {
				t.Fatalf("sent %d, pending %d", endpoint.count(), len(h.pending(t, 48*time.Hour)))
			}
		})
	}
}

func TestGoneDeviceIsRemovedAndOthersStillReceive(t *testing.T) {
	h := newHarness(t)
	gone, kept := newEndpoint(t, status(410)), newEndpoint(t, status(201))
	h.subscribe(t, gone)
	h.subscribe(t, kept)
	h.notify(t)
	h.publish(t)
	h.deliver(t)
	devices, err := h.store.Push().Subscriptions(t.Context(), h.owner.ID)
	if err != nil || len(devices) != 1 || devices[0].Endpoint != kept.server.URL {
		t.Fatalf("devices %+v %v", devices, err)
	}
	if gone.count() != 1 || kept.count() != 1 || len(h.pending(t, 48*time.Hour)) != 0 {
		t.Fatalf("gone %d kept %d", gone.count(), kept.count())
	}
}

func TestThrottledDeliveryHonorsRetryAfter(t *testing.T) {
	h := newHarness(t)
	endpoint := newEndpoint(t, func(call int, w http.ResponseWriter, _ *http.Request) {
		if call == 0 {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(201)
	})
	h.subscribe(t, endpoint)
	h.notify(t)
	h.publish(t)
	h.deliver(t)
	if due := h.pending(t, 9*time.Minute); len(due) != 0 {
		t.Fatalf("retried before Retry-After: %+v", due)
	}
	due := h.pending(t, 10*time.Minute)
	if len(due) != 1 || due[0].Attempt != 1 {
		t.Fatalf("throttled notification not rescheduled: %+v", due)
	}
	h.advance(10 * time.Minute)
	h.deliver(t)
	if endpoint.count() != 2 || len(h.pending(t, 48*time.Hour)) != 0 {
		t.Fatalf("calls %d", endpoint.count())
	}
}

func TestServerFailuresBackOffExponentially(t *testing.T) {
	h := newHarness(t)
	endpoint := newEndpoint(t, status(503))
	h.subscribe(t, endpoint)
	h.notify(t)
	h.publish(t)
	for _, delay := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		h.deliver(t)
		if len(h.pending(t, delay-time.Second)) != 0 || len(h.pending(t, delay)) != 1 {
			t.Fatalf("retry after a server failure not delayed %s", delay)
		}
		h.advance(delay)
	}
	if endpoint.count() != 9 {
		t.Fatalf("attempts %d", endpoint.count())
	}
}

func TestRestartNeverResendsToAcceptedDevice(t *testing.T) {
	h := newHarness(t)
	accepted := newEndpoint(t, status(201))
	ctx, crash := context.WithCancel(t.Context())
	interrupted := newEndpoint(t, func(call int, w http.ResponseWriter, r *http.Request) {
		if call == 0 {
			crash()
			<-r.Context().Done()
			return
		}
		w.WriteHeader(201)
	})
	h.subscribe(t, accepted)
	h.subscribe(t, interrupted)
	h.notify(t)
	h.publish(t)
	if err := h.service.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.pending(t, 0)) != 1 {
		t.Fatal("interrupted delivery not left pending")
	}
	h.service = h.restart(t)
	h.deliver(t)
	if accepted.count() != 1 || interrupted.count() != 2 || len(h.pending(t, 48*time.Hour)) != 0 {
		t.Fatalf("accepted %d interrupted %d", accepted.count(), interrupted.count())
	}
}

func TestRunJoinsOnShutdown(t *testing.T) {
	h := newHarness(t)
	sending := make(chan struct{})
	endpoint := newEndpoint(t, func(_ int, _ http.ResponseWriter, r *http.Request) {
		close(sending)
		<-r.Context().Done()
	})
	h.subscribe(t, endpoint)
	h.notify(t)
	h.publish(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- h.service.Run(ctx) }()
	select {
	case <-sending:
	case <-time.After(10 * time.Second):
		t.Fatal("worker never sent")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if len(h.pending(t, 0)) != 1 {
		t.Fatal("shutdown abandoned an uncertain delivery")
	}
}
