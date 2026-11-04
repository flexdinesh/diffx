package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
	"github.com/oapi-codegen/nullable"
)

type fixture struct {
	mu              sync.Mutex
	heads           []goclient.Context
	collect         bool
	collectionCalls []string
	requests        []string
	events          chan string
	hook            func(http.ResponseWriter, *http.Request) bool
}

func capabilities() goclient.SessionCapabilities {
	var caps goclient.SessionCapabilities
	caps.Diff.Scopes.State = "enabled"
	caps.Diff.Scopes.Values = []goclient.DiffMode{"all", "staged", "unstaged", "uncommitted", "committed"}
	caps.Diff.StagingMetadata.State = "enabled"
	caps.Files.Contents.State = "enabled"
	caps.Review.Comments.State = "enabled"
	return caps
}
func reviewContext(id string) goclient.Context {
	return goclient.Context{Id: id, Name: "Repository", Availability: "available", StreamId: nullable.NewNullableWithValue("stream"), RepositoryId: nullable.NewNullableWithValue("repository"), LatestContextId: nullable.NewNullableWithValue(id), ChangedFileCount: nullable.NewNullableWithValue(2), Capabilities: capabilities(), LastChangedAt: 1, Commits: &goclient.CommitRange{Count: 1}, ScopeSummaries: &[]goclient.ScopeSummary{{Scope: "all", ComparisonKind: "combined", FileCount: 2}, {Scope: "uncommitted", ComparisonKind: "uncommitted", FileCount: 1}, {Scope: "staged", ComparisonKind: "staged", FileCount: 1}, {Scope: "committed", ComparisonKind: "committed", FileCount: 2}}}
}
func manifest(id, mode string) goclient.RepositoryDiff {
	files := make([]goclient.ChangedFile, 40)
	for i := range files {
		files[i] = goclient.ChangedFile{Id: fmt.Sprintf("file-%d", i), Path: fmt.Sprintf("file-%d.go", i), Fingerprint: fmt.Sprintf("fingerprint-%d", i)}
	}
	return goclient.RepositoryDiff{Id: "diff-" + id, VersionId: "version-" + id, Mode: goclient.DiffViewMode(mode), Files: files, Revision: "revision-" + id}
}
func jsonResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	hook := f.hook
	f.mu.Unlock()
	if hook != nil && hook(w, r) {
		return
	}
	switch r.URL.Path {
	case "/api/v2/bootstrap":
		jsonResponse(w, 200, map[string]any{"mode": "local", "input": "checkout", "collect": f.collect, "user": nil, "csrfToken": nil, "googleEnabled": false})
		return
	case "/api/v2/events":
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-f.events:
				io.WriteString(w, "event: ingestion\ndata: "+event+"\n\n")
				w.(http.Flusher).Flush()
			}
		}
	case "/api/v2/contexts":
		f.mu.Lock()
		heads := detached(f.heads)
		f.mu.Unlock()
		jsonResponse(w, 200, goclient.ContextPage{Contexts: heads})
		return
	case "/api/v2/collections":
		f.mu.Lock()
		f.collectionCalls = append(f.collectionCalls, r.URL.Query().Get("initial"))
		heads := detached(f.heads)
		f.mu.Unlock()
		id := ""
		if len(heads) > 0 {
			id = heads[0].Id
		}
		jsonResponse(w, 200, goclient.CollectionResult{Contexts: heads, DefaultContextId: id})
		return
	case "/api/v2/metrics":
		jsonResponse(w, 200, goclient.ServerMetrics{CpuUsage: 1})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "contexts" {
		http.NotFound(w, r)
		return
	}
	id := parts[3]
	if len(parts) == 4 {
		jsonResponse(w, 200, reviewContext(id))
		return
	}
	switch parts[4] {
	case "session":
		jsonResponse(w, 200, goclient.Session{Id: id, Capabilities: capabilities()})
	case "commits":
		if len(parts) == 5 {
			jsonResponse(w, 200, goclient.CommitList{Commits: []goclient.CommitSummary{{Oid: "oid", Subject: "captured"}}})
			return
		}
		if len(parts) == 7 && parts[6] == "diff" {
			jsonResponse(w, 200, manifest(id, "commit"))
			return
		}
		if parts[len(parts)-1] == "patch" {
			jsonResponse(w, 200, goclient.FilePatch{Patch: "patch-" + id})
			return
		}
		jsonResponse(w, 200, goclient.FileContents{Before: "before-" + id, After: "after-" + id})
	case "diff":
		jsonResponse(w, 200, manifest(id, r.URL.Query().Get("scope")))
	case "diffs":
		if len(parts) == 6 && parts[5] == "current" {
			jsonResponse(w, 200, manifest(id, r.URL.Query().Get("scope")))
			return
		}
		if parts[len(parts)-1] == "patch" {
			jsonResponse(w, 200, goclient.FilePatch{Patch: "patch-" + id})
			return
		}
		jsonResponse(w, 200, goclient.FileContents{Before: "before-" + id, After: "after-" + id})
	case "comments":
		if len(parts) == 6 && parts[5] == "export" {
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, "<review server=\"authoritative\"/>")
			return
		}
		if r.Method == "GET" || r.Method == "DELETE" && len(parts) == 5 {
			jsonResponse(w, 200, map[string]any{"comments": []goclient.ReviewComment{{Id: "comment", Body: "server body", Scope: "uncommitted", Placement: &goclient.CommentPlacement{Applicability: "moved", Start: 9, End: 10}}}, "deleted": 1})
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		status := 200
		if r.Method == "POST" && len(parts) == 5 {
			status = 201
		}
		jsonResponse(w, status, goclient.ReviewComment{Id: "comment", Body: "saved"})
	case "review-marks":
		if r.Method == "GET" {
			jsonResponse(w, 200, map[string]any{"marks": []goclient.ReviewMark{}})
			return
		}
		if r.Method == "PUT" {
			jsonResponse(w, 200, goclient.ReviewMark{})
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
func newFixture(t *testing.T, collect bool, hook func(http.ResponseWriter, *http.Request) bool) (*Controller, *fixture, chan struct{}) {
	t.Helper()
	f := &fixture{heads: []goclient.Context{reviewContext("one")}, collect: collect, events: make(chan string, 8), hook: hook}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	client, err := goclient.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	notify := make(chan struct{}, 1)
	c := New(client, func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	})
	c.Start(context.Background(), localprotocol.Input{Kind: "checkout", Selection: "checkout", Paths: []string{"/repo"}})
	t.Cleanup(c.Close)
	return c, f, notify
}
func waitState(t *testing.T, c *Controller, notify <-chan struct{}, condition func(State) bool) State {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		s := c.Snapshot()
		if condition(s) {
			return s
		}
		select {
		case <-notify:
		case <-timer.C:
			t.Fatalf("state condition timed out: %#v", s)
		}
	}
}
func loaded(s State) bool {
	return s.Diff != nil && !s.Loading && len(s.Comments) > 0 && len(s.Commits) > 0
}
func waitResult(t *testing.T, c *Controller, n <-chan struct{}, id uint64) MutationResult {
	t.Helper()
	return waitState(t, c, n, func(s State) bool { return s.Results[id].Done }).Results[id]
}

