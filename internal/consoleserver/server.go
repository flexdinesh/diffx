// Package consoleserver serves the admin console: first-admin setup, admin
// sign-in, and account and invite administration over the server's database.
// It composes the identity service for the console audience; disabling or
// deleting an account notifies every diffx-server replica through the store.
package consoleserver

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/authhttp"
	"github.com/flexdinesh/diffx/internal/consoleui"
	"github.com/flexdinesh/diffx/internal/google"
	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/pgstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
)

type Settings struct {
	Listen string
	// DatabaseURL selects the server's Postgres database; never logged.
	DatabaseURL string
	// PublicURL is the console's browser-facing origin, used in the setup link,
	// admin reset links and the Google callback. Unset, links use the listener.
	PublicURL string
	// AppURL is the app's origin, used in invite and member reset links.
	AppURL          string
	InsecureCookies bool
	// Google enables console Google sign-in for existing admins; it requires PublicURL.
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

// databasePatience lets the console start before its database accepts connections.
const databasePatience = time.Minute

// Compose returns the identity service for store with the console's Google
// client, whose callback is on the console origin.
func Compose(store *pgstore.Store, settings Settings) *identityservice.Service {
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

// Handler serves the console API, probes and assets.
func Handler(store *pgstore.Store, settings Settings, assets fs.FS) http.Handler {
	return authhttp.Probes(store, &transport{identity: Compose(store, settings), settings: settings, assets: assets, admission: authhttp.NewAdmission(store, settings.ClientIPHeader)})
}

// Open connects to and migrates the server's database, waiting for it to start.
func Open(ctx context.Context, databaseURL string) (*pgstore.Store, error) {
	if databaseURL == "" {
		return nil, errors.New("console database URL required")
	}
	// Retention never applies: the console never ingests.
	return pgstore.OpenWaiting(ctx, databaseURL, pgstore.DefaultRetention, databasePatience)
}

// Run serves the console until ctx ends. While no admin exists, each start
// logs a one-time setup link that creates the first admin.
func Run(ctx context.Context, settings Settings, ready func(string)) error {
	store, err := Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	listener, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	setup, err := identityservice.New(store).PrepareSetup(ctx)
	if err != nil {
		return err
	}
	if setup != "" {
		log.Printf("diffx-console setup: no admin yet; within %s open %s/setup#t=%s", identityservice.SetupLifetime, origin(settings, listener.Addr().String()), setup)
	}
	server := &http.Server{Handler: Handler(store, settings, consoleui.Assets()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Minute, IdleTimeout: 60 * time.Second}
	defer server.Close()
	return serverapp.Run(ctx, []serverapp.Endpoint{{Server: server, Listener: listener}}, nil, func() {
		if ready != nil {
			ready(listener.Addr().String())
		}
	})
}

// origin is the console's browser-facing origin for links.
func origin(settings Settings, address string) string {
	if settings.PublicURL != "" {
		return strings.TrimSuffix(settings.PublicURL, "/")
	}
	return "http://" + address
}
