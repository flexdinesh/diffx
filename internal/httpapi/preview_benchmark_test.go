package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/httpapi"
	"github.com/flexdinesh/diffx/internal/reviewstore"
)

// BenchmarkFilePreviews separates the cost of one preview from a complete
// browsing pass. Tiny patches expose work that scales with the file manifest.
func BenchmarkFilePreviews(b *testing.B) {
	for _, count := range []int{10, 100, 622} {
		b.Run(fmt.Sprintf("Files%d", count), func(b *testing.B) {
			var patch strings.Builder
			for index := range count {
				fmt.Fprintf(&patch, "diff --git a/file-%d.txt b/file-%d.txt\n--- a/file-%d.txt\n+++ b/file-%d.txt\n@@ -1 +1 @@\n-before\n+after\n", index, index, index, index)
			}
			input, err := collector.CollectPatch(b.Context(), patch.String(), "", collector.Options{SourceID: "benchmark", SubmissionID: "previews"})
			if err != nil {
				b.Fatal(err)
			}
			store, err := reviewstore.Open(filepath.Join(b.TempDir(), "state.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = store.Close() })
			user, err := store.User("benchmark", "benchmark")
			if err != nil {
				b.Fatal(err)
			}
			provider := contextservice.New(store, user)
			b.Cleanup(func() { _ = provider.Close() })
			submission, err := provider.Ingest(b.Context(), input)
			if err != nil {
				b.Fatal(err)
			}
			snapshot := submission.Snapshot
			if len(snapshot.Files) != count {
				b.Fatalf("collected %d files, want %d", len(snapshot.Files), count)
			}
			handler := httpapi.NewMulti(provider, store, fstest.MapFS{"index.html": {Data: []byte("web")}})
			requests := make([]*http.Request, 0, count)
			for _, file := range snapshot.Files {
				query := url.Values{"scope": {"all"}, "versionId": {snapshot.VersionID}, "fileVersion": {file.Fingerprint}}
				path := "/api/v2/contexts/" + submission.Context.ID + "/diffs/" + snapshot.ID + "/files/" + file.ID + "/patch?" + query.Encode()
				requests = append(requests, httptest.NewRequest(http.MethodGet, path, nil))
			}
			serve := func(b *testing.B, request *http.Request) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					b.Fatalf("preview status %d: %s", response.Code, response.Body.String())
				}
			}
			b.Run("OnePreview", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					serve(b, requests[0])
				}
			})
			b.Run("AllPreviews", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					for _, request := range requests {
						serve(b, request)
					}
				}
			})
		})
	}
}
