package ingestion

import "testing"

// Stored stream keys, replay hashes and review identities are persisted by every
// store; these pinned values must never change.
func TestStreamIdentityIsStable(t *testing.T) {
	metadata := Metadata{SourceID: "source", RepositoryKey: "repository", CheckoutKey: "checkout", Branch: "feature"}
	if got := BranchIdentity(metadata); got != "feature" {
		t.Fatalf("branch label identity = %q", got)
	}
	metadata.BranchID = "branch-id"
	if got := BranchIdentity(metadata); got != "branch-id" {
		t.Fatalf("branch ID identity = %q", got)
	}
	if got := StreamID("source", "repository", "checkout", "branch-id", ""); got != "0e2151f13487b6bdb2886a0d" {
		t.Fatalf("stream ID = %q", got)
	}
	if got := StreamKey("context", metadata); got != StreamID("source", "repository", "checkout", "branch-id", "") {
		t.Fatalf("keyed stream = %q", got)
	}
	metadata.Comparison = &Comparison{Kind: "branch", BaseRef: "main"}
	if got := StreamKey("context", metadata); got != "c8c27a752c3c5315213e6eca" {
		t.Fatalf("policy stream = %q", got)
	}
	if got := StreamKey("context", Metadata{SourceID: "source"}); got != "context:context" {
		t.Fatalf("piped stream = %q", got)
	}
	_, hash, err := Payload(Request{SubmissionID: "submission", Metadata: Metadata{SourceID: "source"}})
	if err != nil || hash != "c5a7533ab7732a23d29bd15f9595a2a91c98e76c5a066355d31911a7a13474c6" {
		t.Fatalf("payload hash = %q, %v", hash, err)
	}
}
