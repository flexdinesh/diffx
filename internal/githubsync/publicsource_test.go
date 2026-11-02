package githubsync_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/github"
	"github.com/flexdinesh/diffx/internal/githubsync"
)

// The errors GitHub answers a refused credential with.
var (
	ipAllowList  = &github.Error{Status: http.StatusForbidden, Message: "the organization has an IP allow list enabled"}
	badToken     = &github.Error{Status: http.StatusUnauthorized, Message: "Bad credentials"}
	rateLimited  = &github.Error{Status: http.StatusTooManyRequests, Message: "API rate limit exceeded"}
	unmintable   = fmt.Errorf("%w: %w", github.ErrCredentialUnavailable, &github.Error{Status: http.StatusNotFound, Message: "Not Found"})
	publicKey    = fmt.Sprintf("installation:%d", publicInstallation)
	aliceViewKey = "user:access-alice"
)

func (f *fixture) ensureAs(t *testing.T, viewer string, admit *admitter) githubsync.PublicPullRequest {
	t.Helper()
	status, err := f.service.EnsurePublic(t.Context(), "octo", "app", 1, viewer, admit.admit)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestRefusedCredentialsFallBackToAnonymousReadsAndFetches(t *testing.T) {
	for name, refusal := range map[string]error{
		"restricted":   ipAllowList,
		"unauthorized": badToken,
		"rate limited": rateLimited,
		"unmintable":   unmintable,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.publicPull()
			f.github.refused = map[string]error{publicKey: refusal}
			if status := f.ensure(t, &admitter{}); status.Status != githubsync.PublicQueued {
				t.Fatalf("ensure: %+v", status)
			}
			if want := []string{publicKey, "anonymous", publicKey, "anonymous"}; !slices.Equal(f.github.reads, want) {
				t.Fatalf("reads %v want %v", f.github.reads, want)
			}
			// The lease reads anonymously too, so the fetch carries no token.
			job := f.lease(t)
			if job == nil || !job.Public || job.Token != "" {
				t.Fatalf("lease: %+v", job)
			}
			if len(f.github.tokens) != 0 {
				t.Fatalf("minted tokens: %+v", f.github.tokens)
			}
		})
	}
}

func TestViewerReadsBeforeAnonymousButNeverFetches(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	f.publicPull()
	f.github.refused = map[string]error{publicKey: ipAllowList}
	f.github.reads = nil
	if status := f.ensureAs(t, f.alice, &admitter{}); status.Status != githubsync.PublicQueued {
		t.Fatalf("ensure: %+v", status)
	}
	if want := []string{publicKey, aliceViewKey, publicKey, aliceViewKey}; !slices.Equal(f.github.reads, want) {
		t.Fatalf("viewer reads %v want %v", f.github.reads, want)
	}
	f.github.reads = nil
	job := f.lease(t)
	if job == nil || !job.Public || job.Token != "" {
		t.Fatalf("lease: %+v", job)
	}
	if want := []string{publicKey, "anonymous"}; !slices.Equal(f.github.reads, want) {
		t.Fatalf("lease reads %v want %v", f.github.reads, want)
	}
}

func TestNotFoundNeverReachesTheViewer(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	f.publicPull()
	// The public installation cannot see the repository: it is not public.
	f.github.hidden = map[string]bool{publicKey: true}
	f.github.reads = nil
	status := f.ensureAs(t, f.alice, &admitter{})
	if status.Failure == nil || status.Failure.Code != githubsync.FailureNotFound {
		t.Fatalf("hidden repository: %+v", status)
	}
	if want := []string{publicKey}; !slices.Equal(f.github.reads, want) {
		t.Fatalf("reads %v want %v", f.github.reads, want)
	}

	// The repository's own installation may answer 404 from a stale reach,
	// so the chain goes on, but past the viewer.
	const own = 77
	f.github.installed = map[string]int64{"octo/app": own}
	// Past the cached answer that the app was not installed.
	*f.clock = f.clock.Add(11 * time.Minute)
	f.github.hidden = map[string]bool{"installation:77": true}
	f.github.refused = map[string]error{publicKey: ipAllowList}
	f.github.reads = nil
	if status := f.ensureAs(t, f.alice, &admitter{}); status.Status != githubsync.PublicQueued {
		t.Fatalf("stale installation: %+v", status)
	}
	if f.github.reads[0] != "installation:77" || slices.Contains(f.github.reads, aliceViewKey) || f.github.reads[len(f.github.reads)-1] != "anonymous" {
		t.Fatalf("reads after a 404: %v", f.github.reads)
	}
}

