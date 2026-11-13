package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/caller"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewservice"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const patch = "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n package main\n-var old = 1\n+var value = 2\n+var added = 3\n func main() {}\n"

// checkout is the provenance of a collected review stream.
type checkout struct{ repository, remote, branch, worktree, host string }

var (
	featureCheckout = checkout{"shop", "git@github.com:acme/shop.git", "feature", "shop-feature", "laptop"}
	mainCheckout    = checkout{"shop", "git@github.com:acme/shop.git", "main", "shop", "laptop"}
)

func observation(where checkout, submission string, collected int64) ingestion.Request {
	file := review.ChangedFile{ID: "main", Path: "main.go", Status: "modified", Additions: 2, Deletions: 1, Fingerprint: "fingerprint-" + submission}
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission,
		Metadata: ingestion.Metadata{
			SourceID: "source", Hostname: where.host, RepositoryKey: "key-" + where.repository, RepositoryName: where.repository, RemoteURL: where.remote,
			CheckoutKey: "checkout-" + where.worktree, Root: "/work/" + where.worktree, WorktreeName: where.worktree, Branch: where.branch, CollectedAt: collected, Trigger: "manual",
		},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{Mode: review.DiffAll, Source: "local", Name: where.repository, Branch: where.branch, Root: "/work/" + where.worktree, Revision: "revision-" + submission, Files: []review.ChangedFile{file}},
			Patches:  map[string]review.FilePatch{"main": {Patch: patch, Contents: &review.FileContents{Before: "package main\nvar old = 1\nfunc main() {}\n", After: "package main\nvar value = 2\nvar added = 3\nfunc main() {}\n"}}},
		}},
	}
}

type fixture struct {
	t       *testing.T
	store   *reviewstore.Store
	service *contextservice.Service
	catalog *reviewservice.Catalog
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := reviewstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, err := store.User("owner", "owner")
	if err != nil {
		t.Fatal(err)
	}
	service := contextservice.New(store, user)
	t.Cleanup(func() { _ = service.Close() })
	return &fixture{t: t, store: store, service: service, catalog: reviewservice.NewCatalog(service, store)}
}

func (f *fixture) ingest(request ingestion.Request) contextservice.Context {
	f.t.Helper()
	published, err := f.service.Ingest(f.t.Context(), request)
	if err != nil {
		f.t.Fatal(err)
	}
	return published.Context
}

// comment stores a user's file comment with a chosen ID on the context's stream.
func (f *fixture) comment(contextID, id, body string) {
	f.t.Helper()
	snapshot, err := f.catalog.GetDiff(f.t.Context(), contextID, review.DiffAll)
	if err != nil {
		f.t.Fatal(err)
	}
	user := review.UserAuthor()
	file := snapshot.Files[0]
	if err := f.store.PutComment(f.t.Context(), contextID, review.ReviewComment{
		ID: id, DiffID: snapshot.ID, VersionID: snapshot.VersionID, Path: file.Path, Scope: review.DiffAll, Fingerprint: file.Fingerprint,
		Target: "file", Side: "additions", Body: body, Status: "open", CreatedAt: 1, Author: &user,
	}); err != nil {
		f.t.Fatal(err)
	}
}

// serve exposes the catalog's MCP endpoint as composition would, with who as
// the authenticated caller when set.
func (f *fixture) serve(scoped, fallback string, who *caller.Caller) *httptest.Server {
	f.t.Helper()
	handler := NewCatalog(f.catalog, scoped, fallback, "test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if who != nil {
			r = r.WithContext(caller.With(r.Context(), *who))
		}
		handler.ServeHTTP(w, r)
	}))
	f.t.Cleanup(server.Close)
	return server
}

func connect(t *testing.T, server *httptest.Server, protocolVersion string, client *mcp.Implementation) *mcp.ClientSession {
	t.Helper()
	session, err := mcp.NewClient(client, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}}).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

var agent = &mcp.Implementation{Name: "claude-code", Title: "Claude Code", Version: "test"}

