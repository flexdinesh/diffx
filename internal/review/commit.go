package review

// DiffCommit names a captured commit's own diff. It is not a review scope:
// ParseDiffMode rejects it, so comments and reviewed marks never target it.
const DiffCommit DiffMode = "commit"

// ComparisonCommit compares one commit with its first parent.
const ComparisonCommit ComparisonKind = "commit"

// MaxCommits bounds the commits one observation captures: the newest of the
// branch's merge-base..HEAD range.
const MaxCommits = 100

// Commit is captured commit metadata. Merge commits have several parents and
// no captured diff.
type Commit struct {
	OID         string   `json:"oid"`
	Parents     []string `json:"parents"`
	PatchID     string   `json:"patchId,omitempty"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	AuthorName  string   `json:"authorName"`
	AuthoredAt  int64    `json:"authoredAt"`
	CommittedAt int64    `json:"committedAt"`
}

func (commit Commit) Merge() bool { return len(commit.Parents) > 1 }

// CommitSummary is a stored commit with its captured diff's totals.
type CommitSummary struct {
	Commit
	Merge     bool `json:"merge"`
	FileCount int  `json:"fileCount"`
	Additions int  `json:"additions"`
	Deletions int  `json:"deletions"`
}

// CommitRange counts captured commits and those omitted beyond MaxCommits.
type CommitRange struct {
	Count   int `json:"count"`
	Omitted int `json:"omitted"`
}
