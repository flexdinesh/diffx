// Package storetest is the behavioral contract shared by every review store.
// It is test support: production packages must not import it.
package storetest

import (
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/reviewservice"
)

// Store is every shared content and review operation both review stores implement.
type Store interface {
	contextservice.Store
	reviewservice.CommentStore
	reviewservice.MutationStore
	reviewservice.CommitStore
	PruneExpired(time.Time) error
}

// Harness opens isolated stores and performs test-only adjustments no
// application operation may perform.
type Harness[S Store] struct {
	Open   func(t *testing.T, retention time.Duration) S
	Owner  func(t *testing.T, store S, name string) string             // creates an owner, returns its ID
	Expire func(t *testing.T, store S, contextID string, at time.Time) // sets expiry of every diff of the context (scopes and commits)
	// Reopen closes store and opens its database again with retention, so
	// durability and retention changes are observable.
	Reopen func(t *testing.T, store S, retention time.Duration) S
	// Submitted sets the context's last submission time, so a retry that
	// refreshes it is observable within one clock tick.
	Submitted func(t *testing.T, store S, contextID string, at time.Time)
}

// Run verifies every shared content and review rule against the harness's stores.
func Run[S Store](t *testing.T, harness Harness[S]) {
	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"ObservationRetryAndIndependentSources", harness.observationRetryAndIndependentSources},
		{"ObservationFreshnessUsesCollectionOrderAndRetries", harness.observationFreshnessUsesCollectionOrderAndRetries},
		{"ObservationFreshnessTracksDeduplicatedSubmissions", harness.observationFreshnessTracksDeduplicatedSubmissions},
		{"ObservationFreshnessKeepsStreamsSeparate", harness.observationFreshnessKeepsStreamsSeparate},
		{"ObservationFreshnessSurvivesPruningLatestSnapshot", harness.observationFreshnessSurvivesPruningLatestSnapshot},
		{"ObservationRollbackOnIncompleteScope", harness.observationRollbackOnIncompleteScope},
		{"ObservationReopensWithoutCheckoutWithinRetention", harness.observationReopensWithoutCheckoutWithinRetention},
		{"ObservationReviewScopesAndMetadataSearch", harness.observationReviewScopesAndMetadataSearch},
		{"ObservationDedupePreservesReviewAndRefreshesOnlyFreshSubmissions", harness.observationDedupePreservesReviewAndRefreshesOnlyFreshSubmissions},
		{"ObservationIdentitySeparatesProvenanceHeadAndContent", harness.observationIdentitySeparatesProvenanceHeadAndContent},
		{"EmptyObservationDedupeWithoutProducerHash", harness.emptyObservationDedupeWithoutProducerHash},
		{"ObservationDedupeConcurrentFreshSubmissions", harness.observationDedupeConcurrentFreshSubmissions},
		{"ObservationExpiryHidesReadsAndPrunesEveryScope", harness.observationExpiryHidesReadsAndPrunesEveryScope},
		{"ReusedBranchNameKeepsIndependentReviewAndFreshness", harness.reusedBranchNameKeepsIndependentReviewAndFreshness},
		{"ActivityProjectionPreservesHeadOrderAndObservationProvenance", harness.activityProjectionPreservesHeadOrderAndObservationProvenance},
		{"ObservationCommitsStoreAndRead", harness.observationCommitsStoreAndRead},
		{"ObservationCommitsExpireAndDeleteWithObservation", harness.observationCommitsExpireAndDeleteWithObservation},
		{"CapturedScopeCountsIncludeCancellingAndDistinctPaths", harness.capturedScopeCountsIncludeCancellingAndDistinctPaths},
		{"ExplicitComparisonKindsRemainDistinctAndOverrideLegacyInference", harness.explicitComparisonKindsRemainDistinctAndOverrideLegacyInference},
		{"NewScopesPersistReviewStateThroughDedupeLateArrivalAndRetention", harness.newScopesPersistReviewStateThroughDedupeLateArrivalAndRetention},
		{"DeleteLatestObservationKeepsOlderSnapshotsStale", harness.deleteLatestObservationKeepsOlderSnapshotsStale},
		{"DirectReplayCannotResurrectDeletedObservation", harness.directReplayCannotResurrectDeletedObservation},
		{"StreamReviewSurvivesSnapshotDeletionUntilStreamEnds", harness.streamReviewSurvivesSnapshotDeletionUntilStreamEnds},
		{"PruningLastSnapshotEndsStreamReview", harness.pruningLastSnapshotEndsStreamReview},
		{"PipedObservationsHaveContextReviews", harness.pipedObservationsHaveContextReviews},
		{"MarksCarryWhileFilePatchIsUnchanged", harness.marksCarryWhileFilePatchIsUnchanged},
		{"ReplaceCommentCannotResurrectDeletedComment", harness.replaceCommentCannotResurrectDeletedComment},
		{"CancelledCommentWriteStopsWithoutChange", harness.cancelledCommentWriteStopsWithoutChange},
		{"ObservationFilePreviewPreservesScopeIdentityAndFingerprint", harness.observationFilePreviewPreservesScopeIdentityAndFingerprint},
		{"ObservationFilePreviewRejectsUnownedOrMissingResources", harness.observationFilePreviewRejectsUnownedOrMissingResources},
		{"ObservationFilePreviewRejectsExpiredAndDeletedContexts", harness.observationFilePreviewRejectsExpiredAndDeletedContexts},
		{"PullRequestProjectionFollowsWinningSubmission", harness.pullRequestProjectionFollowsWinningSubmission},
		{"SessionAssociationsReuseReviewAndFilterEveryCollection", harness.sessionAssociationsReuseReviewAndFilterEveryCollection},
		{"SessionNameUpdatesByCollectionOrderAndSource", harness.sessionNameUpdatesByCollectionOrderAndSource},
		{"ComparisonPoliciesHaveSeparateReviewsAndDurableHeads", harness.comparisonPoliciesHaveSeparateReviewsAndDurableHeads},
		{"RetentionConfigurationPreservesExistingExpiryAndReplay", harness.retentionConfigurationPreservesExistingExpiryAndReplay},
		{"ContainerRunWithoutHarnessRemainsSearchableWithoutSession", harness.containerRunWithoutHarnessRemainsSearchableWithoutSession},
	} {
		t.Run(test.name, test.run)
	}
}