func call(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// succeed calls a tool that must succeed and decodes its structured output.
func succeed[T any](t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) T {
	t.Helper()
	result := call(t, session, name, arguments)
	if result.IsError {
		t.Fatalf("%s %v: %s", name, arguments, text(result))
	}
	var output T
	raw, err := json.Marshal(result.StructuredContent)
	if err == nil {
		err = json.Unmarshal(raw, &output)
	}
	if err != nil {
		t.Fatal(err)
	}
	return output
}

// fail calls a tool that must report a tool error and returns its text.
func fail(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) string {
	t.Helper()
	result := call(t, session, name, arguments)
	if !result.IsError {
		t.Fatalf("%s %v unexpectedly succeeded: %#v", name, arguments, result.StructuredContent)
	}
	return text(result)
}

func text(result *mcp.CallToolResult) string {
	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if value, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, value.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	return names
}

var (
	readTools  = []string{"find_feedback", "get_diff", "get_file_patch", "get_review_comments", "list_commits", "list_contexts"}
	writeTools = []string{"create_review_comment", "reply_to_review_comment", "resolve_review_comment"}
)

func TestToolSetFollowsCallerScopes(t *testing.T) {
	f := newFixture(t)
	all := slices.Sorted(slices.Values(append(slices.Clone(readTools), writeTools...)))
	for _, test := range []struct {
		name string
		who  *caller.Caller
		want []string
	}{
		{"local", nil, all},
		{"session", &caller.Caller{Kind: caller.KindSession}, all},
		{"api token", &caller.Caller{Kind: caller.KindAPIToken, Name: "ci"}, all},
		{"oauth writer", &caller.Caller{Kind: caller.KindOAuth, Name: "Cursor", Scopes: []string{caller.ScopeReviewsRead, caller.ScopeCommentsWrite}}, all},
		{"oauth reader", &caller.Caller{Kind: caller.KindOAuth, Name: "Cursor", Scopes: []string{caller.ScopeReviewsRead}}, readTools},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := connect(t, f.serve("", "", test.who), ProtocolVersion, agent)
			if got := toolNames(t, session); !slices.Equal(got, test.want) {
				t.Fatalf("tools = %v, want %v", got, test.want)
			}
		})
	}
}

func TestToolAnnotationsAndSchemas(t *testing.T) {
	f := newFixture(t)
	session := connect(t, f.serve("", "", nil), ProtocolVersion, agent)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		write := slices.Contains(writeTools, tool.Name)
		if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint == write {
			t.Fatalf("%s: %#v", tool.Name, tool)
		}
		if tool.Annotations.IdempotentHint != (!write || tool.Name == "resolve_review_comment") {
			t.Fatalf("%s idempotency: %#v", tool.Name, tool.Annotations)
		}
	}
}

func TestAddressFeedbackPrompt(t *testing.T) {
	f := newFixture(t)
	session := connect(t, f.serve("", "", nil), ProtocolVersion, agent)
	prompts, err := session.ListPrompts(t.Context(), nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "address-feedback" || len(prompts.Prompts[0].Arguments) != 1 || prompts.Prompts[0].Arguments[0].Required {
		t.Fatalf("prompts: %#v %v", prompts, err)
	}
	for _, test := range []struct {
		arguments map[string]string
		want      []string
	}{
		{map[string]string{"ref": "deadbeef01"}, []string{`find_feedback with ref "deadbeef01"`, "untrusted", "reply_to_review_comment", "resolve_review_comment", "Do not resolve threads you did not handle"}},
		{nil, []string{"find_feedback with this checkout's repository", "branch and worktree"}},
	} {
		got, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "address-feedback", Arguments: test.arguments})
		if err != nil || len(got.Messages) != 1 {
			t.Fatalf("get prompt: %#v %v", got, err)
		}
		content, ok := got.Messages[0].Content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("prompt content: %#v", got.Messages[0].Content)
		}
		for _, want := range test.want {
			if !strings.Contains(content.Text, want) {
				t.Fatalf("prompt %q lacks %q", content.Text, want)
			}
		}
	}
}

