package remoteserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

// fakePushService answers with the next queued status, then 201.
type fakePushService struct {
	server   *httptest.Server
	mu       sync.Mutex
	statuses []int
	bodies   [][]byte
	received chan []byte
}

func newFakePushService(t *testing.T) *fakePushService {
	t.Helper()
	service := &fakePushService{received: make(chan []byte, 16)}
	service.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliveries are POSTs; local tools that probe new ports are not.
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		service.mu.Lock()
		status := http.StatusCreated
		if len(service.statuses) > 0 {
			status, service.statuses = service.statuses[0], service.statuses[1:]
		}
		service.bodies = append(service.bodies, body)
		service.mu.Unlock()
		if r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") {
			status = http.StatusBadRequest
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		w.WriteHeader(status)
		service.received <- body
	}))
	t.Cleanup(service.server.Close)
	return service
}

func (service *fakePushService) answer(statuses ...int) {
	service.mu.Lock()
	service.statuses = append(service.statuses, statuses...)
	service.mu.Unlock()
}

func subscriptionBody(endpoint string, device testsupport.PushDevice, label string) string {
	raw, _ := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": device.P256dh(), "auth": device.AuthSecret()}, "label": label})
	return string(raw)
}

type pushSettingsJSON struct {
	Enabled   bool             `json:"enabled"`
	PublicKey *string          `json:"publicKey"`
	Devices   []pushDeviceJSON `json:"devices"`
}

func pushSettings(t *testing.T, handler http.Handler, cookie *http.Cookie) pushSettingsJSON {
	t.Helper()
	w := identityRequest(handler, "GET", "/api/v2/push", "", cookie, "", "")
	var settings pushSettingsJSON
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &settings) != nil || settings.Devices == nil {
		t.Fatalf("push settings: %d %s", w.Code, w.Body.String())
	}
	return settings
}

