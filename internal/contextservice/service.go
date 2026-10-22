// Package contextservice manages durable review targets independently of transports.
package contextservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/session"
)

type Context struct {
	Stale            bool                            `json:"stale"`
	Sessions         []reviewdata.SessionAssociation `json:"sessions,omitempty"`
	Observation      *ingestion.Metadata             `json:"observation,omitempty"`
	ID               string                          `json:"id"`
	Kind             string                          `json:"kind"`
	Source           string                          `json:"source"`
	Name             string                          `json:"name"`
	Root             *string                         `json:"root"`
	LocationID       *string                         `json:"locationId"`
	RepositoryID     *string                         `json:"repositoryId"`
	CreatedAt        int64                           `json:"createdAt"`
	LastSubmittedAt  int64                           `json:"lastSubmittedAt"`
	LastChangedAt    int64                           `json:"lastChangedAt"`
	ChangedFileCount *int                            `json:"changedFileCount"`
	ExpiresAt        *int64                          `json:"expiresAt"`
	SubmittedFrom    *string                         `json:"submittedFrom"`
	Capabilities     session.Capabilities            `json:"capabilities"`
	Availability     string                          `json:"availability"`
	Branch           *string                         `json:"branch"`
	WorktreeName     *string                         `json:"worktreeName"`
	StreamID         *string                         `json:"streamId"`
	LatestContextID  *string                         `json:"latestContextId"`
}

type Page struct {
	Contexts   []Context `json:"contexts"`
	NextCursor *string   `json:"nextCursor"`
}

type Submission struct {
	Context  Context               `json:"context"`
	Snapshot review.RepositoryDiff `json:"snapshot"`
}

type Service struct {
	events     events
	store      Store
	user       reviewdata.User
	scopeMu    sync.RWMutex
	scope      Scope
	background context.Context
	cancel     context.CancelFunc
}

// Scope limits a composition to its selected checkouts or one capture. The
// zero value is unscoped. Contexts outside the scope behave as not found.
type Scope struct {
	selected     bool
	SourceID     string
	Checkouts    []ingestion.Checkout
	SubmissionID string
}

// ScopeFor selects the requests' checkouts, so later collections and branches
// of those checkouts stay visible. Captures without checkout identity select
// their own submission. An empty selection matches nothing until widened.
func ScopeFor(inputs ...ingestion.Request) Scope {
	scope := Scope{selected: true}
	for _, input := range inputs {
		scope = scope.with(input)
	}
	return scope
}

func (scope Scope) with(input ingestion.Request) Scope {
	metadata := input.Metadata
	scope.SourceID = metadata.SourceID
	if metadata.RepositoryKey == "" || metadata.CheckoutKey == "" {
		scope.SubmissionID = input.SubmissionID
		return scope
	}
	checkout := ingestion.Checkout{RepositoryKey: metadata.RepositoryKey, CheckoutKey: metadata.CheckoutKey}
	if !slices.Contains(scope.Checkouts, checkout) {
		scope.Checkouts = append(slices.Clip(scope.Checkouts), checkout)
	}
	return scope
}

func (scope Scope) IsZero() bool {
	return !scope.selected && scope.SourceID == "" && len(scope.Checkouts) == 0 && scope.SubmissionID == ""
}

func (scope Scope) selection(contextID string) ingestion.Selection {
	return ingestion.Selection{Scoped: !scope.IsZero(), SourceID: scope.SourceID, Checkouts: scope.Checkouts, SubmissionID: scope.SubmissionID, ContextID: contextID}
}

func New(store Store, user reviewdata.User) *Service {
	return NewWithContext(context.Background(), store, user)
}
func NewWithContext(ctx context.Context, store Store, user reviewdata.User) *Service {
	return NewScoped(ctx, store, user, Scope{})
}
func NewScoped(ctx context.Context, store Store, user reviewdata.User, scope Scope) *Service {
	background, cancel := context.WithCancel(ctx)
	return &Service{store: store, user: user, scope: scope, background: background, cancel: cancel}
}
func (service *Service) Close() error { service.cancel(); return nil }

// Select widens a scoped composition to the request's checkout before it is
// ingested, so checkouts discovered after startup become visible. Unscoped
// services are unchanged; only local entry points select.
func (service *Service) Select(input ingestion.Request) {
	service.scopeMu.Lock()
	defer service.scopeMu.Unlock()
	if !service.scope.IsZero() {
		service.scope = service.scope.with(input)
	}
}

func (service *Service) selection(contextID string) ingestion.Selection {
	service.scopeMu.RLock()
	defer service.scopeMu.RUnlock()
	return service.scope.selection(contextID)
}
func (service *Service) UserID() string { return service.user.ID }

func (service *Service) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := service.inScope(ctx, id); err != nil {
		return requestError(err)
	}
	if err := service.store.DeleteContext(ctx, service.user.ID, id); err != nil {
		return requestError(err)
	}
	service.events.publish(id)
	return nil
}

