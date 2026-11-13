package storetest

import (
	"reflect"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
)

func reply(id string, author review.CommentAuthor, body string, created float64) review.CommentReply {
	return review.CommentReply{ID: id, Author: author, Body: body, CreatedAt: created}
}

func replyIDs(comment review.ReviewComment) []string {
	ids := make([]string, 0, len(comment.Replies))
	for _, reply := range comment.Replies {
		ids = append(ids, reply.ID)
	}
	return ids
}

// onlyComment reads the context stream's single comment.
func onlyComment(t *testing.T, store Store, contextID string) review.ReviewComment {
	t.Helper()
	comments, err := store.Comments(t.Context(), contextID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("stream comments: %#v %v", comments, err)
	}
	return comments[0]
}

func (harness Harness[S]) commentThreadsKeepOrderResolutionAndEndWithTheirComment(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	binding, err := ingest(t, store, owner, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := ingest(t, store, owner, observationRequest("other-source", "other"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, id := t.Context(), binding.ContextID
	if err := store.PutComment(ctx, id, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// Comments stored without an author read as the user's.
	if comment := onlyComment(t, store, id); comment.Author == nil || *comment.Author != review.UserAuthor() || len(comment.Replies) != 0 {
		t.Fatalf("legacy comment: %#v", comment)
	}
	agent := review.AgentAuthor("Claude", "claude-code")
	for _, added := range []review.CommentReply{reply("b", review.UserAuthor(), "first", 5), reply("a", agent, "same millisecond", 5), reply("c", review.UserAuthor(), "later", 6)} {
		if ok, err := store.AddReply(ctx, id, "comment", added); err != nil || !ok {
			t.Fatalf("add reply %s: %v %v", added.ID, ok, err)
		}
	}
	if ok, err := store.AddReply(ctx, id, "missing", reply("x", agent, "orphan", 7)); err != nil || ok {
		t.Fatalf("reply to a missing comment: %v %v", ok, err)
	}
	if ok, err := store.AddReply(ctx, other.ContextID, "comment", reply("x", agent, "foreign", 7)); err != nil || ok {
		t.Fatalf("reply through another stream: %v %v", ok, err)
	}
	comment := onlyComment(t, store, id)
	if !reflect.DeepEqual(replyIDs(comment), []string{"b", "a", "c"}) || comment.Replies[1].Author != agent {
		t.Fatalf("replies not in creation order: %#v", comment.Replies)
	}
	if ok, err := store.EditReply(ctx, id, "comment", "b", "revised", 9); err != nil || !ok {
		t.Fatalf("edit reply: %v %v", ok, err)
	}
	if ok, err := store.EditReply(ctx, other.ContextID, "comment", "b", "foreign", 9); err != nil || ok {
		t.Fatalf("edit through another stream: %v %v", ok, err)
	}
	if ok, err := store.EditReply(ctx, id, "comment", "missing", "x", 9); err != nil || ok {
		t.Fatalf("edit missing reply: %v %v", ok, err)
	}
	if edited := onlyComment(t, store, id).Replies[0]; edited.Body != "revised" || edited.EditedAt != 9 || edited.CreatedAt != 5 {
		t.Fatalf("edited reply: %#v", edited)
	}

	// Resolution and its note commit together; a repeat keeps the first resolver.
	note := reply("note", agent, "applied", 10)
	if ok, err := store.ResolveComment(ctx, id, "comment", agent, 10, &note); err != nil || !ok {
		t.Fatalf("resolve: %v %v", ok, err)
	}
	second := reply("confirm", review.UserAuthor(), "confirmed", 11)
	if ok, err := store.ResolveComment(ctx, id, "comment", review.UserAuthor(), 11, &second); err != nil || !ok {
		t.Fatalf("repeat resolve: %v %v", ok, err)
	}
	if ok, err := store.ResolveComment(ctx, id, "missing", agent, 12, nil); err != nil || ok {
		t.Fatalf("resolve missing comment: %v %v", ok, err)
	}
	resolved := onlyComment(t, store, id)
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil || *resolved.ResolvedBy != agent || resolved.ResolvedAt != 10 ||
		!reflect.DeepEqual(replyIDs(resolved), []string{"b", "a", "c", "note", "confirm"}) {
		t.Fatalf("resolved thread: %#v", resolved)
	}
	// Replacing the comment, as reopening does, keeps its thread.
	resolved.Status, resolved.ResolvedBy, resolved.ResolvedAt, resolved.Replies = "open", nil, 0, nil
	if ok, err := store.ReplaceComment(ctx, id, resolved); err != nil || !ok {
		t.Fatalf("reopen: %v %v", ok, err)
	}
	reopened := onlyComment(t, store, id)
	if reopened.Status != "open" || reopened.ResolvedBy != nil || reopened.ResolvedAt != 0 || len(reopened.Replies) != 5 {
		t.Fatalf("reopened thread: %#v", reopened)
	}

	if ok, err := store.DeleteReply(ctx, other.ContextID, "comment", "a"); err != nil || ok {
		t.Fatalf("delete through another stream: %v %v", ok, err)
	}
	if ok, err := store.DeleteReply(ctx, id, "comment", "a"); err != nil || !ok {
		t.Fatalf("delete reply: %v %v", ok, err)
	}
	if ok, err := store.DeleteReply(ctx, id, "comment", "a"); err != nil || ok {
		t.Fatalf("repeat delete reply: %v %v", ok, err)
	}
	if ids := replyIDs(onlyComment(t, store, id)); !reflect.DeepEqual(ids, []string{"b", "c", "note", "confirm"}) {
		t.Fatalf("after delete: %v", ids)
	}

	// Deleting the comment deletes its thread; a comment reusing the ID starts empty.
	if deleted, err := store.DeleteComment(ctx, id, "comment"); err != nil || !deleted {
		t.Fatalf("delete comment: %v %v", deleted, err)
	}
	if err := store.PutComment(ctx, id, review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "again", Status: "open", CreatedAt: 20}); err != nil {
		t.Fatal(err)
	}
	if comment := onlyComment(t, store, id); len(comment.Replies) != 0 {
		t.Fatalf("thread outlived its comment: %#v", comment.Replies)
	}
}

func (harness Harness[S]) commentThreadsEndWithTheirStream(t *testing.T) {
	for _, end := range []string{"delete", "prune"} {
		t.Run(end, func(t *testing.T) {
			store := harness.open(t)
			owner := harness.Owner(t, store, "owner")
			binding, err := ingest(t, store, owner, observationRequest("source", "only"))
			if err != nil {
				t.Fatal(err)
			}
			comment := review.ReviewComment{ID: "comment", Scope: review.DiffAll, Body: "check", Status: "open", CreatedAt: 1}
			if err := store.PutComment(t.Context(), binding.ContextID, comment); err != nil {
				t.Fatal(err)
			}
			if ok, err := store.AddReply(t.Context(), binding.ContextID, "comment", reply("r", review.UserAuthor(), "reply", 2)); err != nil || !ok {
				t.Fatalf("add reply: %v %v", ok, err)
			}
			if end == "delete" {
				err = store.DeleteContext(t.Context(), owner, binding.ContextID)
			} else {
				err = store.PruneExpired(time.Now().Add(30 * 24 * time.Hour))
			}
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := ingest(t, store, owner, observationRequest("source", "fresh"))
			if err != nil {
				t.Fatal(err)
			}
			// The stream's new review can reuse the ID without inheriting the thread.
			if err := store.PutComment(t.Context(), fresh.ContextID, comment); err != nil {
				t.Fatal(err)
			}
			if comment := onlyComment(t, store, fresh.ContextID); len(comment.Replies) != 0 {
				t.Fatalf("thread outlived its stream: %#v", comment.Replies)
			}
		})
	}
}

func (harness Harness[S]) importedThreadsRoundTripWithoutOverwriting(t *testing.T) {
	store := harness.open(t)
	owner := harness.Owner(t, store, "owner")
	binding, err := ingest(t, store, owner, observationRequest("source", "only"))
	if err != nil {
		t.Fatal(err)
	}
	agent := review.AgentAuthor("Claude", "claude-code")
	user := review.UserAuthor()
	exported := review.ReviewComment{
		ID: "comment", DiffID: binding.DiffIDs[review.DiffAll], Scope: review.DiffAll, Path: "file.txt", Target: "file", Side: "additions",
		Body: "check", Status: "resolved", CreatedAt: 1, Author: &agent, ResolvedBy: &user, ResolvedAt: 4,
		Replies: []review.CommentReply{reply("r1", user, "why", 2), {ID: "r2", Author: agent, Body: "fixed", CreatedAt: 3, EditedAt: 4}},
	}
	if _, err := store.ImportComments(t.Context(), binding.ContextID, []review.ReviewComment{exported}); err != nil {
		t.Fatal(err)
	}
	imported := onlyComment(t, store, binding.ContextID)
	imported.Placement = nil
	if !reflect.DeepEqual(imported, exported) {
		t.Fatalf("imported thread:\n got %#v\nwant %#v", imported, exported)
	}
	// Imports never overwrite an existing comment or its thread.
	changed := exported
	changed.Replies = []review.CommentReply{reply("r3", agent, "replacement", 5)}
	if _, err := store.ImportComments(t.Context(), binding.ContextID, []review.ReviewComment{changed}); err != nil {
		t.Fatal(err)
	}
	if ids := replyIDs(onlyComment(t, store, binding.ContextID)); !reflect.DeepEqual(ids, []string{"r1", "r2"}) {
		t.Fatalf("import overwrote a thread: %v", ids)
	}
}
