// Command check-boundaries validates production dependencies for the active Go build target.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
)

const modulePrefix = "github.com/flexdinesh/diffx/"

// Exact package paths make new packages, including nested ones, require an explicit
// architectural role. Command/composition roots wire concrete adapters.
var allowed = map[string][]string{
	"cmd/diffx": {
		"internal/browser", "internal/collector", "internal/config", "internal/contextservice",
		"internal/diffsource", "internal/hooks", "internal/ingestion", "internal/localserver",
		"internal/review", "internal/reviewstore", "internal/submission", "internal/version",
	},
	"cmd/diffx-fetcher":       {"internal/config", "internal/fetcher", "internal/fetchprotocol", "internal/version"},
	"cmd/diffx-server":        {"internal/config", "internal/identityservice", "internal/remoteserver", "internal/reviewstore", "internal/version"},
	"internal/browser":        {},
	"internal/collector":      {"internal/diffsource", "internal/ingestion", "internal/review"},
	"internal/config":         {"internal/processlock"},
	"internal/contextservice": {"internal/review", "internal/reviewdata", "internal/reviewsession", "internal/ingestion"},
	"internal/controlapi":     {"internal/config", "internal/review"},
	"internal/diffsource":     {"internal/processlock", "internal/review"},
	// GitHub wire adapter; integration policy lives in githubsync.
	"internal/github": {},
	// Google sign-in wire adapter; account policy lives in identityservice.
	"internal/google":     {},
	"internal/githubsync": {"internal/fetchprotocol", "internal/github", "internal/ingestion", "internal/review"},
	// The fetcher mirrors GitHub repositories and collects them offline.
	"internal/fetcher":       {"internal/collector", "internal/diffsource", "internal/fetchprotocol", "internal/ingestion"},
	"internal/fetchprotocol": {"internal/ingestion"},
	"internal/hooks":         {"internal/ingestion", "internal/processlock", "internal/submission"},
	"internal/httpapi": {
		"internal/review", "internal/reviewdata", "internal/reviewsession", "internal/contextservice",
		"internal/reviewservice", "internal/ingestion", "internal/processmetrics", "packages/api",
	},
	"internal/ingestion":       {"internal/review"},
	"internal/identityservice": {"internal/google"},
	"internal/ingestionqueue":  {"internal/review", "internal/ingestion"},
	"internal/localserver": {
		"internal/config", "internal/contextservice", "internal/controlapi", "internal/ingestion", "internal/processlock",
		"internal/reviewstore", "internal/serverapp", "internal/version", "internal/webui",
	},
	"internal/mcpapi":         {"internal/review", "internal/contextservice", "internal/reviewservice", "internal/ingestion"},
	"internal/processlock":    {},
	"internal/processmetrics": {},
	"internal/remoteserver": {
		"internal/contextservice", "internal/fetchprotocol", "internal/github", "internal/githubsync", "internal/google", "internal/identityservice", "internal/ingestion", "internal/ingestionqueue", "internal/review",
		"internal/reviewstore", "internal/serverapp", "internal/webui", "packages/api",
	},
	"internal/review":        {},
	"internal/reviewdata":    {"internal/review", "internal/ingestion"},
	"internal/reviewservice": {"internal/review", "internal/reviewdata", "internal/reviewsession", "internal/contextservice", "internal/ingestion"},
	"internal/reviewsession": {"internal/review"},
	"internal/reviewstore":   {"internal/githubsync", "internal/google", "internal/identityservice", "internal/review", "internal/reviewdata", "internal/ingestion", "internal/ingestionqueue", "internal/processlock"},
	"internal/serverapp": {
		"internal/contextservice", "internal/httpapi", "internal/ingestion", "internal/ingestionqueue",
		"internal/mcpapi", "internal/review", "internal/reviewdata", "internal/reviewservice", "internal/version",
	},
	"internal/submission": {"internal/ingestion", "internal/processlock"},
	// Test helpers are classified, but production packages cannot import them.
	"internal/testsupport": {
		"internal/contextservice", "internal/identityservice", "internal/ingestion", "internal/ingestionqueue", "internal/review",
		"internal/reviewstore", "internal/serverapp", "internal/webui",
	},
	"internal/version": {},
	"internal/webui":   {},
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func checkPackage(pkg listedPackage) []string {
	name := strings.TrimPrefix(pkg.ImportPath, modulePrefix)
	permitted, checked := allowed[name]
	if !checked {
		return []string{fmt.Sprintf("unclassified package: %s; assign an architecture boundary", name)}
	}
	var violations []string
	for _, dependency := range pkg.Imports {
		if dependency == "os/exec" && !slices.Contains([]string{
			"internal/browser", "internal/diffsource", "internal/fetcher", "internal/hooks", "internal/localserver",
		}, name) {
			violations = append(violations, fmt.Sprintf("%s must not execute processes", name))
		}
		if (dependency == "database/sql" || dependency == "modernc.org/sqlite" || strings.HasPrefix(dependency, "modernc.org/sqlite/")) && name != "internal/reviewstore" {
			violations = append(violations, fmt.Sprintf("%s must not access SQL storage directly: %s", name, dependency))
		}
		if !strings.HasPrefix(dependency, modulePrefix) {
			continue
		}
		dependency = strings.TrimPrefix(dependency, modulePrefix)
		if dependency == "internal/testsupport" || strings.HasPrefix(dependency, "internal/testsupport/") {
			violations = append(violations, fmt.Sprintf("production dependency on test helpers: %s -> %s", name, dependency))
		} else if !slices.Contains(permitted, dependency) {
			violations = append(violations, fmt.Sprintf("forbidden dependency: %s -> %s", name, dependency))
		}
	}
	return violations
}

func checkPackages(input io.Reader, diagnostics io.Writer) (bool, error) {
	decoder := json.NewDecoder(input)
	passed := true
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			return passed, nil
		} else if err != nil {
			return false, err
		}
		for _, violation := range checkPackage(pkg) {
			fmt.Fprintln(diagnostics, violation)
			passed = false
		}
	}
}

func main() {
	command := exec.Command("go", "list", "-e", "-json", "./cmd/...", "./internal/...")
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	passed, err := checkPackages(bytes.NewReader(output), os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !passed {
		os.Exit(1)
	}
	fmt.Println("Architecture boundaries passed")
}