// Stateless older-protocol requests carry no client info after initialize,
// so their writes fall back to the generic agent name.
func TestOlderProtocolsInitializeAndCallTools(t *testing.T) {
	f := newFixture(t)
	head := f.ingest(observation(featureCheckout, "one", 100))
	server := f.serve("", "", nil)
	for _, version := range []string{"2025-11-25", "2025-06-18"} {
		t.Run(version, func(t *testing.T) {
			session := connect(t, server, version, agent)
			if got := session.InitializeResult(); got == nil || got.ProtocolVersion != version || got.Capabilities.Prompts == nil || got.Capabilities.Tools == nil {
				t.Fatalf("initialize: %#v", got)
			}
			if len(toolNames(t, session)) != len(readTools)+len(writeTools) {
				t.Fatal("older client lacks tools")
			}
			created := succeed[commentOutput](t, session, "create_review_comment", map[string]any{"context_id": head.ID, "path": "main.go", "target": "file", "body": "From " + version})
			if created.Comment.Author == nil || created.Comment.Author.Name != "Agent" {
				t.Fatalf("older client author: %#v", created.Comment.Author)
			}
			found := succeed[findFeedbackOutput](t, session, "find_feedback", map[string]any{"ref": created.Comment.ID})
			if found.ContextID != head.ID || found.FocusCommentID != created.Comment.ID {
				t.Fatalf("older client find_feedback: %#v", found)
			}
		})
	}
}

func TestFindFeedbackResolvesReferences(t *testing.T) {
	f := newFixture(t)
	first := f.ingest(observation(featureCheckout, "first", 100))
	latest := f.ingest(observation(featureCheckout, "latest", 200))
	other := f.ingest(observation(mainCheckout, "main", 150))
	if first.StreamID == nil || latest.StreamID == nil || *first.StreamID != *latest.StreamID || first.ID == latest.ID {
		t.Fatalf("stream fixtures: %#v %#v", first, latest)
	}
	stream := *latest.StreamID
	f.comment(latest.ID, "deadbeef01aa", "Rename this.")
	f.comment(latest.ID, "cafe0001aaaa", "Second thread.")
	f.comment(other.ID, "deadbeef02bb", "On main.")
	session := connect(t, f.serve("", "", nil), ProtocolVersion, agent)

	found := succeed[findFeedbackOutput](t, session, "find_feedback", map[string]any{"ref": "https://diffx.example.com/streams/" + stream})
	if found.ContextID != latest.ID || found.StreamID != stream || found.Repository != "shop" || found.Branch != "feature" || found.Worktree != "shop-feature" ||
		found.Permalink != "/streams/"+stream || found.FocusCommentID != "" || len(found.Threads) != 2 {
		t.Fatalf("stream link: %#v", found)
	}
	thread := found.Threads[slices.IndexFunc(found.Threads, func(thread feedbackThread) bool { return thread.ID == "deadbeef01aa" })]
	if thread.Path != "main.go" || thread.Target != "file" || thread.Body != "Rename this." || thread.Author.Kind != review.AuthorUser ||
		thread.Status != "open" || thread.Permalink != "/streams/"+stream+"?comment=deadbeef01aa" {
		t.Fatalf("thread: %#v", thread)
	}

	pinned := succeed[findFeedbackOutput](t, session, "find_feedback", map[string]any{"ref": "/contexts/" + first.ID})
	if pinned.ContextID != first.ID || len(pinned.Threads) != 2 {
		t.Fatalf("context link: %#v", pinned)
	}

	for _, ref := range []string{"http://localhost:7981/streams/" + stream + "?comment=deadbeef01aa", "deadbeef01aa", "deadbeef01"} {
		focused := succeed[findFeedbackOutput](t, session, "find_feedback", map[string]any{"ref": ref})
		if focused.ContextID != latest.ID || focused.FocusCommentID != "deadbeef01aa" || len(focused.Threads) != 1 || focused.Threads[0].ID != "deadbeef01aa" || focused.OtherOpenThreads != 1 {
			t.Fatalf("comment ref %q: %#v", ref, focused)
		}
	}
	if focused := succeed[findFeedbackOutput](t, session, "find_feedback", map[string]any{"ref": "/contexts/" + first.ID + "?comment=cafe0001aaaa"}); focused.ContextID != first.ID || focused.FocusCommentID != "cafe0001aaaa" {
		t.Fatalf("pinned comment link: %#v", focused)
	}

	ambiguous := fail(t, session, "find_feedback", map[string]any{"ref": "deadbeef"})
	for _, want := range []string{"deadbeef01aa", "deadbeef02bb", "shop (feature, worktree shop-feature)", "shop (main", "main.go", "more characters"} {
		if !strings.Contains(ambiguous, want) {
			t.Fatalf("ambiguous prefix error %q lacks %q", ambiguous, want)
		}
	}
	for ref, want := range map[string]string{
		"ffffffffffff":            "No review comment matches",
		"dead":                    "at least 8 characters",
		"/settings/notifications": "Unrecognized diffx link",
		"/streams/unknown-stream": "not found",
		"/streams/" + stream + "?comment=missing": "not found",
	} {
		if got := fail(t, session, "find_feedback", map[string]any{"ref": ref}); !strings.Contains(got, want) {
			t.Fatalf("ref %q: %q lacks %q", ref, got, want)
		}
	}
}

