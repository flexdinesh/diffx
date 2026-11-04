// Command check-artifacts rejects committed application build outputs.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if err := checkArtifacts("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkArtifacts(root string) error {
	command := exec.Command("git", "ls-files", "-z")
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("list tracked files: %w", err)
	}
	var forbidden []string
	for _, name := range bytes.Split(data, []byte{0}) {
		if builtArtifact(string(name)) {
			forbidden = append(forbidden, string(name))
		}
	}
	if len(forbidden) != 0 {
		return fmt.Errorf("remove application build outputs from Git; publish release assets instead:\n%s", strings.Join(forbidden, "\n"))
	}
	return nil
}

func builtArtifact(name string) bool {
	for _, directory := range []string{"dist/", "apps/web/dist/", "apps/console/dist/", "internal/webui/dist/", "internal/consoleui/dist/", "docs-website/dist/", "apps/desktop/build/", "apps/desktop/.mygo/", "apps/desktop/test-results/"} {
		if strings.HasPrefix(name, directory) {
			return true
		}
	}
	for _, binary := range []string{"diffx", "diffx-server", "diffx-console", "diffx-fetcher", "diffx-native", "apps/desktop/diffx-native"} {
		if name == binary || name == binary+".exe" {
			return true
		}
	}
	return false
}
