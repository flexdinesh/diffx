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
		"internal/diagnostics", "internal/diffsource", "internal/hooks", "internal/ingestion", "internal/localserver",
		"internal/mcpapi", "internal/review", "internal/reviewstore", "internal/sshforward", "internal/submission", "internal/version",
		"packages/localprotocol",
	},
	"cmd/diffx-fetcher": {"internal/config", "internal/fetcher", "internal/fetchprotocol", "internal/version"},
	"cmd/diffx-server":  {"internal/config", "internal/identityservice", "internal/pgstore", "internal/remoteserver", "internal/version"},
	"cmd/diffx-console": {"internal/config", "internal/consoleserver", "internal/identityservice", "internal/version"},
	// HTTP rules shared by the remote server and the console transports.
	"internal/authhttp":  {"internal/identityservice"},
	"internal/browser":   {},
	"internal/collector": {"internal/diffsource", "internal/ingestion", "internal/review"},
	"internal/config":    {"internal/processlock"},
	// Admin console transport and composition over the server's database.
	"internal/consoleserver":  {"internal/authhttp", "internal/consoleui", "internal/google", "internal/identityservice", "internal/pgstore", "internal/serverapp"},
	"internal/consoleui":      {},
	"internal/contextservice": {"internal/review", "internal/reviewdata", "internal/reviewsession", "internal/ingestion"},
	"internal/controlapi":     {"internal/config", "internal/review", "packages/localprotocol"},
	"internal/diffsource":     {"internal/processlock", "internal/review"},
	"internal/diagnostics":    {"internal/processlock"},
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
		"internal/reviewstore", "internal/serverapp", "internal/unixsocket", "internal/version", "internal/webui",
		"packages/localprotocol",
	},
	"internal/mcpapi": {"internal/review", "internal/contextservice", "internal/reviewservice", "internal/ingestion"},
	// Server Postgres persistence; pgdb holds its generated queries.
	"internal/pgstore": {
		"internal/contextservice", "internal/githubsync", "internal/google", "internal/identityservice", "internal/ingestion", "internal/ingestionqueue",
		"internal/pgstore/pgdb", "internal/pushservice", "internal/review", "internal/reviewdata", "internal/reviewservice", "internal/webpush",
	},
	"internal/pgstore/pgdb":   {},
	"internal/processlock":    {},
	"internal/processmetrics": {},
	// Web Push wire adapter: VAPID, payload encryption and response classes.
	"internal/webpush": {},
	// Push rules: enablement, devices and outbox delivery; storage is behind
	// its Store contract and composition is remote only.
	"internal/pushservice": {"internal/webpush"},
	"internal/remoteserver": {
		"internal/authhttp", "internal/contextservice", "internal/fetchprotocol", "internal/github", "internal/githubsync", "internal/google", "internal/identityservice", "internal/ingestion", "internal/ingestionqueue", "internal/pgstore", "internal/pushservice", "internal/review",
		"internal/reviewdata", "internal/serverapp", "internal/webui", "packages/api",
	},
	"internal/review":        {},
	"internal/reviewdata":    {"internal/review", "internal/ingestion"},
	"internal/reviewservice": {"internal/review", "internal/reviewdata", "internal/reviewsession", "internal/contextservice", "internal/ingestion"},
	"internal/reviewsession": {"internal/review"},
	// Local SQLite persistence; server contracts are implemented only by pgstore.
	"internal/reviewstore": {"internal/review", "internal/reviewdata", "internal/ingestion", "internal/processlock"},
	"internal/serverapp": {
		"internal/contextservice", "internal/httpapi", "internal/ingestion", "internal/ingestionqueue",
		"internal/mcpapi", "internal/review", "internal/reviewdata", "internal/reviewservice", "internal/version",
	},
	// Remote access through the system ssh binary; it holds no credentials.
	"internal/sshforward": {},
	"internal/submission": {"internal/ingestion", "internal/processlock"},
	// Unix socket mechanism: bind, dial and peer credentials. Admission policy
	// belongs to the composition that listens.
	"internal/unixsocket": {},
	// Test helpers are classified, but production packages cannot import them.
	// The store contract sees only the consumer interfaces it verifies.
	"internal/storetest": {"internal/contextservice", "internal/ingestion", "internal/review", "internal/reviewdata", "internal/reviewservice"},
	"internal/testsupport": {
		"internal/contextservice", "internal/google", "internal/identityservice", "internal/ingestion", "internal/ingestionqueue", "internal/review",
		"internal/reviewservice", "internal/serverapp", "internal/webui",
	},
	"internal/testsupport/pgtest": {"internal/pgstore"},
	"internal/version":            {},
	"internal/webui":              {},
	// Public wire contracts have no dependency on server implementation packages.
	"packages/api":           {},
	"packages/api/goclient":  {},
	"packages/console-api":   {},
	"packages/localprotocol": {},
	// Desktop is an independent webview host. Only its app imports MyGo;
	// only the session adapter executes the bundled matching server.
	"apps/desktop/cmd/diffx-desktop": {"apps/desktop/internal/app"},
	"apps/desktop/internal/app": {
		"apps/desktop/internal/assets", "apps/desktop/internal/host", "apps/desktop/internal/launcher",
		"apps/desktop/internal/session", "packages/localprotocol",
	},
	"apps/desktop/internal/assets":     {},
	"apps/desktop/internal/launcher":   {"apps/desktop/internal/session", "packages/localprotocol"},
	"apps/desktop/internal/host":       {"apps/desktop/internal/assets", "packages/localprotocol"},
	"apps/desktop/internal/session":    {"packages/api/goclient", "packages/localprotocol"},
	"apps/desktop/tools/stage-desktop": {},
}