func TestCollectionJoinOnceSyncAndImmutableNewer(t *testing.T) {
	c, f, n := newFixture(t, true, nil)
	first := waitState(t, c, n, loaded)
	if first.Scope != "uncommitted" {
		t.Fatalf("default scope %s", first.Scope)
	}
	f.mu.Lock()
	f.heads = []goclient.Context{reviewContext("two")}
	f.mu.Unlock()
	c.Sync()
	next := waitState(t, c, n, func(s State) bool { return s.NewerContextID == "two" && !s.Collecting })
	if next.Current.Id != "one" || next.Diff.Id != "diff-one" {
		t.Fatal("sync swapped selected observation")
	}
	f.mu.Lock()
	calls := append([]string{}, f.collectionCalls...)
	f.mu.Unlock()
	if len(calls) != 2 || calls[0] != "true" || calls[1] != "" {
		t.Fatalf("initial collection ownership: %v", calls)
	}
	if next.Seen["stream"] != "one" {
		t.Fatal("arrival incorrectly marked seen")
	}
	c.OpenLatest("stream")
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Current.Id == "two" && s.Seen["stream"] == "two" })
	if first.Current.Id != "one" || first.Contexts[0].Id != "one" {
		t.Fatal("published snapshot mutated")
	}
}

func TestGenerationCancelsOldContextAndPreviews(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/one/diffs/") && strings.HasSuffix(r.URL.Path, "/patch") {
			once.Do(func() { close(started) })
			<-r.Context().Done()
			close(cancelled)
			return true
		}
		return false
	}
	c, _, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	c.LoadPatch("file-0")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("patch did not start")
	}
	c.OpenContext("two")
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Current.Id == "two" })
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("owner change did not cancel preview")
	}
	if len(c.Snapshot().Patches) != 0 {
		t.Fatal("old patch settled in new context")
	}
	c.LoadPatch("file-0")
	c.LoadContents("file-0")
	s := waitState(t, c, n, func(s State) bool { return s.Patches["file-0"].Patch != "" && s.Contents["file-0"].After != "" })
	if s.Patches["file-0"].Patch != "patch-two" || s.Contents["file-0"].After != "after-two" {
		t.Fatal("preview ownership mismatch")
	}
}

