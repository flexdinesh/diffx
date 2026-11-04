// Package workspace owns native presentation state over the public REST API.
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

type MutationResult struct {
	ID        uint64
	ContextID string
	Scope     goclient.DiffMode
	CommitID  string
	Kind      string
	Done      bool
	Error     string
}

type State struct {
	Bootstrap       *goclient.Bootstrap0
	Contexts        []goclient.Context
	History         []goclient.Context
	Current         *goclient.Context
	Session         *goclient.Session
	Diff            *goclient.RepositoryDiff
	Scope           goclient.DiffMode
	Commits         []goclient.CommitSummary
	CommitsOmitted  int
	CommitID        string
	Patches         map[string]goclient.FilePatch
	Contents        map[string]goclient.FileContents
	PatchLoading    map[string]bool
	ContentLoading  map[string]bool
	FileErrors      map[string]string
	Comments        []goclient.ReviewComment
	Marks           []goclient.ReviewMark
	Loading         bool
	Collecting      bool
	Saving          bool
	Error           string
	Notice          string
	ReviewError     string
	CollectionError string
	CatalogError    string
	NewerContextID  string
	Collection      *goclient.CollectionResult
	Connected       bool
	Query           string
	NextCursor      string
	Searching       bool
	SearchError     string
	Filters         Filters
	Seen            map[string]string
	SavedScopes     map[string]goclient.DiffMode
	Results         map[uint64]MutationResult
	Metrics         *goclient.ServerMetrics
	MetricsLoading  bool
	MetricsError    string
}

type Controller struct {
	client              *goclient.ClientWithResponses
	notify              func()
	mu                  sync.Mutex
	wg                  sync.WaitGroup
	state               State
	ctx                 context.Context
	cancel              context.CancelFunc
	closed              bool
	input               localprotocol.Input
	selected            context.Context
	selectCancel        context.CancelFunc
	selectedID          string
	generation          uint64
	reviewEpoch         uint64
	reviewCancel        context.CancelFunc
	searchGeneration    uint64
	searchCancel        context.CancelFunc
	catalogActive       bool
	catalogAgain        bool
	initialJoined       bool
	collectionFailed    bool
	collectionCompleted bool
	previews            chan struct{}
	cacheOrder          []string
	nextOperation       uint64
	writes              []mutation
	writeReady          chan struct{}
	pending             int
}

func New(client *goclient.ClientWithResponses, notify func()) *Controller {
	return &Controller{client: client, notify: notify, previews: make(chan struct{}, 4), writeReady: make(chan struct{}, 1), state: State{
		Patches: map[string]goclient.FilePatch{}, Contents: map[string]goclient.FileContents{}, PatchLoading: map[string]bool{}, ContentLoading: map[string]bool{}, FileErrors: map[string]string{}, Seen: nil, SavedScopes: map[string]goclient.DiffMode{}, Results: map[uint64]MutationResult{}, Filters: DefaultFilters(),
	}}
}

// Snapshot detaches every nested wire value, including nullable maps. Callers
// can retain it across notifications without observing later state changes.
func (c *Controller) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return detached(c.state)
}

func detached[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var copy T
	if err := json.Unmarshal(data, &copy); err != nil {
		panic(err)
	}
	return copy
}

func (c *Controller) emit() {
	if c.notify != nil {
		c.notify()
	}
}
func (c *Controller) change(fn func(*State)) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	fn(&c.state)
	c.mu.Unlock()
	c.emit()
}

// taskLocked starts only while holding mu, so Close can safely join all work.
func (c *Controller) taskLocked(fn func()) {
	c.wg.Add(1)
	go func() { defer c.wg.Done(); fn() }()
}

func (c *Controller) Start(ctx context.Context, input localprotocol.Input) {
	c.mu.Lock()
	if c.ctx != nil || c.closed {
		c.mu.Unlock()
		return
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.input = detached(input)
	c.taskLocked(c.writeLoop)
	c.taskLocked(c.bootstrap)
	c.taskLocked(c.events)
	c.mu.Unlock()
}

func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

func (c *Controller) aliveLocked() bool { return !c.closed && c.ctx != nil && c.ctx.Err() == nil }
func (c *Controller) ownsLocked(generation uint64) bool {
	return c.aliveLocked() && generation == c.generation
}

func (c *Controller) bootstrap() {
	r, err := c.client.GetBootstrapWithResponse(c.ctx)
	if err == nil {
		err = responseError(r, 200)
	}
	if err == nil && r.JSON200 == nil {
		err = errors.New("bootstrap response missing")
	}
	var boot goclient.Bootstrap0
	if err == nil {
		boot, err = r.JSON200.AsBootstrap0()
		if err == nil && boot.Mode != "local" {
			err = errors.New("native application requires a local server")
		}
	}
	if err != nil {
		c.change(func(s *State) { s.Error = err.Error() })
		return
	}
	c.change(func(s *State) { s.Bootstrap = &boot; s.Error = "" })
	if c.input.ContextID != "" {
		c.OpenContext(c.input.ContextID)
	}
	c.Reload()
	if boot.Collect && c.input.Kind != "capture" {
		c.collect(true)
	}
}

type response interface {
	StatusCode() int
	GetBody() []byte
	GetApplicationproblemJSONDefault() *goclient.Problem
}

func responseError(r response, statuses ...int) error {
	for _, status := range statuses {
		if r.StatusCode() == status {
			return nil
		}
	}
	if problem := r.GetApplicationproblemJSONDefault(); problem != nil {
		return fmt.Errorf("%s", problem.Detail)
	}
	var problem goclient.Problem
	if json.Unmarshal(r.GetBody(), &problem) == nil && problem.Detail != "" {
		return errors.New(problem.Detail)
	}
	return fmt.Errorf("server request failed (HTTP %d)", r.StatusCode())
}

func (c *Controller) RestoreSeen(seen map[string]string) {
	c.change(func(s *State) { s.Seen = detached(seen) })
}
func (c *Controller) RestoreScopes(scopes map[string]goclient.DiffMode) {
	c.change(func(s *State) {
		s.SavedScopes = detached(scopes)
		if s.SavedScopes == nil {
			s.SavedScopes = map[string]goclient.DiffMode{}
		}
	})
}
func (c *Controller) ForgetResult(id uint64) {
	c.change(func(s *State) {
		if result, ok := s.Results[id]; ok && result.Done {
			delete(s.Results, id)
		}
	})
}

func (c *Controller) Retry() {
	c.mu.Lock()
	if !c.aliveLocked() {
		c.mu.Unlock()
		return
	}
	boot, failed, id, scope, commit := c.state.Bootstrap, c.collectionFailed, c.selectedID, c.state.Scope, c.state.CommitID
	if boot == nil {
		c.taskLocked(c.bootstrap)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if failed {
		c.Sync()
	}
	c.Reload()
	if id != "" {
		c.open(id, scope, commit)
	}
}
