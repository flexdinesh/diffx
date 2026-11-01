package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
)

const runnerImage = "debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3"
const postgresImage = "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5"

type pipeline struct {
	client *dagger.Client
	root   string
}

func newPipeline(client *dagger.Client, root string) *pipeline { return &pipeline{client, root} }

func (p *pipeline) source(group string) (*dagger.Directory, error) {
	return p.client.Host().Directory(p.root, dagger.HostDirectoryOpts{Exclude: groupExclusions(group)}), nil
}

func (p *pipeline) prepared(group string) (*dagger.Container, error) {
	source, err := p.source(group)
	if err != nil {
		return nil, err
	}
	mise, err := exec.LookPath("mise")
	if err != nil {
		return nil, err
	}
	mise, err = filepath.EvalSymlinks(mise)
	if err != nil {
		return nil, err
	}
	base := p.client.Container(dagger.ContainerOpts{Platform: "linux/amd64"}).From(runnerImage).
		WithEnvVariable("DEBIAN_FRONTEND", "noninteractive").
		WithExec([]string{"sh", "-ec", "apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git bash build-essential ruby unzip xz-utils && rm -rf /var/lib/apt/lists/*"}).
		WithFile("/usr/local/bin/mise", p.client.Host().File(mise)).
		WithEnvVariable("MISE_YES", "1").WithEnvVariable("HUSKY", "0").
		WithEnvVariable("GOTOOLCHAIN", "local").WithEnvVariable("GOMAXPROCS", "2").
		WithEnvVariable("GOFLAGS", "-p=2").WithEnvVariable("pnpm_config_workspace_concurrency", "1").
		WithEnvVariable("DAGGER_LEAVE_OLD_ENGINE", "true").
		WithWorkdir("/workspace")
	// Installation inputs exclude application code. Include every package
	// manifest and shared workspace/tool configuration before installing.
	manifest := source.Filter(dagger.DirectoryFilterOpts{Include: []string{"mise.toml", "mise.lock", ".node-version", ".npmrc", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "**/package.json", "go.mod", "go.sum"}})
	base = base.WithDirectory("/workspace", manifest).
		WithExec([]string{"mise", "trust", "--all"}).
		WithExec([]string{"mise", "install", "--locked"}).
		WithExec([]string{"mise", "exec", "--", "pnpm", "install", "--frozen-lockfile"}).
		WithExec([]string{"mise", "exec", "--", "go", "mod", "download"}).
		WithDirectory("/workspace", source).
		WithEnvVariable("GIT_AUTHOR_NAME", "Validation").WithEnvVariable("GIT_AUTHOR_EMAIL", "validation@localhost").
		WithEnvVariable("GIT_COMMITTER_NAME", "Validation").WithEnvVariable("GIT_COMMITTER_EMAIL", "validation@localhost").
		WithEnvVariable("GIT_AUTHOR_DATE", "2000-01-01T00:00:00Z").WithEnvVariable("GIT_COMMITTER_DATE", "2000-01-01T00:00:00Z").
		WithExec([]string{"sh", "-ec", "git init -q && git add --all --force . ':!node_modules' ':!**/node_modules' && git -c core.hooksPath=/dev/null commit -qm baseline"})
	return base, nil
}

func (p *pipeline) execute(ctx context.Context, group checkGroup, invocation, artifacts string) error {
	container, err := p.prepared(group.Name)
	if err != nil {
		return err
	}
	if group.Build {
		container = container.WithExec([]string{"mise", "run", "--force", "build"}).
			WithExec([]string{"mise", "run", "--force", "plugins:build"})
	}
	if group.Browser {
		container = container.WithExec([]string{"mise", "exec", "--", "pnpm", "--filter", "@diffx/web", "exec", "playwright", "install", "--with-deps", "chromium"})
	}
	identity := invocation + "-" + group.Name
	container = container.WithEnvVariable("DIFFX_VALIDATION_EXECUTION", identity).
		WithEnvVariable("GOFLAGS", "-p=2 -count=1").
		WithEnvVariable("DIFFX_FIXTURE_BINARY", "/workspace/dist/diffx").
		WithEnvVariable("DIFFX_FIXTURE_SERVER_BINARY", "/workspace/dist/diffx-server").
		WithEnvVariable("DIFFX_FIXTURE_CONSOLE_BINARY", "/workspace/dist/diffx-console").
		WithEnvVariable("DIFFX_FIXTURE_WEB_DIR", "/workspace/apps/web/dist").
		WithEnvVariable("DIFFX_CHECK_RUNNER", "1")
	if group.Database {
		database := p.client.Container().From(postgresImage).
			WithEnvVariable("POSTGRES_USER", "diffx").WithEnvVariable("POSTGRES_PASSWORD", "diffx").
			WithEnvVariable("POSTGRES_DB", "postgres").WithEnvVariable("DIFFX_VALIDATION_EXECUTION", identity).
			WithExposedPort(5432).AsService()
		container = container.WithServiceBinding("postgres", database).
			WithEnvVariable("DIFFX_TEST_DATABASE_URL", "postgres://diffx:diffx@postgres:5432/postgres?sslmode=disable")
	}
	var script strings.Builder
	script.WriteString("mkdir -p /artifacts\nstatus=0\n")
	for index, command := range group.Commands {
		fmt.Fprintf(&script, "if [ \"$status\" -eq 0 ]; then\n mise exec -- %s 2>&1 | tee /artifacts/%02d.log\n status=${PIPESTATUS[0]}\nfi\n", shellArgs(command), index)
	}
	// Preserve the original baseline: builds/generators must not silently change
	// checked-in generated artifacts, even in a successful consumer group.
	script.WriteString("git status --porcelain --untracked-files=all -- packages/api/src/schema.d.ts packages/console-api/src/schema.d.ts packages/plugin-opencode/dist packages/plugin-pi/dist internal/pgstore/pgdb > /artifacts/generated-drift.log\nif [ -s /artifacts/generated-drift.log ]; then cat /artifacts/generated-drift.log; status=1; fi\n")
	script.WriteString("for app in web console; do if [ -d apps/$app/test-results ]; then cp -R apps/$app/test-results /artifacts/$app; fi; done\nexit \"$status\"\n")
	result := container.WithExec([]string{"bash", "-c", script.String()}, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	code, err := result.ExitCode(ctx)
	if err != nil {
		return err
	}
	_, exportErr := result.Directory("/artifacts").Export(ctx, filepath.Join(artifacts, group.Name))
	if code != 0 {
		return errors.Join(fmt.Errorf("checks exited %d", code), exportErr)
	}
	return exportErr
}

func shellArgs(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
