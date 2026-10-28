package httpapi

import (
	"context"
	"fmt"
	"github.com/flexdinesh/diffx/internal/testsupport/storetest"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewservice"
)

func TestStoredPreviewReferences(t *testing.T) {
	raw := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
	input, err := collector.CollectPatch(t.Context(), raw, "", collector.Options{SourceID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	// Both scopes share a file identity but have independent stored versions.
	input.Metadata.RepositoryKey, input.Metadata.CheckoutKey = "repo", "checkout"
	input.Scopes[0].Snapshot.Source = "local"
	input.Scopes[0].Snapshot.ComparisonKind = review.ComparisonUncommitted
	input.Metadata.Root = input.Scopes[0].Snapshot.Root
	input.Metadata.Branch = input.Scopes[0].Snapshot.Branch
	input.Metadata.Head = input.Scopes[0].Snapshot.Head
	staged := input.Scopes[0]
	staged.Snapshot.Mode = review.DiffStaged
	staged.Snapshot.ComparisonKind = review.ComparisonStaged
	file := staged.Snapshot.Files[0]
	staged.Patches = map[string]review.FilePatch{file.ID: {Patch: raw}}
	preview := input.Scopes[0].Patches[file.ID]
	preview.Contents = &review.FileContents{Before: "old\n", After: "new\n"}
	preview.Message = nil
	input.Scopes[0].Patches[file.ID] = preview
	input.Scopes = append(input.Scopes, staged)
	store, err := storetest.Open(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, err := store.User("preview", "preview")
	if err != nil {
		t.Fatal(err)
	}
	provider := contextservice.New(store, user)
	t.Cleanup(func() { _ = provider.Close() })
	submitted, err := provider.Ingest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	active, err := provider.Resolve(t.Context(), submitted.Context.ID)
	if err != nil {
		t.Fatal(err)
	}
	stagedSnapshot, err := active.Source.Snapshot(t.Context(), review.DiffStaged)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := submitted.Snapshot
	handler := newMulti(provider, store, nil)
	for _, test := range []struct {
		name, diffID, versionID, fileID, fingerprint, scope, resource string
		status                                                        int
	}{
		{"patch", snapshot.ID, snapshot.VersionID, file.ID, file.Fingerprint, "all", "patch", 200},
		{"contents", snapshot.ID, snapshot.VersionID, file.ID, file.Fingerprint, "all", "contents", 200},
		{"staged patch", stagedSnapshot.ID, stagedSnapshot.VersionID, file.ID, file.Fingerprint, "staged", "patch", 200},
		{"uncollected contents", stagedSnapshot.ID, stagedSnapshot.VersionID, file.ID, file.Fingerprint, "staged", "contents", 404},
		{"stale scope before missing file", snapshot.ID, "stale", "missing", "", "all", "patch", 409},
		{"different scope before missing file", snapshot.ID, snapshot.VersionID, "missing", "", "staged", "patch", 409},
		{"missing file", snapshot.ID, snapshot.VersionID, "missing", "", "all", "patch", 404},
		{"stale file", snapshot.ID, snapshot.VersionID, file.ID, "stale", "all", "patch", 409},
		{"stale contents", snapshot.ID, snapshot.VersionID, file.ID, "stale", "all", "contents", 409},
		{"missing version", snapshot.ID, "", file.ID, file.Fingerprint, "all", "patch", 400},
		{"foreign diff", "foreign", snapshot.VersionID, file.ID, file.Fingerprint, "all", "patch", 404},
		{"unsupported scope", snapshot.ID, snapshot.VersionID, file.ID, file.Fingerprint, "unstaged", "patch", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := url.Values{"scope": {test.scope}, "versionId": {test.versionID}, "fileVersion": {test.fingerprint}}
			path := fmt.Sprintf("/api/v2/contexts/%s/diffs/%s/files/%s/%s?%s", submitted.Context.ID, test.diffID, test.fileID, test.resource, query.Encode())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != test.status {
				t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if test.name == "patch" {
				got := decode[review.FilePatch](t, response.Result())
				if !reflect.DeepEqual(got, input.Scopes[0].Patches[file.ID]) {
					t.Fatalf("patch = %#v", got)
				}
			}
			if test.name == "contents" {
				got := decode[review.FileContents](t, response.Result())
				if got.Before != "old\n" || got.After != "new\n" {
					t.Fatalf("contents = %#v", got)
				}
			}
		})
	}
	catalog := reviewservice.NewCatalog(provider, store)
	for index, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged} {
		patch, err := catalog.GetPatch(t.Context(), submitted.Context.ID, mode, file.ID)
		if err != nil || !reflect.DeepEqual(patch, input.Scopes[index].Patches[file.ID]) {
			t.Fatalf("catalog patch %s = %#v, %v", mode, patch, err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reviewservice.New(active, store).CurrentPatch(cancelled, review.DiffAll, snapshot.ID, snapshot.VersionID, file.ID, file.Fingerprint); err != context.Canceled {
		t.Fatalf("cancelled preview: %v", err)
	}
	// Direct source access must reject a forged fingerprint too.
	forged := file
	forged.Fingerprint = "forged"
	if _, err := active.Source.Patch(t.Context(), review.DiffAll, forged, nil); err == nil {
		t.Fatal("forged file fingerprint accepted")
	}
	if err := provider.Delete(t.Context(), submitted.Context.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.GetPatch(t.Context(), submitted.Context.ID, review.DiffAll, file.ID); err == nil {
		t.Fatal("deleted preview returned")
	}
}