// retention is the expiry of stores opened by open.
const retention = 7 * 24 * time.Hour

func (harness Harness[S]) open(t *testing.T) S {
	t.Helper()
	return harness.Open(t, retention)
}

func ingest(t *testing.T, store Store, ownerID string, request ingestion.Request) (reviewdata.Binding, error) {
	return store.IngestContext(t.Context(), ownerID, request)
}

func observationRequest(source, submission string) ingestion.Request {
	return ingestion.Request{
		ProtocolVersion: ingestion.ProtocolVersion, SubmissionID: submission,
		Metadata: ingestion.Metadata{SourceID: source, Hostname: "agent-host", RepositoryKey: "repo-key", RepositoryName: "my-repo", CheckoutKey: "checkout", Root: "/deleted/checkout", WorktreeName: "sandbox", Branch: "feature", CollectedAt: 123, Trigger: "agent-hook"},
		Scopes: []ingestion.Scope{
			{Snapshot: review.RepositoryDiff{Mode: review.DiffAll, Revision: "all-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "all-fingerprint"}}}, Patches: map[string]review.FilePatch{"file": {Patch: "all-patch", Contents: &review.FileContents{Before: "old", After: "new"}}}},
			{Snapshot: review.RepositoryDiff{Mode: review.DiffStaged, Revision: "staged-revision", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "staged-fingerprint"}}}, Patches: map[string]review.FilePatch{"file": {Patch: "staged-patch"}}},
			{Snapshot: review.RepositoryDiff{Mode: review.DiffUnstaged, Revision: "unstaged-revision", Files: []review.ChangedFile{}}, Patches: map[string]review.FilePatch{}},
		},
	}
}

// localRequest is an observation whose snapshots carry local checkout provenance.
func localRequest(id string) ingestion.Request {
	request := observationRequest("source", id)
	for i := range request.Scopes {
		request.Scopes[i].Snapshot.Source = "local"
		request.Scopes[i].Snapshot.Root = request.Metadata.Root
		request.Scopes[i].Snapshot.Branch = request.Metadata.Branch
	}
	return request
}
