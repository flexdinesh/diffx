package remoteserver

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// Two replicas share one database: a context ingested through one reaches
// event streams on the other, and disabling an account through one stops its
// services on the other.
func TestReplicasShareEventsAndAccountRemoval(t *testing.T) {
	url := pgtest.URL(t)
	open := func() *pgstore.Store {
		store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	storeA, storeB := open(), open()
	assets := fstest.MapFS{"index.html": {Data: []byte("reviews")}}
	handlerA, closeA, err := MultiHandler(t.Context(), storeA, assets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeA() })
	ctx, cancel := context.WithCancel(t.Context())
	publicB, _, closeB, tasks, err := compose(ctx, storeB, assets, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, len(tasks))
	for _, task := range tasks {
		go func() { done <- task(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		for range tasks {
			<-done
		}
		_ = closeB()
	})
	replicaB, ok := publicB.(*transport)
	if !ok {
		t.Fatal("remote composition is not the transport")
	}
	serverA, serverB := httptest.NewServer(handlerA), httptest.NewServer(publicB)
	t.Cleanup(serverA.Close)
	t.Cleanup(serverB.Close)

	identity := identityservice.New(storeA)
	account := testsupport.RemoteAccount(t, storeA, "member")
	token, err := identity.CreateToken(t.Context(), account.ID, "test", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequestWithContext(t.Context(), "GET", serverB.URL+"/api/v2/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token.Secret)
	events, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	reader := bufio.NewReader(events.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("event connection: %d %q %v", events.StatusCode, line, err)
	}

	input, err := collector.CollectPatch(t.Context(), "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n", t.TempDir(), collector.Options{SourceID: "source", SubmissionID: "submission"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := testsupport.NewClient(serverA.URL, token.Secret).Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("replica B event stream: %v", err)
		}
		if strings.HasPrefix(line, "data:") {
			if !strings.Contains(line, receipt.ContextID) {
				t.Fatalf("replica B event = %q, want %s", line, receipt.ContextID)
			}
			break
		}
	}

	app, err := replicaB.applications.get(account)
	if err != nil {
		t.Fatal(err)
	}
	subscription, unsubscribe := app.service.Subscribe()
	defer unsubscribe()
	if err := identity.DisableAccount(t.Context(), "admin", account.ID); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(10 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-subscription:
		case <-timeout:
			t.Fatal("replica B kept the disabled account's services")
		}
	}
	replicaB.applications.mu.Lock()
	_, cached := replicaB.applications.applications[account.ID]
	replicaB.applications.mu.Unlock()
	if cached {
		t.Fatal("replica B still caches the disabled account")
	}
}

// An admin's settings change reaches every replica's bootstrap through the
// change feed, without a request touching the database.
func TestReplicasReloadChangedSettings(t *testing.T) {
	store := pgtest.Store(t)
	handler, closeServices, err := Handler(t.Context(), store, Settings{}, fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeServices() })
	bootstrap := func() string {
		return identityRequest(handler, "GET", "/api/v2/bootstrap", "", nil, "", "").Body.String()
	}
	if body := bootstrap(); !strings.Contains(body, `"signupEnabled":true,"passwordSignupEnabled":true`) {
		t.Fatalf("default bootstrap: %s", body)
	}
	// Another process, the console, changes the stored settings.
	if err := identityservice.New(store).PutSettings(t.Context(), "", identityservice.Settings{PasswordSignup: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for body := bootstrap(); !strings.Contains(body, `"signupEnabled":false,"passwordSignupEnabled":true`); body = bootstrap() {
		if time.Now().After(deadline) {
			t.Fatalf("settings not reloaded: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// resyncFeed delivers one resync, as a reconnected listener does.
type resyncFeed struct{}

func (resyncFeed) Run(ctx context.Context, deliver func(pgstore.Notification)) error {
	deliver(pgstore.Notification{Kind: pgstore.Resync})
	<-ctx.Done()
	return nil
}

// Changes committed while the listener was away arrive by the resync reload.
func TestResyncReloadsMissedSettings(t *testing.T) {
	store := pgtest.Store(t)
	identity := testsupport.Identity(t, store)
	testsupport.PutSettings(t, store, identityservice.Settings{})
	ctx, cancel := context.WithCancel(t.Context())
	applications := &registry{ctx: ctx, store: store, applications: make(map[string]application)}
	wake, reload := make(chan struct{}, 1), make(chan struct{}, 1)
	done := make(chan error, 2)
	for _, task := range []serverapp.Task{replicate(resyncFeed{}, applications, wake, reload), reloadSettings(identity, reload)} {
		go func() { done <- task(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		<-done
		<-done
	})
	deadline := time.Now().Add(10 * time.Second)
	for identity.Settings() != (identityservice.Settings{}) {
		if time.Now().After(deadline) {
			t.Fatalf("missed settings not reloaded: %+v", identity.Settings())
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-wake:
	default:
		t.Fatal("resync did not wake the worker")
	}
}
