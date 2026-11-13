package reviewservice

import (
	"errors"
	"testing"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewsession"
)

// eventsSpy counts committed comment changes the service announces.
type eventsSpy struct{ changes []string }

func (spy *eventsSpy) CommentsChanged(contextID, streamID string) {
	spy.changes = append(spy.changes, contextID+"/"+streamID)
}

func threadService(t *testing.T, comments ...review.ReviewComment) (*Service, *storeStub, *eventsSpy) {
	t.Helper()
	source := &sourceStub{support: diffsource.Support{Scopes: []review.DiffMode{review.DiffAll}}}
	store := &storeStub{comments: comments}
	session := boundSession(source, reviewsession.Policies{})
	session.ContextID, session.StreamID = "context", "stream"
	service := New(session, store)
	spy := &eventsSpy{}
	service.events = spy
	return service, store, spy
}

func status(err error) int {
	var request *review.RequestError
	if errors.As(err, &request) {
		return request.Status
	}
	return 0
}

func TestRepliesAppendInOrderWithTheirAuthors(t *testing.T) {
	service, _, spy := threadService(t, comment("id", review.DiffAll, "file.go", "old", "open"))
	if _, err := service.AddReply(t.Context(), "id", "  Why?  ", review.UserAuthor()); err != nil {
		t.Fatal(err)
	}
	got, err := service.AddReply(t.Context(), "id", "Fixed", review.AgentAuthor("Claude", "claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Replies) != 2 || got.Replies[0].Body != "Why?" || got.Replies[0].Author != review.UserAuthor() ||
		got.Replies[1].Author.Kind != review.AuthorAgent || got.Replies[1].Author.Name != "Claude" || got.Replies[1].Author.Verified ||
		got.Replies[0].ID == "" || got.Replies[0].ID == got.Replies[1].ID || got.Placement == nil {
		t.Fatalf("thread = %#v", got.Replies)
	}
	if len(spy.changes) != 2 || spy.changes[0] != "context/stream" {
		t.Fatalf("events = %v", spy.changes)
	}
	if _, err := service.AddReply(t.Context(), "id", "  ", review.UserAuthor()); status(err) != 400 {
		t.Fatalf("blank reply: %v", err)
	}
	if _, err := service.AddReply(t.Context(), "id", "x", review.CommentAuthor{Kind: "robot"}); status(err) != 400 {
		t.Fatalf("invalid author: %v", err)
	}
	if _, err := service.AddReply(t.Context(), "missing", "x", review.UserAuthor()); status(err) != 404 {
		t.Fatalf("missing comment: %v", err)
	}
	if len(spy.changes) != 2 {
		t.Fatalf("rejected replies announced changes: %v", spy.changes)
	}
}

func TestUserEditsOnlyOwnRepliesAndDeletesAny(t *testing.T) {
	thread := comment("id", review.DiffAll, "file.go", "old", "open")
	thread.Replies = []review.CommentReply{
		{ID: "mine", Author: review.UserAuthor(), Body: "first", CreatedAt: 1},
		{ID: "agent", Author: review.AgentAuthor("Claude", ""), Body: "second", CreatedAt: 2},
	}
	service, store, spy := threadService(t, thread)
	got, err := service.EditReply(t.Context(), "id", "mine", " revised ")
	if err != nil || got.Replies[0].Body != "revised" || got.Replies[0].EditedAt <= 0 {
		t.Fatalf("edit own reply: %#v %v", got.Replies, err)
	}
	if _, err := service.EditReply(t.Context(), "id", "agent", "rewritten"); status(err) != 403 || store.comments[0].Replies[1].Body != "second" {
		t.Fatalf("edited agent reply: %v %#v", err, store.comments[0].Replies)
	}
	if _, err := service.EditReply(t.Context(), "id", "gone", "x"); status(err) != 404 {
		t.Fatalf("missing reply edit: %v", err)
	}
	if err := service.DeleteReply(t.Context(), "id", "agent"); err != nil || len(store.comments[0].Replies) != 1 {
		t.Fatalf("delete agent reply: %v %#v", err, store.comments[0].Replies)
	}
	if err := service.DeleteReply(t.Context(), "id", "agent"); status(err) != 404 {
		t.Fatalf("repeat delete: %v", err)
	}
	if len(spy.changes) != 2 {
		t.Fatalf("events = %v", spy.changes)
	}
}

func TestResolveRecordsFirstResolverAndAppendsEveryNote(t *testing.T) {
	service, store, spy := threadService(t, comment("id", review.DiffAll, "file.go", "old", "open"))
	agent := review.AgentAuthor("Claude", "")
	if _, err := service.ResolveComment(t.Context(), "id", agent, " Applied "); err != nil {
		t.Fatal(err)
	}
	resolved := store.comments[0]
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil || *resolved.ResolvedBy != agent || resolved.ResolvedAt <= 0 ||
		len(resolved.Replies) != 1 || resolved.Replies[0].Body != "Applied" || resolved.Replies[0].Author != agent {
		t.Fatalf("resolved = %#v", resolved)
	}
	// Status stays idempotent; a repeated note still joins the thread.
	if _, err := service.ResolveComment(t.Context(), "id", review.UserAuthor(), "Confirmed"); err != nil {
		t.Fatal(err)
	}
	if *store.comments[0].ResolvedBy != agent || len(store.comments[0].Replies) != 2 || store.comments[0].Replies[1].Author != review.UserAuthor() {
		t.Fatalf("repeat resolve = %#v", store.comments[0])
	}
	if _, err := service.ResolveComment(t.Context(), "id", review.CommentAuthor{Kind: review.AuthorUser, Name: "spoof"}, ""); status(err) != 400 {
		t.Fatalf("invalid resolver: %v", err)
	}
	if len(spy.changes) != 2 {
		t.Fatalf("events = %v", spy.changes)
	}
}

func TestAuthorFilterSelectsCommentKinds(t *testing.T) {
	agent := review.AgentAuthor("Claude", "")
	byAgent := comment("agent", review.DiffAll, "file.go", "old", "open")
	byAgent.Author = &agent
	legacy := comment("legacy", review.DiffAll, "file.go", "old", "open")
	user := review.UserAuthor()
	legacy.Author = &user
	service, _, _ := threadService(t, byAgent, legacy)
	for author, want := range map[string]string{review.AuthorAgent: "agent", review.AuthorUser: "legacy"} {
		listed, err := service.ListComments(t.Context(), false, author)
		if err != nil || len(listed) != 1 || listed[0].ID != want {
			t.Fatalf("%s list = %#v %v", author, listed, err)
		}
		all, err := service.Comments(t.Context(), author)
		if err != nil || len(all) != 1 || all[0].ID != want {
			t.Fatalf("%s comments = %#v %v", author, all, err)
		}
	}
	if _, err := service.ListComments(t.Context(), false, "robot"); status(err) != 400 {
		t.Fatalf("invalid filter: %v", err)
	}
}

func TestUserCannotRewriteAgentComments(t *testing.T) {
	agent := review.AgentAuthor("Claude", "")
	byAgent := comment("agent", review.DiffAll, "file.go", "old", "open")
	byAgent.Author = &agent
	service, store, _ := threadService(t, byAgent)
	body := "rewritten"
	if _, err := service.UpdateComment(t.Context(), "agent", UpdateCommentInput{Body: &body}); status(err) != 403 || store.putCalls != 0 {
		t.Fatalf("agent comment edit: %v writes %d", err, store.putCalls)
	}
	resolved := "resolved"
	if got, err := service.UpdateComment(t.Context(), "agent", UpdateCommentInput{Status: &resolved}); err != nil || got.ResolvedBy == nil || got.ResolvedBy.Kind != review.AuthorUser {
		t.Fatalf("user resolution of agent comment: %#v %v", got, err)
	}
}
