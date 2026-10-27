package ingestion

import (
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/review"
)

func commitOID(digit string) string { return strings.Repeat(digit, 40) }

func commitRequest() Request {
	request := validRequest()
	head := commitOID("b")
	request.Metadata.Head = &head
	request.Metadata.Comparison = &Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: commitOID("1"), MergeBase: commitOID("1")}
	request.Scopes[0].Snapshot.Head = &head
	request.Scopes[0].Snapshot.ComparisonKind = review.ComparisonCommitted
	merge := Commit{Commit: review.Commit{OID: commitOID("a"), Parents: []string{commitOID("1"), commitOID("2")}, Subject: "Merge main"}}
	diff := review.RepositoryDiff{Source: "local", Mode: review.DiffCommit, ComparisonKind: review.ComparisonCommit, Head: &head, Revision: "commit", Files: []review.ChangedFile{{ID: "file", Path: "file.txt", Fingerprint: "fingerprint"}}}
	commit := Commit{Commit: review.Commit{OID: head, Parents: []string{commitOID("a")}, PatchID: commitOID("c"), Subject: "Change file"}, Snapshot: &diff, Patches: map[string]review.FilePatch{"file": {Patch: "diff"}}}
	request.Commits, request.CommitsTotal = []Commit{merge, commit}, 3
	return request
}

func TestValidateCommits(t *testing.T) {
	if err := Validate(commitRequest()); err != nil {
		t.Fatalf("valid commits rejected: %v", err)
	}
	withoutCommits := commitRequest()
	withoutCommits.Commits, withoutCommits.CommitsTotal = nil, 0
	if err := Validate(withoutCommits); err != nil {
		t.Fatalf("observation without commits rejected: %v", err)
	}
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"working-tree comparison", func(r *Request) { r.Metadata.Comparison = nil }},
		{"total without commits", func(r *Request) { r.Commits = nil }},
		{"total below captured", func(r *Request) { r.CommitsTotal = 1 }},
		{"not ending at HEAD", func(r *Request) { r.Commits = r.Commits[:1] }},
		{"too many commits", func(r *Request) {
			for range review.MaxCommits {
				r.Commits = append([]Commit{r.Commits[0]}, r.Commits...)
			}
			r.CommitsTotal = len(r.Commits)
		}},
		{"repeated oid", func(r *Request) { r.Commits[0].OID = r.Commits[1].OID }},
		{"uppercase oid", func(r *Request) { r.Commits[0].OID = strings.Repeat("A", 40) }},
		{"invalid parent", func(r *Request) { r.Commits[1].Parents = []string{"parent"} }},
		{"invalid patch id", func(r *Request) { r.Commits[1].PatchID = "patch" }},
		{"missing parents", func(r *Request) { r.Commits[1].Parents = nil }},
		{"oversized body", func(r *Request) { r.Commits[1].Body = strings.Repeat("x", (256<<10)+1) }},
		{"merge with diff", func(r *Request) { r.Commits[0].Snapshot = r.Commits[1].Snapshot }},
		{"commit without diff", func(r *Request) { r.Commits[1].Snapshot, r.Commits[1].Patches = nil, nil }},
		{"diff in a review scope", func(r *Request) {
			diff := *r.Commits[1].Snapshot
			diff.Mode = review.DiffCommitted
			r.Commits[1].Snapshot = &diff
		}},
		{"diff of another commit", func(r *Request) {
			diff := *r.Commits[1].Snapshot
			other := commitOID("d")
			diff.Head = &other
			r.Commits[1].Snapshot = &diff
		}},
		{"diff of another branch", func(r *Request) {
			diff := *r.Commits[1].Snapshot
			diff.Branch = "other"
			r.Commits[1].Snapshot = &diff
		}},
		{"missing preview", func(r *Request) { r.Commits[1].Patches = map[string]review.FilePatch{} }},
	}
	for _, test := range cases {
		request := commitRequest()
		test.change(&request)
		if err := Validate(request); err == nil {
			t.Errorf("%s: accepted", test.name)
		}
	}
}

func TestContentIdentityDistinguishesCapturedCommits(t *testing.T) {
	request := commitRequest()
	request.ContentHash = strings.Repeat("e", 64)
	withCommits, err := ContentIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Commits, request.CommitsTotal = nil, 0
	without, err := ContentIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	if withCommits == without || withCommits == "" {
		t.Fatalf("commit capture must not reuse an observation without commits")
	}
}
