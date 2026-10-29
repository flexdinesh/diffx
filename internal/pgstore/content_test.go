package pgstore_test

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
	"github.com/jackc/pgx/v5"
)

// replicas opens stores sharing one database, as server replicas do, and an owner.
func replicas(t *testing.T, count int) ([]*pgstore.Store, string, string) {
	t.Helper()
	url := pgtest.URL(t)
	stores := make([]*pgstore.Store, 0, count)
	for range count {
		store, err := pgstore.Open(t.Context(), url, pgstore.DefaultRetention)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		stores = append(stores, store)
	}
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	owner := rand.Text()
	if _, err := conn.Exec(t.Context(), `INSERT INTO diffx.users(id, name, created_at) VALUES ($1, 'owner', now())`, owner); err != nil {
		t.Fatal(err)
	}
	return stores, owner, url
}

func request(submission, content string) ingestion.Request {
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission, ContentHash: strings.Repeat(content, 64),
		Metadata: ingestion.Metadata{SourceID: "source", Hostname: "host", RepositoryKey: "repo", RepositoryName: "repo", CheckoutKey: "checkout", Root: "/checkout", Branch: "main", CollectedAt: 100, Trigger: "manual"},
		Scopes: []ingestion.Scope{{
			Snapshot: review.RepositoryDiff{Mode: review.DiffAll, Revision: content, Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: content}}},
			Patches:  map[string]review.FilePatch{"file": {Patch: content}},
		}},
	}
}

func TestReplicasReuseOneObservationForConcurrentFreshSubmissions(t *testing.T) {
	t.Parallel()
	stores, owner, _ := replicas(t, 4)
	ids := make(chan string, 16)
	var group sync.WaitGroup
	for i := range 16 {
		group.Go(func() {
			// Half are retries of another goroutine's submission.
			binding, err := stores[i%len(stores)].IngestContext(t.Context(), owner, request(string(rune('a'+i/2)), "a"))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- binding.ContextID
		})
	}
	group.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first != "" && id != first {
			t.Fatalf("replicas published duplicates %s and %s", first, id)
		}
		first = id
	}
	items, err := stores[0].Contexts(t.Context(), owner, 10, 0, "", time.Now())
	if err != nil || len(items) != 1 {
		t.Fatalf("catalog: %d %v", len(items), err)
	}
}

// A stream gaining an observation while its only other observation is deleted
// or pruned keeps a review for the new observation, whichever commits first.
func TestStreamReviewSurvivesConcurrentEnd(t *testing.T) {
	t.Parallel()
	for _, end := range []string{"delete", "prune"} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()
			stores, owner, url := replicas(t, 2)
			for round := range 8 {
				suffix := end + string(rune('a'+round))
				old, err := stores[0].IngestContext(t.Context(), owner, request("old-"+suffix, "b"))
				if err != nil {
					t.Fatal(err)
				}
				if end == "prune" {
					conn, err := pgx.Connect(t.Context(), url)
					if err != nil {
						t.Fatal(err)
					}
					_, err = conn.Exec(t.Context(), `UPDATE diffx.diffs SET expires_at=now() - interval '1 second' WHERE id IN (SELECT diff_id FROM diffx.observation_scopes WHERE context_id=$1)`, old.ContextID)
					conn.Close(context.Background())
					if err != nil {
						t.Fatal(err)
					}
				}
				var group sync.WaitGroup
				var fresh string
				group.Go(func() {
					binding, err := stores[1].IngestContext(t.Context(), owner, request("new-"+suffix, "c"))
					if err != nil {
						t.Error(err)
					}
					fresh = binding.ContextID
				})
				group.Go(func() {
					var err error
					if end == "delete" {
						err = stores[0].DeleteContext(t.Context(), owner, old.ContextID)
					} else {
						err = stores[0].PruneExpired(time.Now())
					}
					if err != nil {
						t.Error(err)
					}
				})
				group.Wait()
				if t.Failed() {
					return
				}
				comment := review.ReviewComment{ID: "comment-" + suffix, Scope: review.DiffAll, Path: "file.txt", Side: "additions", Start: 1, End: 1, Body: "check", Status: "open", CreatedAt: 1}
				if err := stores[0].PutComment(t.Context(), fresh, comment); err != nil {
					t.Fatalf("round %d: new observation lost its review: %v", round, err)
				}
				if err := stores[0].DeleteContext(t.Context(), owner, fresh); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNULIsRejectedInTextButKeptInPreviews(t *testing.T) {
	t.Parallel()
	stores, owner, _ := replicas(t, 1)
	store := stores[0]
	rejected := request("nul", "d")
	rejected.Metadata.Hostname = "host\x00name"
	if _, err := store.IngestContext(t.Context(), owner, rejected); !errors.Is(err, pgstore.ErrNUL) || !review.Permanent(err) {
		t.Fatalf("NUL metadata = %v, want permanent rejection", err)
	}
	// An escaped backslash before u0000 is ordinary text.
	escaped := request("escaped", "e")
	escaped.Metadata.Hostname = `host\u0000`
	accepted := request("binary", "f")
	accepted.Scopes[0].Patches["file"] = review.FilePatch{Patch: "a\x00b", Contents: &review.FileContents{Before: "\x00", After: "x"}}
	for _, input := range []ingestion.Request{escaped, accepted} {
		binding, err := store.IngestContext(t.Context(), owner, input)
		if err != nil {
			t.Fatal(err)
		}
		patch, err := store.ObservationPatch(t.Context(), owner, binding.ContextID, review.DiffAll, "file", input.Scopes[0].Snapshot.Files[0].Fingerprint)
		if err != nil || !reflect.DeepEqual(patch, input.Scopes[0].Patches["file"]) {
			t.Fatalf("patch %#v, %v", patch, err)
		}
		item, err := store.Context(t.Context(), owner, binding.ContextID, time.Now())
		if err != nil || item.Metadata == nil || item.Metadata.Hostname != input.Metadata.Hostname {
			t.Fatalf("metadata %#v, %v", item.Metadata, err)
		}
	}
}
