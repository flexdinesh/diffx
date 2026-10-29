package reviewstore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

// The shared contract checks retention through reads; rows prove every scope
// diff carries the refreshed expiry.
func TestRetentionConfigurationRefreshesEveryScopeDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user := testUser(t, store, "retention")
	request := observationRequest("source", "original")
	request.ProtocolVersion = ingestion.ProtocolVersion
	request.ContentHash = strings.Repeat("a", 64)
	binding, err := store.Ingest(user.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	duration := 2 * 24 * time.Hour
	store, err = OpenWithRetention(path, duration)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request.SubmissionID = "fresh"
	if _, err := store.Ingest(user.ID, request); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.Context(t.Context(), user.ID, binding.ContextID, time.Now())
	if err != nil || *refreshed.ExpiresAt-refreshed.LastSubmittedAt != duration.Milliseconds() {
		t.Fatalf("fresh retention: %#v %v", refreshed, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM observation_scopes s JOIN diffs d ON d.id=s.diff_id WHERE s.context_id=? AND d.expires_at=?`, binding.ContextID, *refreshed.ExpiresAt).Scan(&count); err != nil || count != 3 {
		t.Fatalf("scope expiry: %d %v", count, err)
	}
	if _, err := OpenWithRetention(":memory:", 0); err == nil {
		t.Fatal("nonpositive retention accepted")
	}
}
