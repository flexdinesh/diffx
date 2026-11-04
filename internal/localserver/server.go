package localserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/contextservice"
	"github.com/flexdinesh/diffx/internal/controlapi"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/internal/reviewstore"
	"github.com/flexdinesh/diffx/internal/serverapp"
	"github.com/flexdinesh/diffx/internal/unixsocket"
	buildversion "github.com/flexdinesh/diffx/internal/version"
	"github.com/flexdinesh/diffx/internal/webui"
)

func Run(ctx context.Context, settings Settings, runtimeDir string, initial *ingestion.Request, reset StateReset, ready func(Status, *contextservice.Publication)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var collection Collection
	if initial != nil {
		collection.Requests = []ingestion.Request{*initial}
	}
	published := func(status Status, value Published) {
		if ready == nil {
			return
		}
		var submitted *contextservice.Publication
		if newest, ok := value.Newest(); ok {
			submitted = &newest
		}
		ready(status, submitted)
	}
	instanceID := rand.Text()
	if runtimeDir != "" {
		if _, err := prepareRuntime(runtimeDir); err != nil {
			return err
		}
		owner, err := processlock.TryAcquire(OwnershipPath(runtimeDir))
		if err != nil {
			return fmt.Errorf("service ownership: %w", err)
		}
		defer owner.Close()
		output, err := OpenLog(runtimeDir)
		if err != nil {
			return err
		}
		defer output.Close()
		logger := log.New(output, "diffx: ", log.LstdFlags)
		return runServer(ctx, cancel, settings, runtimeDir, instanceID, Input{Collection: collection}, reset, published, logger)
	}
	return runServer(ctx, cancel, settings, "", instanceID, Input{Collection: collection}, reset, published, log.New(os.Stderr, "diffx: ", log.LstdFlags))
}

// StateReset is an entry point's decision to replace local state. Remote
// composition has none, so its incompatible state is always refused.
type StateReset struct {
	// Force backs up current or older state before opening.
	Force bool
	// Confirm is asked when older diffx state would otherwise be refused.
	Confirm func(*reviewstore.StateError) (bool, error)
	// Saved reports the backup of replaced state.
	Saved func(backup string)
}

// openState applies the reset decision; reviewstore revalidates the state
// under its ownership lock before moving it. Without a decision,
// incompatible state is refused unchanged.
func openState(path string, retention time.Duration, reset StateReset) (*reviewstore.Store, error) {
	if reset.Force {
		if err := resetState(path, reset); err != nil {
			return nil, err
		}
		return reviewstore.OpenWithRetention(path, retention)
	}
	store, err := reviewstore.OpenWithRetention(path, retention)
	var failure *reviewstore.StateError
	if err == nil || reset.Confirm == nil || !errors.As(err, &failure) || !failure.Older() {
		return store, err
	}
	accepted, confirmErr := reset.Confirm(failure)
	if confirmErr != nil {
		return nil, confirmErr
	}
	if !accepted {
		return nil, err
	}
	if err := resetState(path, reset); err != nil {
		return nil, err
	}
	return reviewstore.OpenWithRetention(path, retention)
}

func resetState(path string, reset StateReset) error {
	backup, err := reviewstore.Reset(path)
	if backup != "" && reset.Saved != nil {
		reset.Saved(backup)
	}
	return err
}