// postgresOwners hold every Postgres driver and migration dependency.
var postgresOwners = []string{"internal/pgstore", "internal/pgstore/pgdb", "internal/testsupport/pgtest"}

func postgresDependency(dependency string) bool {
	for _, prefix := range []string{"github.com/jackc/pgx/", "github.com/jackc/pgconn", "github.com/pressly/goose/"} {
		if strings.HasPrefix(dependency, prefix) {
			return true
		}
	}
	return false
}

func testHelper(name string) bool {
	for _, helper := range []string{"internal/storetest", "internal/testsupport"} {
		if name == helper || strings.HasPrefix(name, helper+"/") {
			return true
		}
	}
	return false
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
		if dependency == "github.com/egoist/mygo" || strings.HasPrefix(dependency, "github.com/egoist/mygo/") {
			if name != "apps/desktop/internal/app" {
				violations = append(violations, fmt.Sprintf("%s must not import desktop UI; keep MyGo in apps/desktop presentation packages", name))
			}
		}
		if dependency == "os/exec" && !slices.Contains([]string{
			"internal/browser", "internal/diffsource", "internal/fetcher", "internal/hooks", "internal/localserver",
			"internal/sshforward",
			"apps/desktop/internal/session",
		}, name) {
			violations = append(violations, fmt.Sprintf("%s must not execute processes", name))
		}
		if (dependency == "database/sql" || dependency == "modernc.org/sqlite" || strings.HasPrefix(dependency, "modernc.org/sqlite/")) && name != "internal/reviewstore" {
			violations = append(violations, fmt.Sprintf("%s must not access SQL storage directly: %s", name, dependency))
		}
		if postgresDependency(dependency) && !slices.Contains(postgresOwners, name) {
			violations = append(violations, fmt.Sprintf("%s must not access Postgres storage directly: %s", name, dependency))
		}
		if !strings.HasPrefix(dependency, modulePrefix) {
			continue
		}
		dependency = strings.TrimPrefix(dependency, modulePrefix)
		if testHelper(dependency) {
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
	for _, arguments := range [][]string{
		{"list", "-e", "-json", "./cmd/...", "./internal/...", "./packages/..."},
		{"-C", "apps/desktop", "list", "-mod=readonly", "-e", "-json", "./..."},
	} {
		command := exec.Command("go", arguments...)
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
	}
	fmt.Println("Architecture boundaries passed")
}
