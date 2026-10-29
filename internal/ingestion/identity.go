package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// ContentIdentity preserves the established observation identity algorithm.
// Unknown full content identities remain independent unless all scopes are empty.
func ContentIdentity(request Request) (string, error) {
	if len(request.Scopes) == 0 {
		return "", errors.New("observation scopes required")
	}
	kinds := make([]string, 0, len(request.Scopes))
	modes := make([]string, 0, len(request.Scopes))
	for _, scope := range request.Scopes {
		if scope.Snapshot.ComparisonKind != "" {
			kinds = append(kinds, string(scope.Snapshot.Mode)+":"+string(scope.Snapshot.ComparisonKind))
		}
		modes = append(modes, string(scope.Snapshot.Mode))
		if request.ContentHash == "" && len(scope.Snapshot.Files) != 0 {
			return "", nil
		}
	}
	sort.Strings(modes)
	metadata := request.Metadata
	parts := []any{metadata.SourceID, metadata.RepositoryKey, metadata.CheckoutKey, BranchIdentity(metadata), metadata.Head, request.Scopes[0].Snapshot.Source, modes, request.ContentHash}
	// Keep legacy HEAD identity hashes; branch policies have separate reviews.
	if policy := ComparisonPolicy(metadata); policy != "" {
		parts = append(parts, policy)
	}
	// Omitted kinds preserve protocol 4 identity, including already queued requests.
	if len(kinds) > 0 {
		sort.Strings(kinds)
		parts = append(parts, kinds)
	}
	// Captured commits distinguish an observation from one collected without
	// them; omitting them preserves earlier identities.
	if len(request.Commits) > 0 {
		commits := make([]string, 0, len(request.Commits))
		for _, commit := range request.Commits {
			commits = append(commits, commit.OID)
		}
		parts = append(parts, map[string]any{"commits": commits, "total": request.CommitsTotal})
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ComparisonPolicy identifies a stream independently of resolved commit tips.
func ComparisonPolicy(metadata Metadata) string {
	if metadata.Comparison == nil || metadata.Comparison.Kind == "working-tree" {
		return ""
	}
	return metadata.Comparison.Kind + ":" + metadata.Comparison.BaseRef
}

// BranchIdentity is the stable branch of a stream: a producer's branch ID
// survives renames; otherwise the branch label identifies it.
func BranchIdentity(metadata Metadata) string {
	if metadata.BranchID != "" {
		return metadata.BranchID
	}
	return metadata.Branch
}

// StreamID names one stream head without exposing its keys or depending on row identity.
func StreamID(sourceID, repositoryKey, checkoutKey, branch, comparisonPolicy string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{sourceID, repositoryKey, checkoutKey, branch, comparisonPolicy}, "\x00")))
	return hex.EncodeToString(digest[:12])
}

// StreamKey names the review stream of a new observation. Keyed checkouts share
// their stream head identity; piped observations form a stream of their own.
func StreamKey(contextID string, metadata Metadata) string {
	if metadata.RepositoryKey == "" || metadata.CheckoutKey == "" {
		return "context:" + contextID
	}
	return StreamID(metadata.SourceID, metadata.RepositoryKey, metadata.CheckoutKey, BranchIdentity(metadata), ComparisonPolicy(metadata))
}

// Payload is a request's stored representation and its replay hash. A replay
// under the same submission identity must hash identically.
func Payload(request Request) ([]byte, string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
