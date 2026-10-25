// Package fetcher mirrors leased GitHub pull requests and collects them offline
// with the shared collector. It never claims an account or pull request
// metadata; the server overwrites identity and validates the captured commits.
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/fetchprotocol"
	"github.com/flexdinesh/diffx/internal/ingestion"
)

type Settings struct {
	Mirrors     string
	DiskLimit   int64
	Concurrency int
	// PollInterval is the idle wait between empty leases; zero means 5s.
	PollInterval     time.Duration
	CollectorVersion string
}

// Server is the fetcher's view of the server's internal fetch listener.
type Server interface {
	Lease(context.Context) (*fetchprotocol.Job, error)
	Complete(context.Context, fetchprotocol.Job, ingestion.Request) error
	Fail(context.Context, fetchprotocol.Job, string) error
}

const maxLeaseBackoff = time.Minute

// Run leases and collects pull requests on Concurrency workers until ctx is
// cancelled, then joins every worker and returns nil. Collection inherits the
// process environment, so the caller must export GIT_NO_LAZY_FETCH=1; Run
// refuses to start otherwise rather than mutating process-wide state.
func Run(ctx context.Context, server Server, settings Settings) error {
	return run(ctx, server, settings, github)
}

func run(ctx context.Context, server Server, settings Settings, remote remote) error {
	if os.Getenv("GIT_NO_LAZY_FETCH") != "1" {
		return errors.New("GIT_NO_LAZY_FETCH=1 must be set so collection never fetches missing objects")
	}
	if settings.Mirrors == "" || settings.DiskLimit <= 0 || settings.Concurrency < 1 {
		return errors.New("fetcher requires a mirror directory, a positive disk limit and at least one worker")
	}
	if settings.PollInterval <= 0 {
		settings.PollInterval = 5 * time.Second
	}
	root, err := filepath.Abs(settings.Mirrors)
	if err != nil {
		return err
	}
	if err := requireGit(ctx); err != nil {
		return err
	}
	if err := prepareMirrors(root); err != nil {
		return err
	}
	settings.Mirrors = root
	fetcher := &fetcher{server: server, settings: settings, remote: remote, locks: repositoryLocks{held: make(map[int64]*repositoryLock)}}
	var workers sync.WaitGroup
	for range settings.Concurrency {
		workers.Go(func() { fetcher.work(ctx) })
	}
	workers.Wait()
	return nil
}

type fetcher struct {
	server   Server
	settings Settings
	remote   remote
	locks    repositoryLocks
	evicting sync.Mutex
}

func (fetcher *fetcher) work(ctx context.Context) {
	delay := fetcher.settings.PollInterval
	for ctx.Err() == nil {
		job, err := fetcher.server.Lease(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("diffx-fetcher: lease failed; retrying in %s: %v", delay, err)
			wait(ctx, delay)
			delay = min(2*delay, max(maxLeaseBackoff, fetcher.settings.PollInterval))
			continue
		}
		delay = fetcher.settings.PollInterval
		if job == nil {
			wait(ctx, delay)
			continue
		}
		fetcher.handle(ctx, *job)
	}
}

func wait(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// handle reports exactly one result per lease unless shutdown abandons it;
// an abandoned lease expires and the server leases it again.
func (fetcher *fetcher) handle(ctx context.Context, job fetchprotocol.Job) {
	request, err := fetcher.collect(ctx, job)
	defer fetcher.enforceDiskLimit()
	if ctx.Err() != nil {
		log.Printf("diffx-fetcher: abandoned lease %s on shutdown", job.ID)
		return
	}
	if err != nil {
		detail := redact(err.Error(), job.Token)
		log.Printf("diffx-fetcher: repository %d pull %d failed: %s", job.Repository.ID, job.Number, detail)
		fetcher.reported(job, fetcher.server.Fail(ctx, job, detail))
		return
	}
	fetcher.reported(job, fetcher.server.Complete(ctx, job, request))
}

func (fetcher *fetcher) reported(job fetchprotocol.Job, err error) {
	var status *fetchprotocol.StatusError
	switch {
	case err == nil:
	case errors.As(err, &status) && status.Status == 409:
		log.Printf("diffx-fetcher: lease %s lost; dropped result", job.ID)
	default:
		log.Printf("diffx-fetcher: report lease %s: %s", job.ID, redact(err.Error(), job.Token))
	}
}

// errMoved means GitHub no longer serves the leased commits; the server reads
// the pull request again before its next lease.
var errMoved = errors.New("pull request moved; refetch")

func (fetcher *fetcher) collect(ctx context.Context, job fetchprotocol.Job) (ingestion.Request, error) {
	if err := validateJob(job); err != nil {
		return ingestion.Request{}, err
	}
	url, err := fetcher.remote.url(job.Repository)
	if err != nil {
		return ingestion.Request{}, err
	}
	release := fetcher.locks.lock(job.Repository.ID)
	defer release()
	mirror, err := fetcher.open(ctx, job.Repository.ID, url, job.Token)
	if err != nil {
		return ingestion.Request{}, fmt.Errorf("prepare mirror: %w", err)
	}
	if err := mirror.fetch(ctx, job); err != nil {
		return ingestion.Request{}, err
	}
	if err := mirror.verify(ctx, job); err != nil {
		return ingestion.Request{}, err
	}
	if err := mirror.prefetch(ctx, job); err != nil {
		return ingestion.Request{}, err
	}
	return mirror.collect(ctx, job, fetcher.settings.CollectorVersion)
}

// redact removes the installation token and its encoded header from text
// that may reach logs or the server.
func redact(text, token string) string {
	if token == "" {
		return text
	}
	return strings.NewReplacer(token, "[redacted]", basicCredential(token), "[redacted]").Replace(text)
}
