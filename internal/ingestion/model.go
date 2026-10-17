// Package ingestion defines snapshot contracts, pure identity rules and HTTP clients.
// It contains no collector filesystem or server persistence behavior.
package ingestion

import "github.com/flexdinesh/diffx/internal/review"

const ProtocolVersion = 4
const MaxRequestBytes = 64 << 20

// Comparison records the interpretation and resolved Git baseline of a capture.
// Kind is working-tree or branch; absent comparisons retain legacy HEAD semantics.
type Comparison struct {
	Kind       string `json:"kind"`
	BaseRef    string `json:"baseRef"`
	BaseCommit string `json:"baseCommit,omitempty"`
	MergeBase  string `json:"mergeBase,omitempty"`
}

// AgentSession identifies the session observing a capture, never its authorship.
type AgentSession struct {
	Harness string `json:"harness"`
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
}

// Metadata records facts at collection time. Paths and hostnames are labels,
// not globally unique identities. Account identity comes from the server.
type Metadata struct {
	SourceID         string        `json:"sourceId"`
	Hostname         string        `json:"hostname"`
	RunID            string        `json:"runId"`
	Agent            string        `json:"agent"`
	Trigger          string        `json:"trigger"`
	RepositoryKey    string        `json:"repositoryKey"`
	RepositoryName   string        `json:"repositoryName"`
	RemoteURL        string        `json:"remoteUrl"`
	CheckoutKey      string        `json:"checkoutKey"`
	Root             string        `json:"root"`
	WorktreeName     string        `json:"worktreeName"`
	LinkedWorktree   *bool         `json:"linkedWorktree,omitempty"`
	Branch           string        `json:"branch"`
	BranchID         string        `json:"branchId,omitempty"`
	Head             *string       `json:"head"`
	CollectedAt      int64         `json:"collectedAt"`
	CollectorVersion string        `json:"collectorVersion"`
	Comparison       *Comparison   `json:"comparison,omitempty"`
	AgentSession     *AgentSession `json:"agentSession,omitempty"`
	TriggerRoot      string        `json:"triggerRoot,omitempty"`
}

type Scope struct {
	Snapshot review.RepositoryDiff       `json:"snapshot"`
	Patches  map[string]review.FilePatch `json:"patches"`
}

// Request is one immutable observation. Replays retain the submission ID;
// fresh observations may reuse an existing review when ContentHash matches.
type Request struct {
	ProtocolVersion int      `json:"protocolVersion"`
	SubmissionID    string   `json:"submissionId"`
	ContentHash     string   `json:"contentHash,omitempty"`
	Metadata        Metadata `json:"metadata"`
	Scopes          []Scope  `json:"scopes"`
}

type Filter struct {
	Query       string
	Repository  string
	Branch      string
	Worktree    string
	Hostname    string
	SourceID    string
	RunID       string
	Harness     string
	SessionID   string
	SessionName string
	// Latest omits stale snapshots: each stream's head plus stream-less
	// observations such as piped diffs.
	Latest bool
	// Selection is composition policy, never parsed from requests.
	Selection Selection
}

// IsZero reports a filter that neither restricts results nor selects a scope.
func (filter Filter) IsZero() bool {
	terms := filter.Query + filter.Repository + filter.Branch + filter.Worktree + filter.Hostname +
		filter.SourceID + filter.RunID + filter.Harness + filter.SessionID + filter.SessionName
	return terms == "" && !filter.Latest && !filter.Selection.Scoped && filter.Selection.ContextID == ""
}

// Selection restricts the catalog to the composition's selected checkouts or
// capture submission, optionally one context. An unscoped selection restricts
// only by ContextID; a scoped one without checkouts or submission matches nothing.
type Selection struct {
	Scoped       bool
	SourceID     string
	Checkouts    []Checkout
	SubmissionID string
	ContextID    string
}

// Checkout is a persisted checkout identity within a source.
type Checkout struct {
	RepositoryKey string
	CheckoutKey   string
}

type Job struct {
	ID           string `json:"id"`
	SubmissionID string `json:"submissionId"`
	State        string `json:"state"`
	Attempt      int    `json:"attempt"`
	ContextID    string `json:"contextId,omitempty"`
	Detail       string `json:"detail,omitempty"`
}
