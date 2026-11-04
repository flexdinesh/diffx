package app

import (
	"fmt"
	"io"
)

// Version is set by native packaging. Source builds identify themselves as dev.
var Version = "dev"

// HandleCommand handles informational commands before platform initialization.
func HandleCommand(args []string, output io.Writer) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	switch args[0] {
	case "--version":
		_, err := fmt.Fprintln(output, "diffx-native "+Version)
		return true, err
	case "--help", "-h":
		_, err := fmt.Fprint(output, `Usage: diffx-native [PATH] [LOCAL OPTIONS]
       diffx-native --path DIR [--path DIR...] [--scan-depth N]
       git diff | diffx-native

Open the native local review window. With no PATH, review the shell's checkout.
Local input, comparison, configuration and reset options pass to the matching
private Diffx server. Examples: --scan-worktrees, --base REF, --config-file FILE,
--replace, --reset-state. Headless and remote commands are not native workflows.

  --desktop-launch  Attach to a running session or choose a folder. Does not
                    discover repositories beneath the launcher's directory.
  --help, -h        Print native usage.
  --version         Print the native build version.

DIFFX_NATIVE_SIDECAR sets an explicit absolute sidecar executable for development
and native acceptance tests. Installed builds otherwise use their bundled server.
`)
		return true, err
	}
	return false, nil
}
