package localserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flexdinesh/diffx/internal/controlapi"
	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/internal/unixsocket"
)

var ErrUnavailable = errors.New("service unavailable; another diffx, possibly an older version, owns the runtime directory but its socket does not answer. Stop it and retry")

type Client struct {
	RuntimeDirectory string
}

func NewClient() (*Client, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return nil, err
	}
	return &Client{RuntimeDirectory: dir}, nil
}

// HTTPClient reaches the running instance's socket. Any response there comes
// from the ownership holder, because only it binds the socket.
func (client *Client) HTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: unixsocket.Dialer(SocketPath(client.RuntimeDirectory)), ResponseHeaderTimeout: 30 * time.Second}}
}

func (client *Client) control() *controlapi.Client {
	return controlapi.NewClient(client.HTTPClient())
}

func (client *Client) probe(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{State: "unavailable"}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	status, controlErr := client.control().Status(probeCtx)
	if controlErr == nil {
		status.Settings = normalizeSettings(status.Settings)
		return status, nil
	}
	if err := ctx.Err(); err != nil {
		return Status{State: "unavailable"}, err
	}
	// Another discovery probe can hold ownership briefly while proving stopped.
	// Confirm contention persists before treating it as an unresponsive local server.
	var lock *processlock.Lock
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		lock, err = processlock.TryAcquire(OwnershipPath(client.RuntimeDirectory))
		if !errors.Is(err, processlock.ErrLocked) {
			break
		}
		select {
		case <-ctx.Done():
			return Status{State: "unavailable"}, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	if errors.Is(err, processlock.ErrLocked) {
		return Status{State: "unavailable"}, ErrUnavailable
	}
	if err != nil {
		return Status{State: "unavailable"}, err
	}
	if err := lock.Close(); err != nil {
		return Status{State: "unavailable"}, err
	}
	return Status{State: "stopped"}, nil
}

func (client *Client) Status(ctx context.Context) (Status, error) {
	return client.probe(ctx)
}

func (client *Client) Stop(ctx context.Context) error {
	lock, err := client.lifecycleLock(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	return client.stop(ctx)
}

func (client *Client) lifecycleLock(ctx context.Context) (*processlock.Lock, error) {
	if _, err := prepareRuntime(client.RuntimeDirectory); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := processlock.TryAcquire(LifecyclePath(client.RuntimeDirectory))
		if !errors.Is(err, processlock.ErrLocked) {
			return lock, err
		}
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func (client *Client) stop(ctx context.Context) error {
	status, err := client.probe(ctx)
	if err != nil {
		return err
	}
	if status.State == "stopped" {
		return nil
	}
	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.control().Shutdown(stopCtx); err != nil {
		return err
	}
	for {
		lock, err := processlock.TryAcquire(OwnershipPath(client.RuntimeDirectory))
		if err == nil {
			return lock.Close()
		}
		if !errors.Is(err, processlock.ErrLocked) {
			return err
		}
		if err := pause(stopCtx); err != nil {
			return fmt.Errorf("service shutdown timed out: %w", err)
		}
	}
}