func TestPushRoutesNeedBrowserSessionsAndReportDisabledPush(t *testing.T) {
	store, handler := identityHandler(t, Settings{PublicURL: "http://diffx.example"})
	token := provisionToken(t, store, "automation", identityservice.PurposeAPI)
	cookie, csrf := memberSession(t, store, "user")
	device := testsupport.NewPushDevice(t)
	body := subscriptionBody("https://push.example/a", device, "Phone")
	if w := identityRequest(handler, "GET", "/api/v2/push", "", nil, "", ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := identityRequest(handler, "GET", "/api/v2/push", "", nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token reached push settings: %d", w.Code)
	}
	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", body, nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("API token registered a device: %d", w.Code)
	}
	w := identityRequest(handler, "GET", "/api/v2/push", "", cookie, "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"enabled":false,"devices":[]}` {
		t.Fatalf("disabled settings: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", body, cookie, "", ""); w.Code != 403 {
		t.Fatalf("subscribe without CSRF: %d", w.Code)
	}
	for _, request := range []struct{ method, path, body string }{{"PUT", "/api/v2/push/subscriptions", body}, {"POST", "/api/v2/push/subscriptions/any/test", ""}} {
		w := identityRequest(handler, request.method, request.path, request.body, cookie, csrf, "")
		if w.Code != 409 || !strings.Contains(w.Body.String(), "Push notifications need an HTTPS public URL") || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s %s while disabled: %d %s", request.method, request.path, w.Code, w.Body.String())
		}
	}
	if w := identityRequest(handler, "DELETE", "/api/v2/push/subscriptions/any", "", cookie, csrf, ""); w.Code != 404 {
		t.Fatalf("delete unknown device: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/push", "", cookie, csrf, ""); w.Code != 405 {
		t.Fatalf("POST settings: %d", w.Code)
	}
}

func TestPushDevicesAreOwnerScopedTestedAndRebound(t *testing.T) {
	store, handler := identityHandler(t, Settings{PublicURL: "http://localhost:7980"})
	alice, aliceCSRF := memberSession(t, store, "alice")
	bob, bobCSRF := memberSession(t, store, "bob")
	push := newFakePushService(t)
	device := testsupport.NewPushDevice(t)
	body := subscriptionBody(push.server.URL+"/send/a", device, "Chrome on Android")

	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", subscriptionBody(push.server.URL, device, strings.Repeat("x", 121)), alice, aliceCSRF, ""); w.Code != 400 {
		t.Fatalf("long label: %d", w.Code)
	}
	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", subscriptionBody("http://push.example/a", device, ""), alice, aliceCSRF, ""); w.Code != 400 {
		t.Fatalf("plain HTTP endpoint: %d", w.Code)
	}
	w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", body, alice, aliceCSRF, "")
	var registered pushDeviceJSON
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &registered) != nil || registered.ID == "" || registered.Label != "Chrome on Android" || registered.LastSuccessAt != nil || !strings.Contains(w.Body.String(), `"lastSuccessAt":null`) {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body.String())
	}
	settings := pushSettings(t, handler, alice)
	if !settings.Enabled || settings.PublicKey == nil || len(*settings.PublicKey) != 87 || len(settings.Devices) != 1 || settings.Devices[0].ID != registered.ID {
		t.Fatalf("settings %+v", settings)
	}
	device1 := "/api/v2/push/subscriptions/" + registered.ID
	if w := identityRequest(handler, "DELETE", device1, "", bob, bobCSRF, ""); w.Code != 404 {
		t.Fatalf("bob deleted alice's device: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", device1+"/test", "", bob, bobCSRF, ""); w.Code != 404 || len(push.received) != 0 {
		t.Fatalf("bob tested alice's device: %d", w.Code)
	}

	if w := identityRequest(handler, "POST", device1+"/test", "", alice, aliceCSRF, ""); w.Code != 204 {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	var message map[string]any
	if err := json.Unmarshal(device.Decrypt(t, <-push.received), &message); err != nil {
		t.Fatal(err)
	}
	if message["kind"] != "test" || message["title"] != "diffx" || message["body"] != "Notifications are on for this device." || message["url"] != "/settings/notifications" || message["tag"] != "diffx-test" || message["timestamp"] == nil {
		t.Fatalf("test payload %v", message)
	}
	if settings := pushSettings(t, handler, alice); settings.Devices[0].LastSuccessAt == nil {
		t.Fatal("accepted test not recorded")
	}
	for status, want := range map[int]int{429: 429, 500: 502, 400: 502} {
		push.answer(status)
		w := identityRequest(handler, "POST", device1+"/test", "", alice, aliceCSRF, "")
		<-push.received
		if w.Code != want || (want == 429 && w.Header().Get("Retry-After") != "30") {
			t.Fatalf("push service %d: %d %s", status, w.Code, w.Body.String())
		}
	}
	push.answer(410)
	if w := identityRequest(handler, "POST", device1+"/test", "", alice, aliceCSRF, ""); w.Code != 404 {
		t.Fatalf("gone device: %d", w.Code)
	}
	<-push.received
	if settings := pushSettings(t, handler, alice); len(settings.Devices) != 0 {
		t.Fatalf("gone device kept: %+v", settings)
	}

	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", body, alice, aliceCSRF, ""); w.Code != 200 {
		t.Fatalf("resubscribe: %d", w.Code)
	}
	w = identityRequest(handler, "PUT", "/api/v2/push/subscriptions", body, bob, bobCSRF, "")
	var moved pushDeviceJSON
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &moved) != nil {
		t.Fatalf("rebind: %d %s", w.Code, w.Body.String())
	}
	if settings := pushSettings(t, handler, alice); len(settings.Devices) != 0 {
		t.Fatalf("endpoint kept by its previous account: %+v", settings)
	}
	if settings := pushSettings(t, handler, bob); len(settings.Devices) != 1 || settings.Devices[0].ID != moved.ID {
		t.Fatalf("endpoint not moved: %+v", settings)
	}
	if w := identityRequest(handler, "DELETE", "/api/v2/push/subscriptions/"+moved.ID, "", bob, bobCSRF, ""); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func connectGitHub(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf string) {
	t.Helper()
	w := identityRequest(handler, "POST", "/api/v2/github/connection", "", cookie, csrf, "")
	var started struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &started) != nil {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	authorize, err := url.Parse(started.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v2/github/callback?"+url.Values{"code": {"code"}, "state": {authorize.Query().Get("state")}}.Encode(), nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Header().Get("Location") != "/settings/github?github=connected" {
		t.Fatalf("callback: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestFollowNotifySettingIsReflectedInListing(t *testing.T) {
	store, handler := identityHandler(t, Settings{GitHub: githubSettings(t, fakeGitHubAPI(t).URL)})
	token := provisionToken(t, store, "automation", identityservice.PurposeAPI)
	cookie, csrf := memberSession(t, store, "user")
	connectGitHub(t, handler, cookie, csrf)
	if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", `{"notify":false}`, cookie, csrf, ""); w.Code != 404 {
		t.Fatalf("muted an unfollowed repository: %d %s", w.Code, w.Body.String())
	}
	if w := identityRequest(handler, "PUT", "/api/v2/github/follows/10", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("follow: %d", w.Code)
	}
	listing := func() string {
		w := identityRequest(handler, "GET", "/api/v2/github/repositories", "", cookie, "", "")
		if w.Code != 200 {
			t.Fatalf("repositories: %d", w.Code)
		}
		return w.Body.String()
	}
	if !strings.Contains(listing(), `"followed":true,"lost":false,"notify":true`) {
		t.Fatalf("followed repository must notify by default: %s", listing())
	}
	if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", `{"notify":false}`, cookie, "", ""); w.Code != 403 {
		t.Fatalf("PATCH without CSRF: %d", w.Code)
	}
	if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", `{"notify":false}`, nil, "", "Bearer "+token); w.Code != 403 {
		t.Fatalf("PATCH with an API token: %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"notify":"no"}`, `{"notify":false,"extra":1}`} {
		if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", body, cookie, csrf, ""); w.Code != 400 {
			t.Fatalf("PATCH %s: %d", body, w.Code)
		}
	}
	if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", `{"notify":false}`, cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("mute: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(listing(), `"followed":true,"lost":false,"notify":false`) {
		t.Fatalf("mute not listed: %s", listing())
	}
	if w := identityRequest(handler, "PATCH", "/api/v2/github/follows/10", `{"notify":true}`, cookie, csrf, ""); w.Code != 204 || !strings.Contains(listing(), `"notify":true`) {
		t.Fatalf("unmute: %d %s", w.Code, listing())
	}
	if w := identityRequest(handler, "DELETE", "/api/v2/github/follows/10", "", cookie, csrf, ""); w.Code != 204 || !strings.Contains(listing(), `"followed":false,"lost":false,"notify":false`) {
		t.Fatalf("unfollowed repository notifies: %s", listing())
	}
}