func (service *Service) Resolve(ctx context.Context, id string) (session.Session, error) {
	if err := service.background.Err(); err != nil {
		return session.Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return session.Session{}, err
	}
	now := time.Now()
	item, err := service.store.Context(ctx, service.user.ID, id, now)
	if err == nil {
		err = service.inScope(ctx, id)
	}
	if err != nil {
		return session.Session{}, requestError(err)
	}
	binding, err := service.store.ContextBinding(ctx, service.user.ID, id, now)
	if err != nil {
		return session.Session{}, requestError(err)
	}
	if item.Metadata == nil {
		return session.Session{}, review.Error(500, "Observation metadata missing")
	}
	source := &storedSource{store: service.store, ownerID: service.user.ID, id: id, metadata: *item.Metadata, binding: binding, kind: item.Source}
	resolved := session.Resolve(source, session.Policies{})
	resolved.User = session.User{ID: service.user.ID, Name: service.user.Name}
	resolved.ContextID = binding.ContextID
	resolved.LocationID = binding.LocationID
	resolved.RepositoryID = binding.RepositoryID
	resolved.DiffIDs = binding.DiffIDs
	return resolved, nil
}

func (service *Service) Get(ctx context.Context, id string) (Context, error) {
	if err := ctx.Err(); err != nil {
		return Context{}, err
	}
	item, err := service.store.Context(ctx, service.user.ID, id, time.Now())
	if err == nil {
		err = service.inScope(ctx, id)
	}
	if err != nil {
		return Context{}, requestError(err)
	}
	return service.present(ctx, item)
}

// inScope reports out-of-scope contexts as not found, never as forbidden.
func (service *Service) inScope(ctx context.Context, id string) error {
	selection := service.selection(id)
	if !selection.Scoped {
		return nil
	}
	items, err := service.store.ObservationContexts(ctx, service.user.ID, 1, 0, "", ingestion.Filter{Selection: selection})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return reviewdata.ErrNotFound
	}
	return nil
}

type cursorValue struct {
	Time int64  `json:"t"`
	ID   string `json:"id"`
}

func (service *Service) List(ctx context.Context, limit int, cursor string) (Page, error) {
	return service.ListFiltered(ctx, limit, cursor, ingestion.Filter{})
}

func (service *Service) ListFiltered(ctx context.Context, limit int, cursor string, filter ingestion.Filter) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return Page{}, review.Error(400, "Limit must be between 1 and 500")
	}
	var before cursorValue
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &before) != nil || before.ID == "" || before.Time < 0 {
			return Page{}, review.Error(400, "Invalid context cursor")
		}
	}
	var items []reviewdata.ContextInfo
	var err error
	filter.Selection = service.selection("")
	if !filter.IsZero() {
		items, err = service.store.ObservationContexts(ctx, service.user.ID, limit+1, before.Time, before.ID, filter)
	} else {
		items, err = service.store.Contexts(ctx, service.user.ID, limit+1, before.Time, before.ID, time.Now())
	}
	if err != nil {
		return Page{}, err
	}
	page := Page{Contexts: make([]Context, 0, limit)}
	if len(items) > limit {
		last := items[limit-1]
		raw, err := json.Marshal(cursorValue{Time: last.LastSubmittedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		next := base64.RawURLEncoding.EncodeToString(raw)
		page.NextCursor = &next
		items = items[:limit]
	}
	for _, item := range items {
		value, err := service.present(ctx, item)
		if err != nil {
			return Page{}, err
		}
		page.Contexts = append(page.Contexts, value)
	}
	return page, nil
}

func (service *Service) Count(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return service.store.ContextCount(ctx, service.user.ID, service.selection(""), time.Now())
}

func (service *Service) present(ctx context.Context, item reviewdata.ContextInfo) (Context, error) {
	if item.Kind == "observation" && item.Metadata != nil {
		m := item.Metadata
		binding, err := service.store.ContextBinding(ctx, service.user.ID, item.ID, time.Now())
		scopes := []review.DiffMode{}
		if err != nil {
			return Context{}, requestError(err)
		}
		for _, mode := range []review.DiffMode{review.DiffAll, review.DiffStaged, review.DiffUnstaged} {
			if _, ok := binding.DiffIDs[mode]; ok {
				scopes = append(scopes, mode)
			}
		}
		name := review.RemoteRepositoryName(m.RemoteURL, m.RepositoryName)
		if name == "" {
			name = "Piped"
		}
		value := Context{Source: item.Source, Stale: item.Stale, ID: item.ID, Kind: "observation", Name: name, Root: &m.Root, RepositoryID: item.RepositoryID, CreatedAt: item.CreatedAt, LastSubmittedAt: item.LastSubmittedAt, LastChangedAt: item.ActivityAt, ExpiresAt: item.ExpiresAt, ChangedFileCount: item.ChangedFileCount, SubmittedFrom: item.SubmittedFrom, Observation: m, Sessions: item.Sessions, Branch: &m.Branch, WorktreeName: &m.WorktreeName, StreamID: item.StreamID, LatestContextID: item.LatestContextID, Availability: "available", Capabilities: storedCapabilities(scopes, item.Source == "local")}
		if item.Source == "stdin" {
			value.Name, value.Stale = "Piped", false
			value.Root, value.RepositoryID, value.Branch, value.WorktreeName = nil, nil, nil, nil
			value.StreamID, value.LatestContextID = nil, nil
		}
		return value, nil
	}
	return Context{}, review.Error(500, "Observation metadata missing")
}

func requestError(err error) error {
	switch {
	case errors.Is(err, reviewdata.ErrNotFound):
		return review.Error(404, "Context not found")
	case errors.Is(err, reviewdata.ErrExpired):
		return review.Error(410, "Capture expired")
	case errors.Is(err, reviewdata.ErrSubmissionConflict):
		return review.Error(409, "Submission ID reused with different input")
	default:
		return err
	}
}

// Unavailable worktrees retain their review bindings and stored versions.