func TestMutationResultSurvivesNavigationAndPreservesDraftOwner(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "POST" && r.URL.Path == "/api/v2/contexts/one/comments" {
			var body goclient.CreateComment
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.DiffId != "diff-one" || body.VersionId != "version-one" || body.FileVersion != "fingerprint-0" {
				t.Error("mutation identities changed")
			}
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(409)
			io.WriteString(w, `{"detail":"draft must be retried","status":409}`)
			return true
		}
		return false
	}
	c, _, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	body := goclient.CreateComment{Body: "draft", DiffId: "diff-one", VersionId: "version-one", FileId: "file-0", FileVersion: "fingerprint-0", Scope: "uncommitted", Start: 1, End: 1, Side: "additions"}
	id := c.CreateComment(body)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("save did not start")
	}
	c.OpenContext("two")
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Current.Id == "two" })
	close(release)
	result := waitResult(t, c, n, id)
	if result.ContextID != "one" || result.Error != "draft must be retried" {
		t.Fatalf("lost draft save result: %#v", result)
	}
	if c.Snapshot().Current.Id != "two" {
		t.Fatal("save restored old selection")
	}
	c.ForgetResult(id)
	if _, ok := c.Snapshot().Results[id]; ok {
		t.Fatal("result acknowledgement failed")
	}
	bad := c.CreateComment(body)
	if result := waitResult(t, c, n, bad); result.Error == "" {
		t.Fatal("silently retargeted old draft")
	}
}

func TestAllReviewOperationsCommitReadOnlyAndCanonicalExport(t *testing.T) {
	c, _, n := newFixture(t, false, nil)
	waitState(t, c, n, loaded)
	text := "edit"
	status := goclient.UpdateCommentStatus("resolved")
	operations := []uint64{c.UpdateComment("comment", goclient.UpdateComment{Body: &text, Status: &status}), c.CarryComment("comment"), c.DeleteComments("resolved"), c.DeleteComment("comment"), c.MarkReviewed("file-0", true), c.MarkReviewed("file-0", false), c.ClearReviewed()}
	for _, id := range operations {
		if result := waitResult(t, c, n, id); result.Error != "" {
			t.Fatalf("%s: %s", result.Kind, result.Error)
		}
	}
	exported := make(chan string, 1)
	c.ExportComments(func(text string, err error) {
		if err != nil {
			exported <- err.Error()
		} else {
			exported <- text
		}
	})
	select {
	case text := <-exported:
		if text != "<review server=\"authoritative\"/>" {
			t.Fatalf("export rewritten: %s", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("export did not settle")
	}
	c.OpenCommit("oid")
	waitState(t, c, n, func(s State) bool { return !s.Loading && s.Diff != nil && s.CommitID == "oid" })
	for _, id := range []uint64{c.UpdateComment("comment", goclient.UpdateComment{Body: &text}), c.MarkReviewed("file-0", true), c.ClearReviewed()} {
		if result := waitResult(t, c, n, id); result.Error != "captured commits are read only" {
			t.Fatalf("commit write accepted: %#v", result)
		}
	}
	c.LoadPatch("file-0")
	c.LoadContents("file-0")
	waitState(t, c, n, func(s State) bool { return s.Patches["file-0"].Patch != "" && s.Contents["file-0"].After != "" })
}

func TestSSEEventReloadsDurableHeadsWithoutReplacingReview(t *testing.T) {
	c, f, n := newFixture(t, false, nil)
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Connected })
	f.mu.Lock()
	f.heads = []goclient.Context{reviewContext("two")}
	f.mu.Unlock()
	f.events <- `{"contextId":""}`
	s := waitState(t, c, n, func(s State) bool { return s.NewerContextID == "two" })
	if s.Current.Id != "one" || s.Comments[0].Placement.Start != 9 {
		t.Fatal("event replaced selection or server placement")
	}
}