func TestServiceWorkerAndManifestServedWithBrowserTypes(t *testing.T) {
	store := pgtest.Store(t)
	handler, closeServices, err := Handler(t.Context(), store, Settings{}, fstest.MapFS{
		"index.html": {Data: []byte("app")}, "sw.js": {Data: []byte("self")}, "manifest.webmanifest": {Data: []byte("{}")}, "assets/app.js": {Data: []byte("app()")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeServices() })
	for path, want := range map[string][2]string{
		"/sw.js":                {"text/javascript; charset=utf-8", "no-cache"},
		"/assets/app.js":        {"text/javascript; charset=utf-8", "no-store"},
		"/manifest.webmanifest": {"application/manifest+json", "no-store"},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != want[0] || w.Header().Get("Cache-Control") != want[1] {
			t.Fatalf("%s: %d %q %q", path, w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
		}
	}
}

// fetchedSnapshot is what diffx-fetcher returns for a leased pull request.
func fetchedSnapshot(job fetchprotocol.Job) ingestion.Request {
	head := job.HeadSHA
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: "fetcher",
		Metadata: ingestion.Metadata{SourceID: "fetcher", Hostname: "fetcher", Trigger: "manual", RepositoryKey: "mirror", CheckoutKey: "mirror",
			Root: "/mirrors/10", Branch: "pr", Head: &head, CollectedAt: 1, CollectorVersion: "test",
			Comparison: &ingestion.Comparison{Kind: "branch", BaseRef: "refs/remotes/origin/main", BaseCommit: job.BaseSHA, MergeBase: job.BaseSHA}},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{Source: "local", Root: "/mirrors/10", Name: "10", Branch: "pr", Head: &head, Mode: review.DiffAll,
				ComparisonKind: review.ComparisonCommitted, Revision: "revision", Files: []review.ChangedFile{{ID: "f1", Path: "search.go", Status: "M", Fingerprint: "fp"}}},
			Patches: map[string]review.FilePatch{"f1": {Patch: "@@ -1 +1 @@\n-a\n+b\n"}},
		}},
	}
}