func TestEveryRefusedSourceAnswersRestrictedWithTheViewersRemedy(t *testing.T) {
	f := newFixture(t)
	f.connect(t, f.alice, "alice")
	f.publicPull()
	f.github.refused = map[string]error{publicKey: ipAllowList, "anonymous": ipAllowList, aliceViewKey: ipAllowList}
	signIn, connect := githubsync.RemedySignIn, githubsync.RemedyConnectGitHub
	admit := &admitter{}
	for _, test := range []struct {
		name   string
		viewer string
		remedy *string
	}{
		{"signed out", "", &signIn},
		{"not connected", f.bob, &connect},
		{"viewer refused", f.alice, nil},
	} {
		status := f.ensureAs(t, test.viewer, admit)
		failure := status.Failure
		if status.Status != githubsync.PublicFailed || failure == nil || failure.Code != githubsync.FailureRestricted || failure.Detail == "" {
			t.Fatalf("%s: %+v", test.name, status)
		}
		if (failure.Remedy == nil) != (test.remedy == nil) || failure.Remedy != nil && *failure.Remedy != *test.remedy {
			t.Fatalf("%s: remedy %v want %v", test.name, failure.Remedy, test.remedy)
		}
	}
	if admit.count != 0 {
		t.Fatalf("restricted refusals counted %d fetches", admit.count)
	}
	if _, err := f.service.PublicStatus(t.Context(), "octo", "app", 1); !errors.Is(err, githubsync.ErrNotFound) {
		t.Fatalf("restricted refusal persisted: %v", err)
	}

	// A rate limit among the refusals is returned for the caller to retry.
	f.github.refused["anonymous"] = rateLimited
	var status *github.Error
	if _, err := f.service.EnsurePublic(t.Context(), "octo", "app", 1, "", admit.admit); !errors.As(err, &status) || status.Status != http.StatusTooManyRequests {
		t.Fatalf("rate limited sources: %v", err)
	}
}

func TestPublicReadSourcesArePolicy(t *testing.T) {
	f := newFixture(t)
	key := bytes.Repeat([]byte{7}, 32)
	for _, sources := range [][]githubsync.PublicSource{
		{},
		{githubsync.SourceAnonymous, githubsync.SourceAnonymous},
		{"cache"},
	} {
		if _, err := githubsync.New(f.store.GitHub(), f.github, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: key, PublicInstallationID: publicInstallation, PublicReadSources: sources}); err == nil {
			t.Fatalf("sources %v accepted", sources)
		}
	}
	anonymousOnly, err := githubsync.New(f.store.GitHub(), f.github, githubsync.Options{PublicURL: "https://diffx.test", TokenKey: key, PublicInstallationID: publicInstallation,
		PublicReadSources: []githubsync.PublicSource{githubsync.SourceAnonymous}})
	if err != nil {
		t.Fatal(err)
	}
	f.publicPull()
	if _, err := anonymousOnly.EnsurePublic(t.Context(), "octo", "app", 1, "", (&admitter{}).admit); err != nil {
		t.Fatal(err)
	}
	if want := []string{"anonymous", "anonymous"}; !slices.Equal(f.github.reads, want) {
		t.Fatalf("reads %v want %v", f.github.reads, want)
	}
}
