package localserver

import (
	"os"
	"path/filepath"

	"github.com/flexdinesh/diffx/internal/controlapi"
)

type Settings = controlapi.Settings
type Status = controlapi.Status

func normalizeSettings(settings Settings) Settings {
	if settings.RetentionDays == 0 {
		settings.RetentionDays = 7
	}
	return settings
}

func RuntimeDir() (string, error) {
	if dir := os.Getenv("DIFFX_RUNTIME_DIR"); dir != "" {
		return prepareRuntime(dir)
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return prepareRuntime(filepath.Join(dir, "diffx"))
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return prepareRuntime(filepath.Join(state, "diffx", "runtime"))
}

func prepareRuntime(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	if err := protectRuntime(path); err != nil {
		return "", err
	}
	return path, nil
}

func OwnershipPath(dir string) string { return filepath.Join(dir, "daemon.lock") }
func LifecyclePath(dir string) string { return filepath.Join(dir, "lifecycle.lock") }
func LogPath(dir string) string       { return filepath.Join(dir, "daemon.log") }

// SocketPath is the owner-only API, MCP and control socket. Only the holder of
// OwnershipPath binds it, so whoever answers there is the running instance.
func SocketPath(dir string) string { return filepath.Join(dir, "diffx.sock") }