// runServer scopes reads to the capture or discovered checkouts. Checkout
// discovery and collection start after serving, with an initially empty scope.
func runServer(ctx context.Context, cancel context.CancelFunc, settings Settings, runtimeDir, instanceID string, input Input, reset StateReset, ready func(Status, Published), logger *log.Logger) error {
	initial, collect := input.Collection, input.Collect
	settings = normalizeSettings(settings)
	if settings.RetentionDays < 1 || settings.RetentionDays > 106751 {
		return errors.New("retention-days must be between 1 and 106751")
	}
	if settings.State == "" {
		path, err := config.DefaultStatePath()
		if err != nil {
			return err
		}
		settings.State = path
	}
	statePath := settings.State
	if statePath == "memory" {
		statePath = ""
	}
	store, err := openState(statePath, time.Duration(settings.RetentionDays)*24*time.Hour, reset)
	if err != nil {
		return err
	}
	defer store.Close()
	processUser, err := osuser.Current()
	if err != nil {
		return fmt.Errorf("resolve process user: %w", err)
	}
	user, err := store.User(processUser.Uid, processUser.Username)
	if err != nil {
		return err
	}
	if err := store.PruneExpired(time.Now()); err != nil {
		return err
	}
	service := contextservice.NewScoped(ctx, store, user, contextservice.ScopeFor(initial.Requests...))
	defer service.Close()
	published, err := serverapp.Publish(ctx, service, initial)
	if err != nil {
		return err
	}
	defaultContextID := ""
	if newest, ok := published.Newest(); ok {
		defaultContextID = newest.Context.ID
	} else if len(published.Failures) > 0 {
		return errors.New("no checkout could be ingested: " + serverapp.FailureSummary(published.Failures))
	}
	var assets fs.FS = webui.Assets()
	if settings.WebDir != "" {
		absolute, err := filepath.Abs(settings.WebDir)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("web-dir must be a directory")
		}
		settings.WebDir = absolute
		assets = os.DirFS(absolute)
	}
	if settings.Headless && runtimeDir == "" {
		return errors.New("headless mode needs the local socket; it has no viewer otherwise")
	}
	status := Status{State: "running", StateID: user.ID, InstanceID: instanceID, PID: os.Getpid(), Version: buildversion.Number(), ProtocolVersion: controlapi.ProtocolVersion}
	if input.Session != nil {
		metadata := *input.Session
		metadata.Input.Paths = append([]string(nil), metadata.Input.Paths...)
		if defaultContextID != "" && metadata.Input.Kind == "capture" {
			metadata.Input.ContextID = defaultContextID
		}
		status.Session = &metadata
	}
	var viewer net.Listener
	if !settings.Headless {
		viewer, err = listenViewer(settings.Port)
		if err != nil {
			return err
		}
		defer viewer.Close()
		address, ok := viewer.Addr().(*net.TCPAddr)
		if !ok {
			return errors.New("unexpected viewer address")
		}
		settings.Port = address.Port
		status.ViewerURL = "http://" + net.JoinHostPort(viewerHost, strconv.Itoa(address.Port))
	}
	if runtimeDir != "" {
		status.Socket = SocketPath(runtimeDir)
	}
	status.Settings = settings
	statusFunc := func() (Status, error) {
		value := status
		var err error
		value.Observations, err = service.Count(ctx)
		return value, err
	}
	configuration := serverapp.Options{}
	tasks := []serverapp.Task{serverapp.Prune(store.PruneExpired)}
	if collect != nil {
		collections := serverapp.NewCollections(service, input.Selection, collect, defaultContextID)
		configuration.Collections = collections
		tasks = append(tasks, collections.Initial(func(published Published, err error) {
			if err != nil {
				logger.Printf("initial collection: %v", err)
				return
			}
			logger.Printf("collected %d checkouts (%d failed)", len(published.Publications), len(published.Failures))
			for _, failure := range published.Failures {
				logger.Printf("%s: %s", failure.Path, failure.Detail)
			}
		}))
	}
	app := browserAccess(serverapp.Handler(ctx, service, store, assets, defaultContextID, configuration))
	var endpoints []serverapp.Endpoint
	if viewer != nil {
		endpoints = append(endpoints, serverapp.Endpoint{Server: newServer(app, logger), Listener: viewer})
	}
	if runtimeDir != "" {
		// The caller holds ownership, so a socket file left here is stale.
		socket, err := unixsocket.Listen(status.Socket)
		if err != nil {
			return fmt.Errorf("local socket: %w", err)
		}
		defer socket.Close()
		// Control is reachable only through the owner's socket.
		routes := http.NewServeMux()
		routes.Handle("/control/v1/", controlapi.NewWithLifetime(ctx, statusFunc, cancel))
		routes.Handle("/", app)
		endpoints = append(endpoints, serverapp.Endpoint{Server: newServer(routes, logger), Listener: sameUser{Listener: socket, uid: uint32(os.Getuid())}})
	}
	return serverapp.Run(ctx, endpoints, tasks, func() {
		if ready != nil {
			ready(status, published)
		}
	})
}

// viewerHost is the only viewer bind address. Other machines reach a viewer
// or the socket through SSH forwarding, never a public listener.
const viewerHost = "127.0.0.1"

func newServer(handler http.Handler, logger *log.Logger) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, ErrorLog: logger}
}

func listenViewer(port int) (net.Listener, error) {
	if port >= 0 {
		return net.Listen("tcp", net.JoinHostPort(viewerHost, strconv.Itoa(port)))
	}
	for port := 7981; port <= 7990; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(viewerHost, strconv.Itoa(port)))
		if err == nil {
			return listener, nil
		}
	}
	return nil, errors.New("no available diffx port (7981-7990); choose --port")
}

// sameUser admits only connections from the owning user. The runtime
// directory is private already; this also covers a socket exposed elsewhere.
type sameUser struct {
	net.Listener
	uid uint32
}

func (listener sameUser) Accept() (net.Conn, error) {
	for {
		conn, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if uid, err := unixsocket.PeerUID(conn); err == nil && uid == listener.uid {
			return conn, nil
		}
		_ = conn.Close()
	}
}

// browserAccess is the one browser policy for every app transport. A browser
// may sit behind the viewer, an SSH-forwarded socket or a forwarded viewer
// port, so the port is not checked: Origin must match Host, cross-site requests
// are refused, and Host must name loopback, which defeats DNS rebinding.
func browserAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			publicProblem(w, 403, "cross_origin", "Cross-origin access denied")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			publicProblem(w, 403, "cross_site", "Cross-site access denied")
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			publicProblem(w, 403, "invalid_host", "HTTP host denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func publicProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(controlapi.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail})
}
