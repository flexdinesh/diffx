package localserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/controlapi"
	buildversion "github.com/flexdinesh/diffx/internal/version"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

// SessionOptions composes native attachment policy at the local lifecycle
// boundary. Attach selects the current scope without collecting another input.
type SessionOptions struct {
	Attach  bool
	Replace bool
}

func RunSession(ctx context.Context, settings Settings, input Input, policy SessionOptions, reset StateReset, ready func(localprotocol.Readiness) error, stderr io.Writer) error {
	if !policy.Attach {
		var err error
		settings, err = effectiveSettings(settings)
		if err != nil {
			return err
		}
		if settings.Headless {
			return errors.New("desktop sessions require a loopback viewer")
		}
	}
	decide := func(status Status) (bool, error) {
		compatible := compatibleSession(status)
		if policy.Attach {
			return compatible == nil, compatible
		}
		if !policy.Replace {
			if compatible != nil {
				return false, fmt.Errorf("%w; use --replace", compatible)
			}
			if reset.Force || input.Session == nil || input.Session.ScopeID != status.Session.ScopeID {
				return false, errors.New("local session scope conflict; use --replace")
			}
			actual, err := effectiveSettings(status.Settings)
			if err != nil {
				return false, err
			}
			// Zero requests any free port; -1 requests the default range.
			if settings.Port == 0 || settings.Port == -1 && actual.Port >= 7981 && actual.Port <= 7990 {
				actual.Port = settings.Port
			}
			if actual != settings {
				return false, errors.New("local session configuration conflict; use --replace")
			}
			return true, nil
		}
		return false, nil
	}
	return runForeground(ctx, settings, input, decide, policy.Attach, reset, func(status Status, _ Published, owned bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := compatibleSession(status); err != nil {
			return err
		}
		return ready(localprotocol.Readiness{ProtocolVersion: localprotocol.Version, Socket: status.Socket,
			InstanceID: status.InstanceID, ViewerURL: status.ViewerURL, Owned: owned,
			ScopeID: status.Session.ScopeID, Input: status.Session.Input})
	}, stderr)
}

func compatibleSession(status Status) error {
	if status.State != "running" || status.ProtocolVersion != controlapi.ProtocolVersion || status.Version != buildversion.Number() || status.Session == nil || status.Session.ProtocolVersion != localprotocol.Version || status.Session.ScopeID == "" || status.Socket == "" || status.InstanceID == "" {
		return errors.New("running local instance is incompatible with desktop sessions")
	}
	viewer, err := url.Parse(status.ViewerURL)
	if err != nil || viewer.Scheme != "http" || viewer.User != nil || viewer.Path != "" || viewer.RawQuery != "" || viewer.Fragment != "" || viewer.Port() == "" {
		return errors.New("running local instance has no compatible loopback viewer")
	}
	ip := net.ParseIP(viewer.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("running local instance has no compatible loopback viewer")
	}
	switch status.Session.Input.Kind {
	case "checkout", "checkouts":
		if len(status.Session.Input.Paths) == 0 || status.Session.Input.Selection == "" {
			return errors.New("running local instance has incomplete checkout metadata")
		}
	case "capture":
		if status.Session.Input.ContextID == "" {
			return errors.New("running local instance has incomplete capture metadata")
		}
	default:
		return errors.New("running local instance has unsupported input metadata")
	}
	return nil
}

func (client *Client) waitInstance(ctx context.Context, instanceID string) error {
	err := client.control().WaitInstance(ctx, instanceID)
	if ctx.Err() != nil {
		return nil
	}
	// One final read identifies replacement; a broken stream still terminates
	// attachment even if a server answers, rather than silently losing monitoring.
	status, statusErr := client.probe(ctx)
	if ctx.Err() != nil {
		return nil
	}
	if statusErr != nil {
		return fmt.Errorf("attached local instance lost: %w", statusErr)
	}
	if status.State != "running" || status.InstanceID != instanceID {
		return errors.New("attached local instance stopped or was replaced")
	}
	if err != nil {
		return fmt.Errorf("attached local instance disconnected: %w", err)
	}
	return errors.New("attached local instance lifetime stream closed")
}

func effectiveSettings(settings Settings) (Settings, error) {
	settings = normalizeSettings(settings)
	var err error
	if settings.State == "" {
		settings.State, err = config.DefaultStatePath()
		if err != nil {
			return settings, err
		}
	}
	if settings.State != "memory" {
		settings.State, err = canonicalPath(settings.State)
		if err != nil {
			return settings, err
		}
	}
	if settings.WebDir != "" {
		settings.WebDir, err = canonicalPath(settings.WebDir)
	}
	return settings, err
}

// canonicalPath resolves existing ancestors too, since a new state database
// may live below a symlink before the file itself exists.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if os.IsNotExist(err) && filepath.Dir(absolute) != absolute {
		parent, parentErr := canonicalPath(filepath.Dir(absolute))
		if parentErr == nil {
			return filepath.Join(parent, filepath.Base(absolute)), nil
		}
	}
	return "", err
}
