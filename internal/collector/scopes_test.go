package collector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func scopeFor(t *testing.T, request ingestion.Request, mode review.DiffMode) ingestion.Scope {
	t.Helper()
	for _, scope := range request.Scopes {
		if scope.Snapshot.Mode == mode {
			return scope
		}
	}
	t.Fatalf("missing scope %s", mode)
	return ingestion.Scope{}
}

func TestCapturedScopeKindsFollowSourceEndpoints(t *testing.T) {
	root := branchRepository(t)
	for _, test := range []struct {
		name, base, branch string
		kinds              map[review.DiffMode]review.ComparisonKind
	}{
		{"branch checkout", "auto", "", map[review.DiffMode]review.ComparisonKind{
			review.DiffAll: review.ComparisonCombined, review.DiffUncommitted: review.ComparisonUncommitted,
			review.DiffCommitted: review.ComparisonCommitted, review.DiffStaged: review.ComparisonStaged, review.DiffUnstaged: review.ComparisonUnstaged,
		}},
		{"HEAD checkout", "HEAD", "", map[review.DiffMode]review.ComparisonKind{
			review.DiffAll: review.ComparisonUncommitted, review.DiffStaged: review.ComparisonStaged, review.DiffUnstaged: review.ComparisonUnstaged,
		}},
		{"objects only", "auto", "feature", map[review.DiffMode]review.ComparisonKind{review.DiffAll: review.ComparisonCommitted}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := Collect(t.Context(), root, Options{SourceID: "machine", Base: test.base, Branch: test.branch})
			if err != nil {
				t.Fatal(err)
			}
			if len(request.Scopes) != len(test.kinds) {
				t.Fatalf("scope count %d, want %d", len(request.Scopes), len(test.kinds))
			}
			for mode, kind := range test.kinds {
				if got := scopeFor(t, request, mode).Snapshot.ComparisonKind; got != kind {
					t.Fatalf("%s kind %q, want %q", mode, got, kind)
				}
			}
		})
	}
	raw := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+new\n"
	piped, err := CollectPatch(t.Context(), raw, "", Options{SourceID: "machine"})
	if err != nil || len(piped.Scopes) != 1 || piped.Scopes[0].Snapshot.ComparisonKind != review.ComparisonPatch {
		t.Fatalf("piped scopes: %+v, %v", piped.Scopes, err)
	}
}

func TestBranchScopesPreserveCancellingChanges(t *testing.T) {
	for _, after := range []string{"original\n", "committed\n"} {
		t.Run(strings.TrimSpace(after), func(t *testing.T) {
			root := branchRepository(t)
			write(t, root, "tracked", "staged\n")
			git(t, root, "add", "tracked")
			write(t, root, "tracked", after)
			request := collect(t, root)
			empty := review.DiffAll
			if after == "committed\n" {
				empty = review.DiffUncommitted
			}
			if files := scopeFor(t, request, empty).Snapshot.Files; len(files) != 0 {
				t.Fatalf("cancelled %s changes remain: %+v", empty, files)
			}
			for _, want := range []struct {
				mode          review.DiffMode
				before, after string
			}{
				{review.DiffCommitted, "original\n", "committed\n"},
				{review.DiffStaged, "committed\n", "staged\n"},
				{review.DiffUnstaged, "staged\n", after},
			} {
				patch := patchFor(t, request, want.mode, "tracked")
				if patch.Contents == nil || patch.Contents.Before != want.before || patch.Contents.After != want.after || !strings.Contains(patch.Patch, "-"+want.before) || !strings.Contains(patch.Patch, "+"+want.after) {
					t.Fatalf("%s endpoints: %+v", want.mode, patch)
				}
			}
		})
	}
}

func TestUncommittedRecreatedAndUntrackedContents(t *testing.T) {
	root := branchRepository(t)
	git(t, root, "rm", "tracked")
	write(t, root, "tracked", "recreated\n")
	write(t, root, "untracked", "new\n")
	request := collect(t, root)
	for _, want := range []struct {
		mode                review.DiffMode
		name, before, after string
	}{
		{review.DiffAll, "tracked", "original\n", "recreated\n"},
		{review.DiffUncommitted, "tracked", "committed\n", "recreated\n"},
		{review.DiffCommitted, "tracked", "original\n", "committed\n"},
		{review.DiffStaged, "tracked", "committed\n", ""},
		{review.DiffUnstaged, "tracked", "", "recreated\n"},
		{review.DiffUncommitted, "untracked", "", "new\n"},
	} {
		patch := patchFor(t, request, want.mode, want.name)
		if patch.Contents == nil || patch.Contents.Before != want.before || patch.Contents.After != want.after {
			t.Fatalf("%s %s contents: %+v", want.mode, want.name, patch.Contents)
		}
	}
	for _, mode := range []review.DiffMode{review.DiffCommitted, review.DiffStaged} {
		if files := scopeFor(t, request, mode).Snapshot.Files; len(files) != 1 || files[0].Path != "tracked" {
			t.Fatalf("untracked included in %s: %+v", mode, files)
		}
	}
}

type changingScopeSource struct {
	diffsource.Source
	mode review.DiffMode
	seen bool
}

func (source *changingScopeSource) Snapshot(ctx context.Context, mode review.DiffMode) (review.RepositoryDiff, error) {
	snapshot, err := source.Source.Snapshot(ctx, mode)
	if mode == source.mode {
		if source.seen {
			snapshot.Revision = "changed"
		}
		source.seen = true
	}
	return snapshot, err
}

func TestBranchCollectionChecksEveryScopeForConsistency(t *testing.T) {
	root := branchRepository(t)
	source, err := diffsource.OpenComparison(t.Context(), root, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range source.Support().Scopes {
		t.Run(string(mode), func(t *testing.T) {
			_, err := collectRepository(t.Context(), &changingScopeSource{Source: source, mode: mode}, Options{SourceID: "machine"})
			if !errors.Is(err, errChanged) {
				t.Fatalf("accepted inconsistent %s: %v", mode, err)
			}
		})
	}
}

func TestFingerprintIncludesOnlyPresentComparisonDescriptors(t *testing.T) {
	request := ingestion.Request{ProtocolVersion: 4, ContentHash: "full-content", Scopes: []ingestion.Scope{{Snapshot: review.RepositoryDiff{Source: "local", Mode: review.DiffAll}}}}
	legacy := Fingerprint(request)
	// Protocol 4 fingerprint before comparison descriptors were introduced.
	if legacy != "7130db0ab937178fa6a07ce16446d1bb44f7466c756731368de4e369d9dd7927" {
		t.Fatalf("legacy fingerprint changed: %s", legacy)
	}
	request.Scopes[0].Snapshot.ComparisonKind = review.ComparisonCombined
	combined := Fingerprint(request)
	request.Scopes[0].Snapshot.ComparisonKind = review.ComparisonUncommitted
	uncommitted := Fingerprint(request)
	if legacy == "" || legacy == combined || combined == uncommitted || legacy == uncommitted {
		t.Fatal("comparison descriptors omitted from fingerprint")
	}
	request.Scopes[0].Snapshot.ComparisonKind = ""
	if Fingerprint(request) != legacy {
		t.Fatal("legacy fingerprint changed after omitting descriptor")
	}
}