func TestFindFeedbackUsesCheckoutHintsAndDefaults(t *testing.T) {
	f := newFixture(t)
	f.ingest(observation(featureCheckout, "old", 100))
	feature := f.ingest(observation(featureCheckout, "new", 200))
	mainline := f.ingest(observation(mainCheckout, "main", 150))
	global := connect(t, f.serve("", "", nil), ProtocolVersion, agent)

	for _, hints := range []map[string]any{
		{"repository": "shop", "branch": "feature"},
		{"repository": "https://github.com/acme/shop", "worktree": "shop-feature"},
		{"repository": "git@github.com:acme/shop.git", "branch": "feature", "hostname": "laptop"},
	} {
		if found := succeed[findFeedbackOutput](t, global, "find_feedback", hints); found.ContextID != feature.ID {
			t.Fatalf("hints %v: %#v", hints, found)
		}
	}
	several := succeed[findFeedbackOutput](t, global, "find_feedback", map[string]any{"repository": "shop"})
	if several.ContextID != "" || len(several.Candidates) != 2 || several.Guidance == "" || len(several.Threads) != 0 {
		t.Fatalf("several heads: %#v", several)
	}
	ids := []string{several.Candidates[0].ContextID, several.Candidates[1].ContextID}
	if !slices.Contains(ids, feature.ID) || !slices.Contains(ids, mainline.ID) || several.Candidates[0].Repository != "shop" || several.Candidates[0].Hostname != "laptop" {
		t.Fatalf("candidates: %#v", several.Candidates)
	}
	if got := fail(t, global, "find_feedback", map[string]any{"branch": "missing"}); !strings.Contains(got, "list_contexts") {
		t.Fatalf("no head: %q", got)
	}
	if got := fail(t, global, "find_feedback", map[string]any{}); !strings.Contains(got, "ref") {
		t.Fatalf("no ref, hints or default: %q", got)
	}

	withDefault := connect(t, f.serve("", mainline.ID, nil), ProtocolVersion, agent)
	if found := succeed[findFeedbackOutput](t, withDefault, "find_feedback", map[string]any{}); found.ContextID != mainline.ID {
		t.Fatalf("default context: %#v", found)
	}
	// A default does not scope the global endpoint.
	succeed[getReviewCommentsOutput](t, withDefault, "get_review_comments", map[string]any{"context_id": feature.ID})

	scoped := connect(t, f.serve(feature.ID, "", nil), ProtocolVersion, agent)
	if found := succeed[findFeedbackOutput](t, scoped, "find_feedback", map[string]any{}); found.ContextID != feature.ID {
		t.Fatalf("scoped context: %#v", found)
	}
	if got := fail(t, scoped, "find_feedback", map[string]any{"ref": "/contexts/" + mainline.ID}); !strings.Contains(got, "global /mcp") {
		t.Fatalf("scoped endpoint with a foreign ref: %q", got)
	}
	fail(t, scoped, "get_review_comments", map[string]any{"context_id": mainline.ID})
}

