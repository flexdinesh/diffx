package ingestion

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

func Validate(input Request) error {
	if input.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported ingestion protocol %d", input.ProtocolVersion)
	}
	if strings.TrimSpace(input.SubmissionID) == "" || len(input.SubmissionID) > 128 {
		return fmt.Errorf("submissionId required; maximum 128 bytes")
	}
	if input.ContentHash != "" {
		if len(input.ContentHash) != 64 || input.ContentHash != strings.ToLower(input.ContentHash) {
			return fmt.Errorf("contentHash must be a lowercase SHA256 hex digest")
		}
		if _, err := hex.DecodeString(input.ContentHash); err != nil {
			return fmt.Errorf("contentHash must be a lowercase SHA256 hex digest")
		}
	}
	if strings.TrimSpace(input.Metadata.SourceID) == "" || len(input.Metadata.SourceID) > 256 || input.Metadata.CollectedAt <= 0 {
		return fmt.Errorf("sourceId and positive collectedAt required")
	}
	if input.Metadata.ActivityAt < 0 {
		return fmt.Errorf("activityAt must be nonnegative")
	}
	if input.Metadata.Trigger != "manual" && input.Metadata.Trigger != "agent-hook" && input.Metadata.Trigger != "pipe" && input.Metadata.Trigger != GitHubTrigger {
		return fmt.Errorf("trigger must be manual, agent-hook, pipe, or github")
	}
	if err := validatePullRequest(input.Metadata); err != nil {
		return err
	}
	values := []string{input.Metadata.Hostname, input.Metadata.RunID, input.Metadata.Agent, input.Metadata.RepositoryKey, input.Metadata.RepositoryName, input.Metadata.RemoteURL, input.Metadata.CheckoutKey, input.Metadata.Root, input.Metadata.WorktreeName, input.Metadata.Branch, input.Metadata.BranchID, input.Metadata.CollectorVersion, input.Metadata.TriggerRoot}
	if comparison := input.Metadata.Comparison; comparison != nil {
		if comparison.Kind != "working-tree" && comparison.Kind != "branch" {
			return fmt.Errorf("comparison kind must be working-tree or branch")
		}
		if strings.TrimSpace(comparison.BaseRef) == "" {
			return fmt.Errorf("comparison baseRef required")
		}
		if comparison.Kind == "working-tree" && comparison.BaseRef != "HEAD" {
			return fmt.Errorf("working-tree comparison requires HEAD baseline")
		}
		if comparison.Kind == "branch" && (comparison.BaseCommit == "" || comparison.MergeBase == "") {
			return fmt.Errorf("branch comparison requires baseCommit and mergeBase")
		}
		for _, commit := range []string{comparison.BaseCommit, comparison.MergeBase} {
			if commit != "" && !validCommit(commit) {
				return fmt.Errorf("comparison commits must be lowercase Git object IDs")
			}
		}
		values = append(values, comparison.BaseRef)
	}
	if session := input.Metadata.AgentSession; session != nil {
		if strings.TrimSpace(session.Harness) == "" || strings.TrimSpace(session.ID) == "" {
			return fmt.Errorf("agentSession harness and id required")
		}
		values = append(values, session.Harness, session.ID, session.Name)
	}
	for _, value := range values {
		if len(value) > 32<<10 {
			return fmt.Errorf("metadata value exceeds 32 KiB")
		}
	}
	if len(input.Scopes) < 1 || len(input.Scopes) > 5 {
		return fmt.Errorf("one to five scopes required")
	}
	seen := make(map[review.DiffMode]bool)
	for _, scope := range input.Scopes {
		if _, err := review.ParseDiffMode(string(scope.Snapshot.Mode)); err != nil || seen[scope.Snapshot.Mode] {
			return fmt.Errorf("invalid or repeated scope")
		}
		seen[scope.Snapshot.Mode] = true
		kind := scope.Snapshot.ComparisonKind
		newScope := scope.Snapshot.Mode == review.DiffUncommitted || scope.Snapshot.Mode == review.DiffCommitted
		if newScope && kind == "" {
			return fmt.Errorf("comparisonKind required for %s scope", scope.Snapshot.Mode)
		}
		if kind != "" {
			if err := validateComparisonKind(scope.Snapshot, input.Metadata); err != nil {
				return err
			}
		}
		if scope.Snapshot.Source != "local" && scope.Snapshot.Source != "stdin" {
			return fmt.Errorf("snapshot source must be local or stdin")
		}
		if scope.Snapshot.Source == "local" && (input.Metadata.RepositoryKey == "" || input.Metadata.CheckoutKey == "") {
			return fmt.Errorf("Git snapshots require repositoryKey and checkoutKey")
		}
		if scope.Snapshot.Source == "local" && (scope.Snapshot.Root != input.Metadata.Root || scope.Snapshot.Branch != input.Metadata.Branch || !sameHead(scope.Snapshot.Head, input.Metadata.Head)) {
			return fmt.Errorf("Git snapshot and observation metadata must describe the same checkout state")
		}
		first := input.Scopes[0].Snapshot
		if scope.Snapshot.Source != first.Source || scope.Snapshot.Root != first.Root || scope.Snapshot.Branch != first.Branch || !sameHead(scope.Snapshot.Head, first.Head) {
			return fmt.Errorf("all scopes must describe the same checkout state")
		}
		if scope.Snapshot.Revision == "" || scope.Snapshot.Files == nil {
			return fmt.Errorf("snapshot revision and files required")
		}
		files := make(map[string]bool)
		paths := make(map[string]bool)
		for _, file := range scope.Snapshot.Files {
			if file.ID == "" || file.Path == "" || file.Fingerprint == "" || files[file.ID] || paths[file.Path] {
				return fmt.Errorf("unique file identities, paths, and fingerprints required")
			}
			files[file.ID], paths[file.Path] = true, true
			if _, ok := scope.Patches[file.ID]; !ok {
				return fmt.Errorf("preview required for every file, including unavailable previews")
			}
		}
		if len(files) != len(scope.Patches) {
			return fmt.Errorf("preview identities must match snapshot files")
		}
	}
	if !seen[review.DiffAll] {
		return fmt.Errorf("all scope required")
	}
	return nil
}

