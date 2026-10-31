# AGENTS.md

One product, multi-project repo. Go CLI/server module at the root (`cmd/`,
`internal/`, `tools/`); embedded React/Vite app in `apps/web/` and admin
console in `apps/console/`; OpenAPI contracts (Go embed + generated TS client)
in `packages/api/` and `packages/console-api/`; shared React design
system in `packages/ui/`; agent plugins in `packages/plugin-*`; user docs site
in `docs-website/`; maintainer docs in `docs/`; cross-package conformance,
contract and release tests in `test/`.

- Keep changes minimal. Follow existing patterns.
- Preserve TypeScript type safety: no `any`, type assertions, or non-null assertions.
- Use pnpm. Install dependencies with `pnpm install --frozen-lockfile`.
- Use mise tasks or pnpm scripts for project commands; toolchain and orchestration live in `mise.toml`.
- While iterating, run only the checks for what changed:
  - Go package: `go test ./internal/<pkg>/...`
  - TypeScript, lint, formatting: `mise run check:static`
  - Web unit/DOM or browser: `pnpm --filter @diffx/web test:unit` or `test:browser`
  - Console: `pnpm --filter @diffx/console test`
  - Plugins: `pnpm --filter @diffx/plugin-<name> test`
  - API contract: `mise run test:conformance`; boundaries and design tokens: `mise run test:contracts:rules`
  - Docs site: `mise run docs:check`
- `mise run check` runs every check and prints one line per phase; a failure prints the log tail, log path and rerun command.
- Run long checks in the background and wait for their completion notification; never poll logs or `sleep`.
- Keep full suites and architecture/design guards local through the pre-push hook; keep CI/release verification lightweight.
- `internal/webui/dist` and `internal/consoleui/dist` are ignored on `main`; run `mise run web:stage` and `mise run console:stage` (or `mise run build`) before Go builds/tests. CI commits only the web assets, on release tags and `edge`.
- See `docs/development.md` and `docs/release.md` for workflows.

## Design and architecture

- Before editing, read `docs/architecture.md` and relevant `docs/system.md` sections.
- For frontend changes, also read `DESIGN.md` and frontend conventions in `docs/development.md`.
- Identify the behavior owner, affected contracts, and applicable invariants before choosing an implementation.
- Extend the owning module; encapsulate behavior behind its public contract.
- Share semantic rules, not merely similar code. Avoid speculative abstractions.
- Compose local/remote policies at entry points; keep mode branches out of domain operations.
- Define interfaces beside consumers; expose only required operations.
- Keep transports thin. Services own application rules; storage owns atomic persistence.
- Preserve ownership, authorization, identity, retry, cancellation, and durability contracts.
- Reuse UI tokens and primitives; preserve Pierre's rendering and measured geometry.
- Verify affected behavior at boundaries, including both modes when shared behavior changes.
- Update canonical docs and checks with intentional design changes; explain the changed requirement.
- Never weaken a check solely to accommodate a violating implementation.
- Before completion, review the diff against applicable design rules; report validation and remaining risks.

## Git etiquette

- Pushing runs every check through the pre-push hook (`mise run check:push`); do not run `mise run check` separately before pushing. Resolve failures.