func TestPreviewLimitFingerprintAndCache(t *testing.T) {
	var mu sync.Mutex
	active, maximum := 0, 0
	started := make(chan struct{}, 40)
	release := make(chan struct{})
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/diffs/") || !strings.HasSuffix(r.URL.Path, "/patch") {
			return false
		}
		parts := strings.Split(r.URL.Path, "/")
		file := parts[len(parts)-2]
		index, _ := strconv.Atoi(strings.TrimPrefix(file, "file-"))
		if q := r.URL.Query(); q.Get("fileVersion") != fmt.Sprintf("fingerprint-%d", index) || q.Get("versionId") != "version-one" || q.Get("scope") != "uncommitted" {
			t.Errorf("preview identities %v", q)
		}
		mu.Lock()
		active++
		maximum = max(maximum, active)
		mu.Unlock()
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		jsonResponse(w, 200, goclient.FilePatch{Patch: file})
		mu.Lock()
		active--
		mu.Unlock()
		return true
	}
	c, _, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	for i := range 40 {
		c.LoadPatch(fmt.Sprintf("file-%d", i))
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("preview workers did not start")
		}
	}
	mu.Lock()
	if active != 4 {
		t.Errorf("active previews %d", active)
	}
	mu.Unlock()
	close(release)
	s := waitState(t, c, n, func(s State) bool { return len(s.PatchLoading) == 0 })
	mu.Lock()
	maxActive := maximum
	mu.Unlock()
	if maxActive > 4 || len(s.Patches) != maxCachedFiles {
		t.Fatalf("preview bounds active=%d cached=%d", maxActive, len(s.Patches))
	}
}

func TestSnapshotsDetachedAndPresentationRules(t *testing.T) {
	context := reviewContext("a")
	if DefaultScope(context) != "uncommitted" {
		t.Fatal("staged edits must select Uncommitted")
	}
	context.ScopeSummaries = &[]goclient.ScopeSummary{{Scope: "uncommitted", ComparisonKind: "uncommitted"}, {Scope: "committed", ComparisonKind: "committed", FileCount: 1}}
	if DefaultScope(context) != "committed" {
		t.Fatal("clean checkout must select Branch changes")
	}
	source := goclient.ContextSource("stdin")
	piped := reviewContext("stdin")
	piped.Source = &source
	stale := true
	piped.Stale = &stale
	piped.Name = "sensitive repository"
	piped.Branch = nullable.NewNullableWithValue("sensitive branch")
	if identity := DisplayIdentity(piped); identity.Name != "Piped" || identity.Branch != "" || identity.Root != "" {
		t.Fatalf("stdin identity leaked: %#v", identity)
	}
	if len(PickerResults([]goclient.Context{piped}, "", Filters{Freshness: "latest", Hosts: []string{"other"}, Branches: []string{"other"}})) != 1 {
		t.Fatal("repository filters excluded piped capture")
	}
	left, right := reviewContext("b"), reviewContext("a")
	left.LastChangedAt = 10
	right.LastChangedAt = 10
	if CompareActivity(left, right) <= 0 {
		t.Fatal("unstable identity ordering")
	}
	c, _, n := newFixture(t, false, nil)
	first := waitState(t, c, n, loaded)
	first.Seen["stream"] = "changed"
	first.Current.Capabilities.Diff.Scopes.Values[0] = "unstaged"
	first.Comments[0].Placement.Start = 999
	second := c.Snapshot()
	if second.Seen["stream"] == "changed" || second.Current.Capabilities.Diff.Scopes.Values[0] != "all" || second.Comments[0].Placement.Start != 9 {
		t.Fatal("snapshot aliases controller state")
	}
}