// Explicit kinds describe endpoints independently of which other views a
// producer captured. Only legacy interpretation depends on the collected set.
func validateComparisonKind(snapshot review.RepositoryDiff, metadata Metadata) error {
	kind := snapshot.ComparisonKind
	branch := metadata.Comparison != nil && metadata.Comparison.Kind == "branch"
	valid := false
	if snapshot.Source == "stdin" {
		valid = snapshot.Mode == review.DiffAll && kind == review.ComparisonPatch
	} else {
		switch snapshot.Mode {
		case review.DiffAll:
			valid = !branch && kind == review.ComparisonUncommitted || branch && (kind == review.ComparisonCombined || kind == review.ComparisonCommitted)
		case review.DiffUncommitted:
			valid = kind == review.ComparisonUncommitted
		case review.DiffStaged:
			valid = kind == review.ComparisonStaged
		case review.DiffUnstaged:
			valid = kind == review.ComparisonUnstaged
		case review.DiffCommitted:
			valid = branch && kind == review.ComparisonCommitted
		}
	}
	if !valid {
		return fmt.Errorf("comparisonKind does not match captured scope and baseline")
	}
	if kind == review.ComparisonCommitted && (snapshot.Head == nil || *snapshot.Head == "") {
		return fmt.Errorf("committed comparison requires captured HEAD")
	}
	return nil
}

// A pull request capture is always a GitHub-triggered branch comparison.
func validatePullRequest(metadata Metadata) error {
	pull := metadata.PullRequest
	if (pull != nil) != (metadata.Trigger == GitHubTrigger) {
		return fmt.Errorf("pullRequest metadata and github trigger must be supplied together")
	}
	if pull == nil {
		return nil
	}
	if metadata.SourceID != GitHubSourceID || metadata.Comparison == nil || metadata.Comparison.Kind != "branch" {
		return fmt.Errorf("pullRequest requires the github source and a branch comparison")
	}
	owner, name, ok := strings.Cut(pull.FullName, "/")
	if pull.Provider != "github" || pull.RepositoryID <= 0 || pull.Number <= 0 || !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("pullRequest requires github provider, repositoryId, owner/name and number")
	}
	if pull.URL != fmt.Sprintf("https://github.com/%s/pull/%d", pull.FullName, pull.Number) {
		return fmt.Errorf("pullRequest url must address the pull request on github.com")
	}
	if pull.State != "open" && pull.State != "closed" && pull.State != "merged" {
		return fmt.Errorf("pullRequest state must be open, closed, or merged")
	}
	if pull.Author.Login == "" || pull.Author.URL != "https://github.com/"+pull.Author.Login {
		return fmt.Errorf("pullRequest author requires login and github.com profile url")
	}
	if pull.CreatedAt < 0 || pull.UpdatedAt < pull.CreatedAt {
		return fmt.Errorf("pullRequest times must be nonnegative and ordered")
	}
	if len(pull.Body) > 256<<10 {
		return fmt.Errorf("pullRequest body exceeds 256 KiB")
	}
	for _, value := range []string{pull.FullName, pull.Title, pull.Author.Login, pull.BaseRef, pull.HeadRef, pull.HeadRepository} {
		if len(value) > 32<<10 {
			return fmt.Errorf("metadata value exceeds 32 KiB")
		}
	}
	return nil
}

func validCommit(value string) bool {
	if (len(value) != 40 && len(value) != 64) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sameHead(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
