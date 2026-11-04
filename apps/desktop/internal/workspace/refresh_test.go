package workspace

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func TestHealthySSERefreshesSharedReviewWithoutCollectionOrNavigation(t *testing.T) {
	var revision atomic.Int32
	c, f, n := newFixture(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/comments") && revision.Load() > 0 {
			jsonResponse(w, 200, map[string]any{"comments": []goclient.ReviewComment{{Id: "external-comment", Body: "changed in web", Scope: "uncommitted", Status: "resolved"}}})
			return true
		}
		return false
	})
	before := waitState(t, c, n, func(s State) bool { return loaded(s) && s.Connected })
	revision.Store(1)
	// No ingestion event or reconnect: comments have neither. The production
	// refresh cadence must observe another client's mutation itself.
	after := waitState(t, c, n, func(s State) bool {
		return len(s.Comments) == 1 && s.Comments[0].Id == "external-comment"
	})
	if after.Current.Id != before.Current.Id || after.Diff.VersionId != before.Diff.VersionId || !after.Connected {
		t.Fatal("review refresh changed selected snapshot or SSE connection")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.collectionCalls) != 0 {
		t.Fatal("review refresh collected Git")
	}
}

func TestReviewRefreshCoalescesWhileReadIsInFlight(t *testing.T) {
	var block atomic.Bool
	var reads atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	c, _, n := newFixture(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/comments") || !block.Load() {
			return false
		}
		if reads.Add(1) == 1 {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		jsonResponse(w, 200, map[string]any{"comments": []goclient.ReviewComment{{Id: "refreshed", Body: "fresh"}}})
		return true
	})
	waitState(t, c, n, loaded)
	block.Store(true)
	c.reloadReview()
	<-entered
	for range 20 {
		c.reloadReview()
	}
	if reads.Load() != 1 {
		t.Fatal("parallel review reads were admitted")
	}
	close(release)
	waitState(t, c, n, func(s State) bool { return reads.Load() == 2 && len(s.Comments) > 0 && s.Comments[0].Id == "refreshed" })
	c.Close()
	if got := reads.Load(); got != 2 {
		t.Fatalf("coalesced requests=%d, want 2", got)
	}
}
