package ingestion

import (
	"slices"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/review"
)

func TestLegacyComparisonInterpretation(t *testing.T) {
	branch := Metadata{Comparison: &Comparison{Kind: "branch"}}
	for _, test := range []struct {
		name     string
		mode     review.DiffMode
		source   string
		metadata Metadata
		scopes   []review.DiffMode
		want     review.ComparisonKind
	}{
		{"HEAD", review.DiffAll, "local", Metadata{}, []review.DiffMode{review.DiffAll}, review.ComparisonUncommitted},
		{"branch checkout", review.DiffAll, "local", branch, []review.DiffMode{review.DiffAll, review.DiffStaged, review.DiffUnstaged}, review.ComparisonCombined},
		{"branch objects", review.DiffAll, "local", branch, []review.DiffMode{review.DiffAll}, review.ComparisonCommitted},
		{"patch", review.DiffAll, "stdin", branch, []review.DiffMode{review.DiffAll}, review.ComparisonPatch},
		{"staged", review.DiffStaged, "local", branch, nil, review.ComparisonStaged},
		{"unstaged", review.DiffUnstaged, "local", branch, nil, review.ComparisonUnstaged},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := LegacyComparisonKind(test.mode, test.source, test.metadata, test.scopes); got != test.want {
				t.Fatalf("kind = %q, want %q", got, test.want)
			}
		})
	}
}

func fiveScopes() Request {
	r := validRequest()
	head := strings.Repeat("c", 40)
	r.Metadata.Head = &head
	r.Metadata.Comparison = &Comparison{Kind: "branch", BaseRef: "refs/heads/main", BaseCommit: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
	base := r.Scopes[0]
	r.Scopes = nil
	for _, entry := range []struct {
		mode review.DiffMode
		kind review.ComparisonKind
	}{
		{review.DiffAll, review.ComparisonCombined}, {review.DiffStaged, review.ComparisonStaged}, {review.DiffUnstaged, review.ComparisonUnstaged},
		{review.DiffUncommitted, review.ComparisonUncommitted}, {review.DiffCommitted, review.ComparisonCommitted},
	} {
		scope := base
		scope.Snapshot.Mode, scope.Snapshot.ComparisonKind, scope.Snapshot.Head = entry.mode, entry.kind, &head
		r.Scopes = append(r.Scopes, scope)
	}
	return r
}

func TestValidateExplicitComparisonKinds(t *testing.T) {
	if err := Validate(fiveScopes()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []review.ComparisonKind{review.ComparisonCombined, review.ComparisonCommitted} {
		r := fiveScopes()
		r.Scopes = r.Scopes[:1]
		r.Scopes[0].Snapshot.ComparisonKind = kind
		if err := Validate(r); err != nil {
			t.Fatalf("standalone %s: %v", kind, err)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*Request)
	}{
		{"six scopes", func(r *Request) { r.Scopes = append(r.Scopes, r.Scopes[0]) }},
		{"new kind required", func(r *Request) { r.Scopes[3].Snapshot.ComparisonKind = "" }},
		{"committed kind required", func(r *Request) { r.Scopes[4].Snapshot.ComparisonKind = "" }},
		{"wrong semantic kind", func(r *Request) { r.Scopes[3].Snapshot.ComparisonKind = review.ComparisonCommitted }},
		{"unknown kind", func(r *Request) { r.Scopes[0].Snapshot.ComparisonKind = "mystery" }},
		{"wrong staged kind", func(r *Request) { r.Scopes[1].Snapshot.ComparisonKind = review.ComparisonUnstaged }},
		{"committed baseline", func(r *Request) { r.Metadata.Comparison = nil }},
		{"committed HEAD", func(r *Request) {
			r.Metadata.Head = nil
			for i := range r.Scopes {
				r.Scopes[i].Snapshot.Head = nil
			}
		}},
		{"patch scope", func(r *Request) {
			for i := range r.Scopes {
				r.Scopes[i].Snapshot.Source = "stdin"
				r.Scopes[i].Snapshot.ComparisonKind = review.ComparisonPatch
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := fiveScopes()
			test.change(&r)
			if err := Validate(r); err == nil {
				t.Fatal("invalid comparison accepted")
			}
		})
	}
}

func TestContentIdentityPreservesLegacyHashAndSeparatesExplicitKinds(t *testing.T) {
	r := validRequest()
	r.ContentHash = strings.Repeat("a", 64)
	legacy, err := ContentIdentity(r)
	if err != nil || legacy != "b0258ba2a703ce506eb55b6f40d91139b197679c4fec9feef3fc6ce7ce761790" {
		t.Fatalf("legacy identity = %s, %v", legacy, err)
	}
	r.Scopes[0].Snapshot.ComparisonKind = review.ComparisonUncommitted
	explicit, err := ContentIdentity(r)
	if err != nil || explicit == legacy {
		t.Fatalf("explicit kind omitted from identity: %s, %v", explicit, err)
	}
	r = fiveScopes()
	r.ContentHash = strings.Repeat("a", 64)
	first, err := ContentIdentity(r)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(r.Scopes)
	reversed, err := ContentIdentity(r)
	if err != nil || reversed != first {
		t.Fatalf("scope order changed identity: %s, %s, %v", first, reversed, err)
	}
	r.Scopes[len(r.Scopes)-1].Snapshot.ComparisonKind = review.ComparisonCommitted
	different, err := ContentIdentity(r)
	if err != nil || different == first {
		t.Fatalf("distinct interpretation deduped: %s, %v", different, err)
	}
}
