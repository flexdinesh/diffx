package contextservice

import (
	"reflect"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestPublicationNormalizesLegacyManifestAndCapturedCapabilities(t *testing.T) {
	for _, test := range []struct {
		name       string
		additional review.DiffMode
		kind       review.ComparisonKind
		staging    bool
	}{
		{"legacy objects", "", "", false},
		{"legacy checkout", review.DiffStaged, "", true},
		{"two non-staging scopes", review.DiffCommitted, review.ComparisonCommitted, false},
		{"uncommitted without staging views", review.DiffUncommitted, review.ComparisonUncommitted, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := testService(t)
			head := strings.Repeat("c", 40)
			input := ingestion.Request{ProtocolVersion: 4, SubmissionID: "capture", Metadata: ingestion.Metadata{
				SourceID: "source", RepositoryKey: "repo", CheckoutKey: "checkout", Root: "/absent/checkout", Trigger: "manual", CollectedAt: 1, Head: &head,
				Comparison: &ingestion.Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)},
			}, Scopes: []ingestion.Scope{{Snapshot: review.RepositoryDiff{Source: "local", Root: "/absent/checkout", Head: &head, Mode: review.DiffAll, Revision: "revision", Files: []review.ChangedFile{}}, Patches: map[string]review.FilePatch{}}}}
			if test.additional != "" {
				scope := input.Scopes[0]
				scope.Snapshot.Mode, scope.Snapshot.ComparisonKind = test.additional, test.kind
				input.Scopes = append(input.Scopes, scope)
			}
			publication, err := service.Ingest(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			want := review.ComparisonCommitted
			if test.staging {
				want = review.ComparisonCombined
			}
			if publication.Snapshot.ComparisonKind != want || publication.Context.ScopeSummaries[0].ComparisonKind != want {
				t.Fatalf("publication not normalized: %+v", publication)
			}
			active, err := service.Resolve(t.Context(), publication.Context.ID)
			if err != nil {
				t.Fatal(err)
			}
			support := active.Source.Support()
			if len(support.Scopes) != len(input.Scopes) || support.StagingMetadata != test.staging {
				t.Fatalf("captured support: %+v", support)
			}
			if !reflect.DeepEqual(active.Capabilities, publication.Context.Capabilities) {
				t.Fatalf("capabilities differ: %+v, %+v", active.Capabilities, publication.Context.Capabilities)
			}
			if input.Scopes[0].Snapshot.ComparisonKind != "" {
				t.Fatal("publication mutated request")
			}
		})
	}
}
