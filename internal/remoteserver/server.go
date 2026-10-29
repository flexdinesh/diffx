// Package remoteserver composes distributed identity and owner-scoped applications.
package remoteserver

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/ingestionqueue"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/webui"
)

type Settings struct {
	Listen string
	// DatabaseURL selects the shared Postgres database; it holds credentials and is never logged.
	DatabaseURL string
	// PublicURL is the browser-facing origin used in the logged setup link. An
	// https origin, or loopback http for development, enables Web Push.
	PublicURL       string
	InsecureCookies bool
	RetentionDays   int
	GitHub          *GitHubSettings
	// Google enables Google sign-in and invites; it requires PublicURL.
	Google *GoogleSettings
	// ClientIPHeader names the header a trusted ingress sets to the client IP.
	// Non-empty enables authentication rate limiting shared by every replica.
	ClientIPHeader string
}

type GoogleSettings struct {
	ClientID     string
	ClientSecret string
	// authorizeURL and tokenURL replace Google in tests.
	authorizeURL, tokenURL string
}

// composeIdentity composes account rules with Google sign-in when configured.
func composeIdentity(store *pgstore.Store, settings Settings) *identityservice.Service {
	if settings.Google == nil {
		return identityservice.New(store)
	}
	var options []google.Option
	if settings.Google.tokenURL != "" {
		options = append(options, google.WithEndpoints(settings.Google.authorizeURL, settings.Google.tokenURL))
	}
	redirect := strings.TrimSuffix(settings.PublicURL, "/") + googleCallback
	return identityservice.New(store, identityservice.WithGoogle(google.New(settings.Google.ClientID, settings.Google.ClientSecret, redirect, options...)))
}

type application struct {
	service *contextservice.Service
	handler http.Handler
}
type registry struct {
	mu           sync.Mutex
	ctx          context.Context
	store        *pgstore.Store
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
	// Events come from the store's change feed, so every replica hears every commit.
	service := contextservice.NewWithContext(registry.ctx, registry.store, reviewdata.User{ID: account.ID, Name: account.Username}, contextservice.ExternalEvents())
	app := application{service, serverapp.Handler(registry.ctx, service, registry.store, registry.assets, "", serverapp.Options{Queue: registry.store.Queue()})}
	registry.applications[account.ID] = app
	return app, nil
}

// remove stops a deleted or disabled owner's services; a later request for an
// enabled account activates them again.
func (registry *registry) remove(accountID string) error {
	registry.mu.Lock()
	app, ok := registry.applications[accountID]
	delete(registry.applications, accountID)
	registry.mu.Unlock()
	if !ok {
		return nil
	}
	return app.service.Close()
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
func Handler(ctx context.Context, store *pgstore.Store, settings Settings, assets fs.FS) (http.Handler, func() error, error) {
	return MultiHandler(ctx, store, assets, settings)
}
func MultiHandler(ctx context.Context, store *pgstore.Store, assets fs.FS, options ...Settings) (http.Handler, func() error, error) {
	var settings Settings
	if len(options) > 0 {
		settings = options[0]
	}
	ctx, cancel := context.WithCancel(ctx)
	handler, _, closeServices, tasks, err := compose(ctx, store, assets, settings)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	done := make(chan error, len(tasks))
	for _, task := range tasks {
		go func() { done <- task(ctx) }()
	}
	var once sync.Once
	var closeErr error
	return probes(store, handler), func() error {
		once.Do(func() {
			cancel()
			for range tasks {
				closeErr = errors.Join(closeErr, <-done)
			}
			closeErr = errors.Join(closeErr, closeServices())
		})
		return closeErr
	}, nil
}

// compose returns the public handler and, when GitHub is configured, the
// internal fetcher handler. Push routes and delivery exist in every remote
// composition; without a suitable public URL push reports itself disabled.
func compose(ctx context.Context, store *pgstore.Store, assets fs.FS, settings Settings) (http.Handler, http.Handler, func() error, []serverapp.Task, error) {
	identity := composeIdentity(store, settings)
	github, err := composeGitHub(store, settings.GitHub)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	push, err := pushservice.New(ctx, store.Push(), pushservice.Options{PublicURL: settings.PublicURL})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	applications := &registry{ctx: ctx, store: store, assets: assets, applications: make(map[string]application)}
	remote := &transport{identity: identity, applications: applications, settings: settings, assets: assets, limiter: newLimiter(store, settings.ClientIPHeader), authSlots: make(chan struct{}, 4), github: github, push: push}
	// Listening starts before serving, so no commit after compose is missed.
	feed, err := store.Listen(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	closeServices := func() error { return errors.Join(applications.close(), feed.Close()) }
	wake := make(chan struct{}, 1)
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
		}, ingestionqueue.Wake(wake))
	}
	tasks := []serverapp.Task{worker, push.Run, replicate(feed, applications, wake)}
	var internal http.Handler
	if github != nil {
		tasks = append(tasks, reconcileAtStartup(github.Reconcile, store.GitHub()))
		internal = fetchHandler(github, settings.GitHub.FetcherSecret)
	}
	return remote, internal, closeServices, tasks, nil
}

// databasePatience lets a replica start before its database accepts connections.
const databasePatience = time.Minute

func Run(ctx context.Context, settings Settings, ready func(string)) error {
	if settings.DatabaseURL == "" {
		return errors.New("remote database URL required")
	}
	retention, err := Retention(settings.RetentionDays)
	if err != nil {
		return err
	}
	store, err := pgstore.OpenWaiting(ctx, settings.DatabaseURL, retention, databasePatience)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.PruneExpired(time.Now()); err != nil {
		return err
	}
	handler, internal, closeServices, tasks, err := compose(ctx, store, webui.Assets(), settings)
	if err != nil {
		return err
	}
	defer closeServices()
	listener, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: probes(store, handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	defer server.Close()
	endpoints := []serverapp.Endpoint{{Server: server, Listener: listener}}
	if internal != nil {
		// The fetcher protocol never shares the public listener.
		internalListener, err := net.Listen("tcp", settings.GitHub.InternalListen)
		if err != nil {
			return err
		}
		defer internalListener.Close()
		internalServer := &http.Server{Handler: internal, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
		defer internalServer.Close()
		endpoints = append(endpoints, serverapp.Endpoint{Server: internalServer, Listener: internalListener})
	}
	setup, err := identityservice.New(store).PrepareSetup(ctx)
	if err != nil {
		return err
	}
	return serverapp.Run(ctx, endpoints, append(tasks, serverapp.Prune(store.PruneExpired)), func() {
		if ready != nil {
			ready(listener.Addr().String())
		}
		if setup != "" {
			origin := strings.TrimSuffix(settings.PublicURL, "/")
			if origin == "" {
				origin = "http://" + listener.Addr().String()
			}
			log.Printf("diffx-server setup: no admin yet; within %s open %s/setup#t=%s", identityservice.SetupLifetime, origin, setup)
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