func TestPartialCollectionFailureRetryIsFresh(t *testing.T) {
	var mu sync.Mutex
	calls := []string{}
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v2/collections" {
			return false
		}
		mu.Lock()
		calls = append(calls, r.URL.Query().Get("initial"))
		attempt := len(calls)
		mu.Unlock()
		result := goclient.CollectionResult{Contexts: []goclient.Context{reviewContext("one")}, DefaultContextId: "one"}
		if attempt == 1 {
			result.Failures = []goclient.CollectionFailure{{Path: "/failed", Detail: "checkout unavailable"}}
		}
		jsonResponse(w, 200, result)
		return true
	}
	c, _, n := newFixture(t, true, hook)
	first := waitState(t, c, n, func(s State) bool { return loaded(s) && s.CollectionError != "" })
	if first.Current.Id != "one" || len(first.Collection.Failures) != 1 {
		t.Fatal("partial failure discarded usable review")
	}
	c.Retry()
	waitState(t, c, n, func(s State) bool { return loaded(s) && !s.Collecting && s.CollectionError == "" })
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != "true" || calls[1] != "" {
		t.Fatalf("retry must collect fresh: %v", calls)
	}
}

func TestHistoryPaginationFiltersAndMetricsAreOnDemand(t *testing.T) {
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v2/contexts" || r.URL.Query().Get("latest") == "true" {
			return false
		}
		cursor := r.URL.Query().Get("cursor")
		page := goclient.ContextPage{Contexts: []goclient.Context{reviewContext("history-one")}, NextCursor: nullable.NewNullableWithValue("next")}
		if cursor == "next" {
			page = goclient.ContextPage{Contexts: []goclient.Context{reviewContext("history-two")}}
		}
		jsonResponse(w, 200, page)
		return true
	}
	c, f, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	f.mu.Lock()
	for _, request := range f.requests {
		if strings.Contains(request, "/metrics") || strings.Contains(request, "cursor=") && !strings.Contains(request, "latest=true") {
			t.Errorf("unsolicited history/metrics request %s", request)
		}
	}
	f.mu.Unlock()
	c.Search("Repository")
	page := waitState(t, c, n, func(s State) bool { return !s.Searching && len(s.History) == 1 })
	if page.NextCursor != "next" || len(PickerResults(page.History, page.Query, page.Filters)) != 1 {
		t.Fatal("history first page lost metadata")
	}
	c.SearchNext()
	waitState(t, c, n, func(s State) bool { return !s.Searching && len(s.History) == 2 && s.NextCursor == "" })
	c.LoadMetrics()
	waitState(t, c, n, func(s State) bool { return !s.MetricsLoading && s.Metrics != nil })
}

func TestSSEReconnectReloadsMissedHeads(t *testing.T) {
	var mu sync.Mutex
	connections := 0
	disconnect := make(chan struct{})
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v2/events" {
			return false
		}
		mu.Lock()
		connections++
		current := connections
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		if current == 1 {
			select {
			case <-disconnect:
			case <-r.Context().Done():
			}
		} else {
			<-r.Context().Done()
		}
		return true
	}
	c, f, n := newFixture(t, false, hook)
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Connected })
	f.mu.Lock()
	f.heads = []goclient.Context{reviewContext("two")}
	f.mu.Unlock()
	close(disconnect)
	s := waitState(t, c, n, func(s State) bool { return s.Connected && s.NewerContextID == "two" })
	if s.Current.Id != "one" || s.Diff.Id != "diff-one" {
		t.Fatal("reconnection replaced immutable review")
	}
}