func TestOpenedPullRequestReachesSubscribedDevice(t *testing.T) {
	store := pgtest.Store(t)
	github := githubSettings(t, fakeGitHubAPI(t).URL)
	github.FetcherSecret = strings.Repeat("s", 32)
	ctx, cancel := context.WithCancel(t.Context())
	handler, internal, closeServices, tasks, err := compose(ctx, store, fstest.MapFS{"index.html": {Data: []byte("app")}}, Settings{PublicURL: "http://localhost:7980", GitHub: github})
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
		closeServices()
	})

	cookie, csrf := memberSession(t, store, "alice")
	connectGitHub(t, handler, cookie, csrf)
	if w := identityRequest(handler, "PUT", "/api/v2/github/follows/10", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("follow: %d", w.Code)
	}
	push := newFakePushService(t)
	device := testsupport.NewPushDevice(t)
	if w := identityRequest(handler, "PUT", "/api/v2/push/subscriptions", subscriptionBody(push.server.URL, device, "Phone"), cookie, csrf, ""); w.Code != 200 {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body.String())
	}

	body := `{"action":"opened","number":1,"installation":{"id":5},"repository":{"id":10,"full_name":"octo/app"}}`
	if code := webhook(handler, "opened-1", signed(body), body); code != 202 {
		t.Fatalf("webhook: %d", code)
	}
	fetcher := func(path string, payload any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+github.FetcherSecret)
		w := httptest.NewRecorder()
		internal.ServeHTTP(w, r)
		return w
	}
	w := fetcher(fetchprotocol.LeasePath, nil)
	var job fetchprotocol.Job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Number != 1 {
		t.Fatalf("lease: %d %s", w.Code, w.Body.String())
	}
	snapshot := fetchedSnapshot(job)
	if w := fetcher(fetchprotocol.ResultPath, fetchprotocol.Result{ID: job.ID, LeaseToken: job.LeaseToken, Request: &snapshot}); w.Code != 204 {
		t.Fatalf("result: %d %s", w.Code, w.Body.String())
	}

	var received []byte
	select {
	case received = <-push.received:
	case <-time.After(20 * time.Second):
		t.Fatal("pull request notification never reached the push service")
	}
	var message map[string]any
	if err := json.Unmarshal(device.Decrypt(t, received), &message); err != nil {
		t.Fatal(err)
	}
	stream, _ := message["tag"].(string)
	if message["kind"] != "pull_request" || message["title"] != "octo/app #1" || message["body"] != "Add search\nNew PR by @mona" || stream == "" || message["url"] != "/streams/"+stream {
		t.Fatalf("payload %v", message)
	}
	contexts := identityRequest(handler, "GET", "/api/v2/contexts", "", cookie, "", "")
	if contexts.Code != 200 || !strings.Contains(contexts.Body.String(), `"streamId":"`+stream+`"`) {
		t.Fatalf("notification links a stream the account cannot open: %s", contexts.Body.String())
	}
	select {
	case extra := <-push.received:
		t.Fatalf("notification sent twice: %d bytes", len(extra))
	case <-time.After(2500 * time.Millisecond):
	}
}
