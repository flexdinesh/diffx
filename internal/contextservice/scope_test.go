package contextservice

import (
	"github.com/flexdinesh/diffx/internal/testsupport/storetest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/ingestion"
)

func TestScopeLimitsCatalogAndLookupsToSelectedCheckoutOrCapture(t *testing.T) {
	store, err := storetest.Open(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, err := store.User("test-user", "test-user")
	if err != nil {
		t.Fatal(err)
	}
	unscoped := New(store, user)
	t.Cleanup(func() { _ = unscoped.Close() })
	root := testRepo(t)
	catalogGit(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	catalogGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	catalogGit(t, root, "worktree", "add", "-qb", "feature", linked)
	if err := os.WriteFile(filepath.Join(linked, "file"), []byte("linked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ingest := func(input ingestion.Request) Context {
		t.Helper()
		submitted, err := unscoped.Ingest(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		return submitted.Context
	}
	collect := func(path, submission string) ingestion.Request {
		t.Helper()
		input, err := collector.Collect(t.Context(), path, collector.Options{SourceID: "source", SubmissionID: submission})
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	pipe := func(patch, submission string) ingestion.Request {
		t.Helper()
		input, err := collector.CollectPatch(t.Context(), patch, root, collector.Options{SourceID: "source", SubmissionID: submission})
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	selected := collect(root, "selected-1")
	first := ingest(selected)
	linkedInput := collect(linked, "linked")
	other := ingest(linkedInput)
	captured := pipe(testPatch, "piped-1")
	capture := ingest(captured)
	otherCapture := ingest(pipe(testPatch+"\n", "piped-2"))
	// A later collection of the same checkout on another branch stays in scope.
	catalogGit(t, root, "switch", "-qc", "later")
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := ingest(collect(root, "selected-2"))
	// A later process piping the same patch reuses the stored capture.
	repeated := pipe(testPatch, "piped-3")
	if reused := ingest(repeated); reused.ID != capture.ID {
		t.Fatalf("matching capture not reused: %s, want %s", reused.ID, capture.ID)
	}

	for _, test := range []struct {
		name    string
		scope   Scope
		visible []string
		hidden  []string
	}{
		{"checkout", ScopeFor(selected), []string{first.ID, later.ID}, []string{other.ID, capture.ID, otherCapture.ID}},
		{"checkouts", ScopeFor(selected, linkedInput), []string{first.ID, other.ID, later.ID}, []string{capture.ID, otherCapture.ID}},
		{"no checkouts", Scope{SourceID: "source"}, nil, []string{first.ID, other.ID, later.ID, capture.ID, otherCapture.ID}},
		{"awaiting discovery", ScopeFor(), nil, []string{first.ID, other.ID, later.ID, capture.ID, otherCapture.ID}},
		{"capture", ScopeFor(repeated), []string{capture.ID}, []string{first.ID, other.ID, later.ID, otherCapture.ID}},
		{"unscoped", Scope{}, []string{first.ID, other.ID, capture.ID, otherCapture.ID, later.ID}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewScoped(t.Context(), store, user, test.scope)
			defer service.Close()
			var listed []string
			cursor := ""
			for {
				page, err := service.List(t.Context(), 1, cursor)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Contexts {
					listed = append(listed, item.ID)
				}
				if page.NextCursor == nil {
					break
				}
				cursor = *page.NextCursor
			}
			slices.Sort(listed)
			want := slices.Sorted(slices.Values(test.visible))
			if !slices.Equal(listed, want) {
				t.Fatalf("listed %v, want %v", listed, want)
			}
			filtered, err := service.ListFiltered(t.Context(), 100, "", ingestion.Filter{SourceID: "source"})
			if err != nil || len(filtered.Contexts) != len(test.visible) {
				t.Fatalf("filtered scope: %#v, %v", filtered, err)
			}
			if count, err := service.Count(t.Context()); err != nil || count != len(test.visible) {
				t.Fatalf("count = %d, %v; want %d", count, err, len(test.visible))
			}
			for _, id := range test.visible {
				if _, err := service.Get(t.Context(), id); err != nil {
					t.Fatalf("get %s: %v", id, err)
				}
				if _, err := service.Resolve(t.Context(), id); err != nil {
					t.Fatalf("resolve %s: %v", id, err)
				}
			}
			for _, id := range test.hidden {
				_, err := service.Get(t.Context(), id)
				assertStatus(t, err, 404)
				_, err = service.Resolve(t.Context(), id)
				assertStatus(t, err, 404)
				assertStatus(t, service.Delete(t.Context(), id), 404)
				if _, err := unscoped.Get(t.Context(), id); err != nil {
					t.Fatalf("out-of-scope delete removed %s: %v", id, err)
				}
			}
		})
	}
	scoped := NewScoped(t.Context(), store, user, ScopeFor(selected))
	defer scoped.Close()
	// Checkouts discovered after startup join the scope before ingestion.
	assertStatus(t, func() error { _, err := scoped.Get(t.Context(), other.ID); return err }(), 404)
	scoped.Select(linkedInput)
	if _, err := scoped.Get(t.Context(), other.ID); err != nil {
		t.Fatalf("selected checkout hidden: %v", err)
	}
	unscopedSelect := New(store, user)
	defer unscopedSelect.Close()
	unscopedSelect.Select(linkedInput)
	if count, err := unscopedSelect.Count(t.Context()); err != nil || count != 5 {
		t.Fatalf("select narrowed an unscoped service: %d %v", count, err)
	}
	if err := scoped.Delete(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	_, err = unscoped.Get(t.Context(), first.ID)
	assertStatus(t, err, 404)
}
