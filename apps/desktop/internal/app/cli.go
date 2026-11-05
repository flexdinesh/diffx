package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/flexdinesh/diffx/apps/desktop/internal/session"
	"github.com/flexdinesh/diffx/packages/localprotocol"
)

var Version = "dev"

// HandleCommand runs before platform initialization.
func HandleCommand(args []string, output io.Writer) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	switch args[0] {
	case "--desktop-contract":
		sidecar := os.Getenv("DIFFX_DESKTOP_SIDECAR")
		if sidecar == "" {
			executable, err := os.Executable()
			if err != nil {
				return true, err
			}
			sidecar, err = session.ResolveSidecar(executable)
			if err != nil {
				return true, err
			}
		}
		return true, json.NewEncoder(output).Encode(localprotocol.DesktopContract{ProtocolVersion: localprotocol.Version, Build: Version, Sidecar: sidecar})
	case "--version":
		_, err := fmt.Fprintln(output, "diffx-desktop "+Version)
		return true, err
	case "--help", "-h":
		_, err := fmt.Fprint(output, `Usage: diffx-desktop [PATH] [LOCAL OPTIONS]
       diffx-desktop --path DIR [--path DIR...] [--scan-depth N]
       git diff | diffx-desktop

Open the shared Diffx review workspace in a desktop window.
Local comparison, configuration and reset options pass to the bundled matching CLI.
Another scope requires explicit --replace or confirmation in the desktop window.

  --desktop-launch  Attach to a running local session, otherwise choose a folder.
  --help, -h        Print usage.
  --version         Print the desktop build version.

DIFFX_DESKTOP_SIDECAR selects an absolute matching sidecar path for development.
Linux requires WebKitGTK. The shared web target requires Safari 16.4 or newer.
`)
		return true, err
	}
	return false, nil
}