func TestAgentsCreateReplyAndResolveFeedback(t *testing.T) {
	f := newFixture(t)
	head := f.ingest(observation(featureCheckout, "one", 100))
	stream := *head.StreamID
	who := &caller.Caller{Kind: caller.KindAPIToken, Name: "ci-token"}
	session := connect(t, f.serve(head.ID, "", who), ProtocolVersion, agent)

	line := succeed[commentOutput](t, session, "create_review_comment", map[string]any{"path": "main.go", "side": "additions", "start": 2, "end": 3, "body": "Explain these."})
	want, ok := review.PatchContext(patch, "additions", 2, 3)
	comment := line.Comment
	if !ok || comment.Code != want || comment.Anchor == nil || comment.Scope != review.DiffAll || comment.Side != "additions" || comment.Start != 2 || comment.End != 3 ||
		comment.Placement == nil || comment.Placement.Applicability != review.Anchored || line.Permalink != "/streams/"+stream+"?comment="+comment.ID {
		t.Fatalf("line comment: %#v %q", comment, line.Permalink)
	}
	if author := comment.Author; author == nil || author.Kind != review.AuthorAgent || author.Name != "Claude Code (ci-token)" || author.Verified {
		t.Fatalf("api token author: %#v", author)
	}
	file := succeed[commentOutput](t, session, "create_review_comment", map[string]any{"path": "main.go", "target": "file", "body": "Whole file."})
	if file.Comment.Target != "file" || file.Comment.Code != "" || file.Comment.Start != 0 {
		t.Fatalf("file comment: %#v", file.Comment)
	}
	for arguments, want := range map[*map[string]any]string{
		{"path": "main.go", "side": "additions", "start": 40, "end": 41, "body": "x"}: "200 visible lines",
		{"path": "main.go", "side": "sideways", "start": 1, "end": 1, "body": "x"}:    "Invalid comment",
		{"path": "other.go", "target": "file", "body": "x"}:                           "not in this review",
		{"path": "main.go", "target": "file", "scope": "staged", "body": "x"}:         "not enabled",
		{"path": "main.go", "target": "file", "body": " "}:                            "Invalid comment",
	} {
		if got := fail(t, session, "create_review_comment", *arguments); !strings.Contains(got, want) {
			t.Fatalf("create %v: %q lacks %q", *arguments, got, want)
		}
	}

	replied := succeed[commentOutput](t, session, "reply_to_review_comment", map[string]any{"comment_id": comment.ID, "body": "Added a doc comment."})
	if len(replied.Comment.Replies) != 1 || replied.Comment.Replies[0].Body != "Added a doc comment." || replied.Comment.Replies[0].Author.Name != "Claude Code (ci-token)" {
		t.Fatalf("reply: %#v", replied.Comment.Replies)
	}
	fail(t, session, "reply_to_review_comment", map[string]any{"comment_id": "missing", "body": "x"})

	resolved := succeed[resolveReviewCommentOutput](t, session, "resolve_review_comment", map[string]any{"comment_ids": []string{comment.ID, file.Comment.ID, "missing", comment.ID}, "note": "Done."})
	if len(resolved.Results) != 3 || resolved.Results[0] != (resolveResult{CommentID: comment.ID, Status: "resolved"}) || resolved.Results[1].Status != "resolved" ||
		resolved.Results[2].CommentID != "missing" || resolved.Results[2].Status != "error" || !strings.Contains(resolved.Results[2].Error, "missing") {
		t.Fatalf("batch resolve: %#v", resolved)
	}
	single := succeed[resolveReviewCommentOutput](t, session, "resolve_review_comment", map[string]any{"comment_id": comment.ID})
	if single.CommentID != comment.ID || single.Status != "resolved" || single.Results != nil {
		t.Fatalf("single resolve stays compatible: %#v", single)
	}
	if got := fail(t, session, "resolve_review_comment", map[string]any{"comment_id": "missing"}); !strings.Contains(got, "missing") {
		t.Fatalf("single unknown resolve: %q", got)
	}

	listed := succeed[getReviewCommentsOutput](t, session, "get_review_comments", map[string]any{"comment_id": comment.ID})
	if len(listed.Comments) != 1 {
		t.Fatalf("comment_id filter returns a resolved comment: %#v", listed)
	}
	thread := listed.Comments[0]
	if thread.Status != "resolved" || thread.ResolvedBy == nil || thread.ResolvedBy.Name != "Claude Code (ci-token)" || len(thread.Replies) != 2 || thread.Replies[1].Body != "Done." {
		t.Fatalf("resolved thread: %#v", thread)
	}
	if open := succeed[getReviewCommentsOutput](t, session, "get_review_comments", map[string]any{}); len(open.Comments) != 0 {
		t.Fatalf("open comments after resolve: %#v", open)
	}
	f.comment(head.ID, "user-comment", "From the user.")
	for arguments, want := range map[*map[string]any]int{
		{"include_resolved": true}:                     3,
		{"include_resolved": true, "author": "agent"}:  2,
		{"include_resolved": true, "author": "user"}:   1,
		{"include_resolved": true, "path": "main.go"}:  3,
		{"include_resolved": true, "path": "other.go"}: 0,
	} {
		if got := succeed[getReviewCommentsOutput](t, session, "get_review_comments", *arguments); len(got.Comments) != want {
			t.Fatalf("filter %v: %d comments, want %d", *arguments, len(got.Comments), want)
		}
	}
	fail(t, session, "get_review_comments", map[string]any{"author": "robot"})
}

