package ingestion

import (
	"slices"

	"github.com/flexdinesh/diffx/internal/review"
)

// LegacyComparisonKind interprets captured scopes without modifying their stored
// manifests or request identities. Explicit manifest kinds take precedence at reads.
func LegacyComparisonKind(mode review.DiffMode, source string, metadata Metadata, scopes []review.DiffMode) review.ComparisonKind {
	if source == "stdin" {
		return review.ComparisonPatch
	}
	switch mode {
	case review.DiffStaged:
		return review.ComparisonStaged
	case review.DiffUnstaged:
		return review.ComparisonUnstaged
	case review.DiffUncommitted:
		return review.ComparisonUncommitted
	case review.DiffCommitted:
		return review.ComparisonCommitted
	default:
		if metadata.Comparison != nil && metadata.Comparison.Kind == "branch" {
			if slices.Contains(scopes, review.DiffStaged) || slices.Contains(scopes, review.DiffUnstaged) {
				return review.ComparisonCombined
			}
			return review.ComparisonCommitted
		}
		return review.ComparisonUncommitted
	}
}
