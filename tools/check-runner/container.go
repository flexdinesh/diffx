package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	imageRepository = "diffx-check"
	projectPrefix   = "diffx-check-s"
	cacheVolume     = "diffx-check-gocache"
	projectLabel    = "com.docker.compose.project"
	// groupConcurrency bounds groups per run, host groups included. Each
	// container group is limited to two CPUs by compose.yaml.
	groupConcurrency = 3
	teardownTimeout  = 30 * time.Second
)

// targetParents mirrors the Dockerfile stage graph for image targets.
var targetParents = map[string]string{
	"source":       "",
	"built":        "source",
	"built-docs":   "built",
	"desktop":      "built",
	"desktop-docs": "built-docs",
	"site":         "",
}

// targetOrder builds shared ancestors before their descendants.
var targetOrder = []string{"source", "built", "built-docs", "desktop", "desktop-docs", "site"}

// sourceVariant names the group whose input predicate defines a target's
// container source, so container inputs and fingerprints share one rule.
func sourceVariant(target string) string {
	switch target {
	case "built-docs", "desktop-docs":
		return "contracts"
	case "site":
		return "docs"
	default:
		return "go"
	}
}

// dockerCommand is the narrow Docker surface used for inspection and cleanup.
type dockerCommand func(context.Context, ...string) ([]byte, error)

func dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Env = checkEnvironment(os.Environ())
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// checkEnvironment keeps host Compose settings and check variables out of
// runner invocations; check-runner passes project, file and image explicitly.
func checkEnvironment(inherited []string) []string {
	environment := make([]string, 0, len(inherited))
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "COMPOSE_") || key == "DIFFX_CHECK_IMAGE" || key == "DIFFX_VALIDATION_EXECUTION" {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

func imageTag(slot int, target string) string {
	return fmt.Sprintf("%s:s%d-%s", imageRepository, slot, target)
}

func projectName(slot int, group string) string {
	return fmt.Sprintf("%s%d-%s", projectPrefix, slot, group)
}

func verifyDocker(ctx context.Context, docker dockerCommand) error {
	if _, err := docker(ctx, "compose", "version"); err != nil {
		return fmt.Errorf("isolated checks need Docker with Compose v2: %w", err)
	}
	if _, err := docker(ctx, "buildx", "version"); err != nil {
		return fmt.Errorf("isolated checks need Docker Buildx: %w", err)
	}
	return nil
}

func ensureCacheVolume(ctx context.Context, docker dockerCommand) error {
	_, err := docker(ctx, "volume", "create", "--label", "io.diffx.check=cache", cacheVolume)
	return err
}

func dockerLines(output []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// removeProject removes one compose project's containers, networks and
// project volumes by label. The external shared cache has no project label.
func removeProject(ctx context.Context, docker dockerCommand, project string) error {
	filter := "label=" + projectLabel + "=" + project
	var errs []error
	for _, kind := range [][]string{{"container", "rm", "--force"}, {"network", "rm"}, {"volume", "rm", "--force"}} {
		list := []string{kind[0], "ls", "--quiet", "--filter", filter}
		if kind[0] == "container" {
			list = []string{"container", "ls", "--all", "--quiet", "--filter", filter}
		}
		output, err := docker(ctx, list...)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ids := dockerLines(output); len(ids) > 0 {
			if _, err := docker(ctx, append(kind, ids...)...); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// teardownProject runs on success, failure and cancellation, so it uses its
// own deadline instead of the invocation context.
func teardownProject(docker dockerCommand, project string) error {
	ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	return removeProject(ctx, docker, project)
}

// sweepSlot removes resources left by a dead run in this slot. Holding the
// slot lock guarantees no live run owns them.
func sweepSlot(ctx context.Context, docker dockerCommand, slot int) error {
	prefix := fmt.Sprintf("%s%d-", projectPrefix, slot)
	var projects []string
	for _, list := range [][]string{
		{"container", "ls", "--all", "--filter", "label=" + projectLabel, "--format", "{{.Label \"" + projectLabel + "\"}}"},
		{"network", "ls", "--filter", "label=" + projectLabel, "--format", "{{.Label \"" + projectLabel + "\"}}"},
	} {
		output, err := docker(ctx, list...)
		if err != nil {
			return err
		}
		for _, project := range dockerLines(output) {
			if strings.HasPrefix(project, prefix) && !slices.Contains(projects, project) {
				projects = append(projects, project)
			}
		}
	}
	var errs []error
	for _, project := range projects {
		errs = append(errs, removeProject(ctx, docker, project))
	}
	errs = append(errs, untagSlotImages(ctx, docker, slot))
	return errors.Join(errs...)
}

// untagSlotImages drops this slot's tags; layers remain in the build cache.
func untagSlotImages(ctx context.Context, docker dockerCommand, slot int) error {
	output, err := docker(ctx, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", imageRepository)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s:s%d-", imageRepository, slot)
	var tags []string
	for _, tag := range dockerLines(output) {
		if strings.HasPrefix(tag, prefix) {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return nil
	}
	_, err = docker(ctx, append([]string{"image", "rm"}, tags...)...)
	return err
}

// prepareContainers verifies Docker, removes this slot's stale resources and
// materializes build contexts. The caller removes the returned directory.
func prepareContainers(ctx context.Context, slot int, snapshot string) (string, error) {
	if err := verifyDocker(ctx, dockerOutput); err != nil {
		return "", err
	}
	if err := ensureCacheVolume(ctx, dockerOutput); err != nil {
		return "", err
	}
	if err := sweepSlot(ctx, dockerOutput, slot); err != nil {
		return "", err
	}
	mise, err := exec.LookPath("mise")
	if err != nil {
		return "", err
	}
	if mise, err = filepath.EvalSymlinks(mise); err != nil {
		return "", err
	}
	// A sibling of the snapshot keeps hard links on one filesystem.
	contexts, err := os.MkdirTemp(filepath.Dir(snapshot), "diffx-check-contexts-")
	if err != nil {
		return "", err
	}
	if err := materializeContexts(snapshot, contexts, mise); err != nil {
		_ = os.RemoveAll(contexts)
		return "", err
	}
	return contexts, nil
}

func isManifest(path string) bool {
	switch path {
	case "mise.toml", "mise.lock", ".node-version", ".npmrc", "pnpm-lock.yaml", "pnpm-workspace.yaml":
		return true
	}
	switch filepath.Base(path) {
	case "package.json", "go.mod", "go.sum":
		return true
	}
	return false
}

// contextNames are the named build contexts the Dockerfile consumes.
var contextNames = []string{"manifests", "core", "extra", "site-source", "site-manifests", "mise"}

// contextTree nests source files below each context root. BuildKit applies a
// .dockerignore at a context root, and the repository's own .dockerignore (for
// the server image) would otherwise filter the check sources.
const contextTree = "tree"

// materializeContexts hard-links snapshot files into one directory per named
// build context, selected by the same predicate as group fingerprints.
func materializeContexts(snapshot, destination, mise string) error {
	for _, name := range contextNames {
		directory := filepath.Join(destination, name, contextTree)
		if name == "mise" {
			directory = filepath.Join(destination, name)
		}
		// Empty contexts still need their tree for COPY and bind sources.
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(destination, "main"), 0o755); err != nil {
		return err
	}
	err := filepath.WalkDir(snapshot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(snapshot, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		core := groupInput(sourceVariant("built"), name)
		site := groupInput(sourceVariant("site"), name)
		var targets []string
		if core {
			targets = append(targets, "core")
			if isManifest(name) {
				targets = append(targets, "manifests")
			}
		} else if groupInput(sourceVariant("built-docs"), name) {
			targets = append(targets, "extra")
		}
		if site {
			targets = append(targets, "site-source")
			if isManifest(name) {
				targets = append(targets, "site-manifests")
			}
		}
		for _, target := range targets {
			if err := linkFile(path, filepath.Join(destination, target, contextTree, filepath.FromSlash(name))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return linkFile(mise, filepath.Join(destination, "mise", "mise"))
}

func linkFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if err := os.Link(source, destination); err == nil {
		return nil
	}
	// Different filesystems cannot share inodes; copy instead.
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func buildImage(ctx context.Context, slot int, target, snapshot, contexts string, log *os.File) error {
	args := []string{"docker", "build", "--progress=plain",
		"--file", filepath.Join(snapshot, "tools", "check-runner", "Dockerfile"),
		"--target", target, "--tag", imageTag(slot, target)}
	for _, name := range contextNames {
		args = append(args, "--build-context", name+"="+filepath.Join(contexts, name))
	}
	args = append(args, filepath.Join(contexts, "main"))
	return runCommand(ctx, contexts, checkEnvironment(os.Environ()), args, log, log)
}

// groupScript runs each command with its own log, then rejects drift in
// checked-in generated artifacts and collects browser results.
func groupScript(group checkGroup) string {
	var script strings.Builder
	script.WriteString("mkdir -p /artifacts\nstatus=0\n")
	for index, command := range group.Commands {
		fmt.Fprintf(&script, "if [ \"$status\" -eq 0 ]; then\n mise exec -- %s 2>&1 | tee /artifacts/%02d.log\n status=${PIPESTATUS[0]}\nfi\n", shellArgs(command), index)
	}
	// Preserve the original baseline: builds/generators must not silently change
	// checked-in generated artifacts, even in a successful consumer group.
	script.WriteString("git status --porcelain --untracked-files=all -- packages/api/src/schema.d.ts packages/api/goclient/client.gen.go packages/console-api/src/schema.d.ts packages/plugin-opencode/dist packages/plugin-pi/dist internal/pgstore/pgdb > /artifacts/generated-drift.log\nif [ -s /artifacts/generated-drift.log ]; then cat /artifacts/generated-drift.log; status=1; fi\n")
	script.WriteString("for app in web console desktop; do if [ -d apps/$app/test-results ]; then cp -R apps/$app/test-results /artifacts/$app; fi; done\nexit \"$status\"\n")
	return script.String()
}

func shellArgs(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}

// runGroup executes one group in its own compose project and always removes
// the project afterwards. Artifacts are copied out with the caller's ownership.
func runGroup(ctx context.Context, docker dockerCommand, slot int, group checkGroup, invocation, snapshot, artifacts string) (err error) {
	project := projectName(slot, group.Name)
	container := project + "-runner"
	directory := filepath.Join(artifacts, group.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(directory, "container.log")
	log, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, log.Close(), teardownProject(docker, project))
	}()
	service := "runner"
	if group.Database {
		service = "runner-db"
	}
	args := []string{"docker", "compose", "--file", filepath.Join(snapshot, "tools", "check-runner", "compose.yaml"),
		"--project-name", project, "run", "--name", container, "--no-TTY", "--interactive=false"}
	if group.Name == "desktop-window" {
		args = append(args, "--env", "GDK_BACKEND=x11")
	}
	args = append(args, service, "bash", "-c", groupScript(group))
	environment := append(checkEnvironment(os.Environ()),
		"DIFFX_CHECK_IMAGE="+imageTag(slot, group.Target),
		"DIFFX_VALIDATION_EXECUTION="+invocation+"-"+group.Name)
	runErr := runCommand(ctx, snapshot, environment, args, log, log)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	copyCtx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	_, copyErr := docker(copyCtx, "container", "cp", container+":/artifacts/.", directory)
	if runErr != nil {
		return errors.Join(fmt.Errorf("checks failed: %w (log: %s)", runErr, logPath), copyErr)
	}
	return copyErr
}