func TestWriteAttribution(t *testing.T) {
	f := newFixture(t)
	head := f.ingest(observation(featureCheckout, "one", 100))
	for _, test := range []struct {
		name   string
		who    *caller.Caller
		client *mcp.Implementation
		want   review.CommentAuthor
	}{
		{"oauth", &caller.Caller{Kind: caller.KindOAuth, Name: "Cursor", Verified: true, ClientID: "https://cursor.example/client", Scopes: []string{caller.ScopeCommentsWrite}}, agent,
			review.CommentAuthor{Kind: review.AuthorAgent, Name: "Cursor", Verified: true, ClientID: "https://cursor.example/client"}},
		{"local title", nil, agent, review.CommentAuthor{Kind: review.AuthorAgent, Name: "Claude Code"}},
		{"local name", nil, &mcp.Implementation{Name: "codex", Version: "1"}, review.CommentAuthor{Kind: review.AuthorAgent, Name: "codex"}},
		{"session", &caller.Caller{Kind: caller.KindSession}, agent, review.CommentAuthor{Kind: review.AuthorAgent, Name: "Claude Code"}},
		{"api token", &caller.Caller{Kind: caller.KindAPIToken, Name: "laptop"}, agent, review.CommentAuthor{Kind: review.AuthorAgent, Name: "Claude Code (laptop)"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := connect(t, f.serve(head.ID, "", test.who), ProtocolVersion, test.client)
			created := succeed[commentOutput](t, session, "create_review_comment", map[string]any{"path": "main.go", "target": "file", "body": test.name})
			if created.Comment.Author == nil || *created.Comment.Author != test.want {
				t.Fatalf("author = %#v, want %#v", created.Comment.Author, test.want)
			}
		})
	}
	for _, test := range []struct {
		who  caller.Caller
		want string
	}{
		{caller.Caller{Kind: caller.KindAPIToken, Name: "laptop"}, "laptop"},
		{caller.Caller{Kind: caller.KindAPIToken}, "Agent"},
		{caller.Caller{}, "Agent"},
		{caller.Caller{Kind: caller.KindOAuth, Scopes: []string{caller.ScopeCommentsWrite}}, "Agent"},
	} {
		if got := agentAuthor(test.who, nil); got.Name != test.want || got.Verified || !got.Valid() {
			t.Fatalf("%#v without client info: %#v", test.who, got)
		}
	}
}

func TestToolErrorIsVisibleToModel(t *testing.T) {
	f := newFixture(t)
	head := f.ingest(observation(featureCheckout, "one", 100))
	session := connect(t, f.serve(head.ID, "", nil), ProtocolVersion, agent)
	result := call(t, session, "resolve_review_comment", map[string]any{"comment_id": "missing"})
	if !result.IsError || len(result.Content) != 1 || !strings.Contains(text(result), "missing") {
		t.Fatalf("result: %#v", result)
	}
}

// blockingProvider holds catalog listing until its request is cancelled.
type blockingProvider struct {
	*contextservice.Service
	started, canceled chan struct{}
}

func (provider blockingProvider) ListFiltered(ctx context.Context, _ int, _ string, _ ingestion.Filter) (contextservice.Page, error) {
	close(provider.started)
	<-ctx.Done()
	close(provider.canceled)
	return contextservice.Page{}, ctx.Err()
}

func TestRequestCancellationReachesService(t *testing.T) {
	f := newFixture(t)
	provider := blockingProvider{Service: f.service, started: make(chan struct{}), canceled: make(chan struct{})}
	f.catalog = reviewservice.NewCatalog(provider, f.store)
	session := connect(t, f.serve("", "", nil), ProtocolVersion, agent)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_contexts", Arguments: map[string]any{}})
	}()
	<-provider.started
	cancel()
	<-done
	<-provider.canceled
}
