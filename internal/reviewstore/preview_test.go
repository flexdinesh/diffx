package reviewstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/review"
)

func TestObservationFilePreviewPreservesScopeIdentityAndFingerprint(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "preview-owner")
	request := observationRequest("source", "preview")
	binding, err := store.Ingest(owner.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	versions := make(map[review.DiffMode]string)
	for _, scope := range request.Scopes[:2] {
		mode := scope.Snapshot.Mode
		t.Run(string(mode), func(t *testing.T) {
			snapshot, err := store.ObservationSnapshot(owner.ID, binding.ContextID, mode)
			if err != nil {
				t.Fatal(err)
			}
			version, err := store.ObservationFileVersion(t.Context(), owner.ID, binding.ContextID, mode, "file")
			if err != nil || version.DiffID != snapshot.ID || version.VersionID != snapshot.VersionID || version.Fingerprint == nil || *version.Fingerprint != scope.Snapshot.Files[0].Fingerprint {
				t.Fatalf("file identity: %#v, %v", version, err)
			}
			versions[mode] = version.VersionID
			patch, err := store.ObservationPatch(t.Context(), owner.ID, binding.ContextID, mode, "file", *version.Fingerprint)
			if err != nil || !reflect.DeepEqual(patch, scope.Patches["file"]) {
				t.Fatalf("scope patch: %#v, %v", patch, err)
			}
			missing, err := store.ObservationFileVersion(t.Context(), owner.ID, binding.ContextID, mode, "missing")
			if err != nil || missing.DiffID != version.DiffID || missing.VersionID != version.VersionID || missing.Fingerprint != nil {
				t.Fatalf("missing file must retain scope identity: %#v, %v", missing, err)
			}
			if _, err := store.ObservationPatch(t.Context(), owner.ID, binding.ContextID, mode, "file", "wrong-fingerprint"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("wrong fingerprint returned patch: %v", err)
			}
		})
	}
	if binding.DiffIDs[review.DiffAll] == binding.DiffIDs[review.DiffStaged] || versions[review.DiffAll] == versions[review.DiffStaged] {
		t.Fatal("all and staged must have independent diff and version identities")
	}
	version, err := store.ObservationFileVersion(t.Context(), owner.ID, binding.ContextID, review.DiffUnstaged, "file")
	if err != nil || version.DiffID != binding.DiffIDs[review.DiffUnstaged] || version.VersionID == "" || version.Fingerprint != nil {
		t.Fatalf("empty scope must retain identity: %#v, %v", version, err)
	}
}

func TestObservationFilePreviewRejectsUnownedOrMissingResources(t *testing.T) {
	store := openTestStore(t)
	owner := testUser(t, store, "preview-owner")
	other := testUser(t, store, "other-owner")
	request := observationRequest("source", "preview")
	request.Scopes = request.Scopes[:2]
	binding, err := store.Ingest(owner.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, ownerID, contextID string
		mode                     review.DiffMode
	}{
		{"foreign owner", other.ID, binding.ContextID, review.DiffAll},
		{"missing owner", "missing", binding.ContextID, review.DiffAll},
		{"missing context", owner.ID, "missing", review.DiffAll},
		{"missing scope", owner.ID, binding.ContextID, review.DiffUnstaged},
	} {
		t.Run(item.name, func(t *testing.T) {
			if _, err := store.ObservationFileVersion(t.Context(), item.ownerID, item.contextID, item.mode, "file"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("file identity: %v", err)
			}
			if _, err := store.ObservationPatch(t.Context(), item.ownerID, item.contextID, item.mode, "file", "all-fingerprint"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("patch: %v", err)
			}
		})
	}
	if _, err := store.ObservationPatch(t.Context(), owner.ID, binding.ContextID, review.DiffAll, "missing", "all-fingerprint"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file returned patch: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ObservationFileVersion(cancelled, owner.ID, binding.ContextID, review.DiffAll, "file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled identity read: %v", err)
	}
	if _, err := store.ObservationPatch(cancelled, owner.ID, binding.ContextID, review.DiffAll, "file", "all-fingerprint"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled patch read: %v", err)
	}
}

func TestObservationFilePreviewRejectsExpiredAndDeletedContexts(t *testing.T) {
	for _, state := range []string{"expired", "deleted"} {
		t.Run(state, func(t *testing.T) {
			store := openTestStore(t)
			owner := testUser(t, store, "preview-owner")
			binding, err := store.Ingest(owner.ID, observationRequest("source", "preview"))
			if err != nil {
				t.Fatal(err)
			}
			if state == "expired" {
				_, err = store.db.Exec(`UPDATE diffs SET expires_at=? WHERE id IN (SELECT diff_id FROM observation_scopes WHERE context_id=?)`, time.Now().Add(-time.Second).UnixMilli(), binding.ContextID)
			} else {
				err = store.DeleteContext(owner.ID, binding.ContextID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.ObservationFileVersion(t.Context(), owner.ID, binding.ContextID, review.DiffAll, "file"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("file identity: %v", err)
			}
			if _, err := store.ObservationPatch(t.Context(), owner.ID, binding.ContextID, review.DiffAll, "file", "all-fingerprint"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("patch: %v", err)
			}
		})
	}
}