func TestPreservedDraftCanSaveItsOriginalContext(t *testing.T) {
	saved := make(chan goclient.CreateComment, 1)
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "POST" || r.URL.Path != "/api/v2/contexts/one/comments" {
			return false
		}
		var body goclient.CreateComment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		saved <- body
		jsonResponse(w, 201, goclient.ReviewComment{Id: "saved", Body: body.Body})
		return true
	}
	c, _, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	body := goclient.CreateComment{Body: "retained draft", DiffId: "diff-one", VersionId: "version-one", FileId: "file-0", FileVersion: "fingerprint-0", Scope: "uncommitted", Start: 1, End: 1, Side: "additions"}
	c.OpenContext("two")
	waitState(t, c, n, func(s State) bool { return loaded(s) && s.Current.Id == "two" })
	id := c.CreateCommentFor("one", body)
	result := waitResult(t, c, n, id)
	if result.Error != "" || result.ContextID != "one" {
		t.Fatalf("preserved draft save %#v", result)
	}
	select {
	case got := <-saved:
		if got.Body != body.Body || got.DiffId != body.DiffId {
			t.Fatal("saved draft changed")
		}
	default:
		t.Fatal("original context not called")
	}
}

func TestMarksRemainAvailableWhenCommentsDisabled(t *testing.T) {
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/session") {
			caps := capabilities()
			caps.Review.Comments.State = "disabled"
			jsonResponse(w, 200, goclient.Session{Id: "one", Capabilities: caps})
			return true
		}
		return false
	}
	c, f, n := newFixture(t, false, hook)
	waitState(t, c, n, func(s State) bool { return s.Diff != nil && !s.Loading && len(s.Commits) > 0 })
	if result := waitResult(t, c, n, c.MarkReviewed("file-0", true)); result.Error != "" {
		t.Fatalf("marks incorrectly depend on comments: %#v", result)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.requests {
		if strings.HasSuffix(request, "/comments") {
			t.Errorf("disabled comments fetched: %s", request)
		}
	}
}

func TestFilesFollowFolderFirstTraversal(t *testing.T) {
	files := []goclient.ChangedFile{{Id: "1", Path: "root.go"}, {Id: "2", Path: "z/b.go"}, {Id: "3", Path: "a/file.go"}, {Id: "4", Path: "a/nested/file.go"}}
	ordered := FilesInTreeOrder(files)
	want := []string{"a/nested/file.go", "a/file.go", "z/b.go", "root.go"}
	for i, file := range ordered {
		if file.Path != want[i] {
			t.Fatalf("file order %#v", ordered)
		}
	}
	if files[0].Path != "root.go" {
		t.Fatal("reordered caller slice")
	}
}

func TestBulkWritesRetainEveryAdmittedOperation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var bodies []string
	hook := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/comments/comment") {
			return false
		}
		var body goclient.UpdateComment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		first := len(bodies) == 0
		if body.Body != nil {
			bodies = append(bodies, *body.Body)
		}
		mu.Unlock()
		if first {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		jsonResponse(w, 200, goclient.ReviewComment{Id: "comment"})
		return true
	}
	c, _, n := newFixture(t, false, hook)
	waitState(t, c, n, loaded)
	operations := make([]uint64, 100)
	for i := range operations {
		body := strconv.Itoa(i)
		operations[i] = c.UpdateComment("comment", goclient.UpdateComment{Body: &body})
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("bulk did not start")
	}
	close(release)
	for _, id := range operations {
		if result := waitResult(t, c, n, id); result.Error != "" {
			t.Fatalf("bulk action dropped: %#v", result)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 100 {
		t.Fatalf("bulk applied %d operations", len(bodies))
	}
	for i, body := range bodies {
		if body != strconv.Itoa(i) {
			t.Fatalf("bulk reordered at %d: %s", i, body)
		}
	}
}
