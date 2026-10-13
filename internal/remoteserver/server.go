// Package remoteserver composes distributed identity and owner-scoped applications.
package remoteserver

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/webui"
)

type Settings struct {
	Listen          string
	State           string
	Username        string
	Password        string
	SignupEnabled   bool
	InsecureCookies bool
	RetentionDays   int
}

type application struct {
	service *contextservice.Service
	handler http.Handler
}
type registry struct {
	mu           sync.Mutex
	ctx          context.Context
	store        *reviewstore.Store
	assets       fs.FS
	applications map[string]application
	closed       bool
}

func (registry *registry) get(account identityservice.Account) (application, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return application{}, context.Canceled
	}
	if app, ok := registry.applications[account.ID]; ok {
		return app, nil
	}
	service := contextservice.NewWithContext(registry.ctx, registry.store, reviewstore.User{ID: account.ID, Name: account.Username})
	app := application{service, serverapp.Handler(registry.ctx, service, registry.store, registry.assets, "", serverapp.Options{Queue: registry.store.Queue()})}
	registry.applications[account.ID] = app
	return app, nil
}
func (registry *registry) close() error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.closed = true
	var err error
	for _, app := range registry.applications {
		err = errors.Join(err, app.service.Close())
	}
	return err
}

// Handler supplies distributed identity; local composition never calls it.
func Handler(ctx context.Context, store *reviewstore.Store, settings Settings, assets fs.FS) (http.Handler, func() error, error) {
	return MultiHandler(ctx, store, assets, settings)
}
func MultiHandler(ctx context.Context, store *reviewstore.Store, assets fs.FS, options ...Settings) (http.Handler, func() error, error) {
	var settings Settings
	if len(options) > 0 {
		settings = options[0]
	}
	ctx, cancel := context.WithCancel(ctx)
	handler, closeServices, worker, err := compose(ctx, store, assets, settings)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- worker(ctx) }()
	var once sync.Once
	var closeErr error
	return handler, func() error {
		once.Do(func() { cancel(); closeErr = errors.Join(<-done, closeServices()) })
		return closeErr
	}, nil
}
func compose(ctx context.Context, store *reviewstore.Store, assets fs.FS, settings Settings) (http.Handler, func() error, serverapp.Task, error) {
	identity := identityservice.New(store)
	if err := bootstrap(ctx, identity, settings); err != nil {
		return nil, nil, nil, err
	}
	applications := &registry{ctx: ctx, store: store, assets: assets, applications: make(map[string]application)}
	remote := &transport{identity: identity, applications: applications, settings: settings, assets: assets, limiter: newLimiter(), authSlots: make(chan struct{}, 4)}
	worker := func(ctx context.Context) error {
		return ingestionqueue.Run(ctx, store.Queue(), func(ctx context.Context, owner string, input ingestion.Request) (string, error) {
			accounts, err := identity.Accounts(ctx)
			if err != nil {
				return "", err
			}
			for _, account := range accounts {
				if account.ID == owner {
					app, err := applications.get(account)
					if err != nil {
						return "", err
					}
					result, err := app.service.Ingest(ctx, input)
					return result.Context.ID, err
				}
			}
			return "", review.Error(403, "Account unavailable")
		})
	}
	return remote, applications.close, worker, nil
}
func bootstrap(ctx context.Context, identity *identityservice.Service, settings Settings) error {
	if (settings.Username == "") != (settings.Password == "") {
		return errors.New("DIFFX_SERVER_USERNAME and DIFFX_SERVER_PASSWORD must be supplied together")
	}
	if settings.Username != "" {
		if _, err := identity.Bootstrap(ctx, settings.Username, settings.Password); err != nil {
			return err
		}
	}
	accounts, err := identity.Accounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 && !settings.SignupEnabled {
		return errors.New("remote server needs a provisioned user, bootstrap username/password, or signup enabled")
	}
	return nil
}
func Run(ctx context.Context, settings Settings, ready func(string)) error {
	if settings.State == "" {
		return errors.New("remote state database path required")
	}
	if settings.State == "memory" {
		settings.State = ":memory:"
	}
	retention, err := Retention(settings.RetentionDays)
	if err != nil {
		return err
	}
	store, err := reviewstore.OpenWithRetention(settings.State, retention)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.PruneExpired(time.Now()); err != nil {
		return err
	}
	handler, closeServices, worker, err := compose(ctx, store, webui.Assets(), settings)
	if err != nil {
		return err
	}
	defer closeServices()
	listener, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	defer server.Close()
	return serverapp.Run(ctx, []serverapp.Endpoint{{Server: server, Listener: listener}}, []serverapp.Task{worker, serverapp.Prune(store.PruneExpired)}, func() {
		if ready != nil {
			ready(listener.Addr().String())
		}
	})
}
func Retention(days int) (time.Duration, error) {
	if days == 0 {
		days = 7
	}
	const day = 24 * time.Hour
	if days < 0 || int64(days) > int64((1<<63-1)/day) {
		return 0, errors.New("retention days must be positive and fit within a duration")
	}
	return time.Duration(days) * day, nil
}
