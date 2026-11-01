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
  - Go tests and vet: `mise run check:go`
  - TypeScript, lint, formatting: `mise run check:static`
  - Web/console unit, DOM and plugins: `mise run check:unit`
  - Web/console browser: `mise run check:browser`
  - API contract: `mise run check:conformance`; boundaries and design tokens: `mise run check:contracts`
  - Docs site: `mise run check:docs`
- `mise run check` validates working-file snapshots: static checks run on the host; tests and their services run in isolated Linux containers. Public mise and pnpm test tasks route through Dagger. `*:exec:*` tasks/scripts are internal execution commands, not public test entry points.
- Development, formatting, format verification, lint, type checking and read-only architecture guards stay on the host. Static aggregate checks use a private temporary snapshot and dependencies, without Docker. Go vet stays with Go tests because it requires embedded build assets.
- Before creating or updating a PR, commit changes and run `mise run check:pr` on clean HEAD. Matching successful group results are reused; use `mise run check:pr --force` to execute checks again.
- Run long checks in the background and wait for their completion notification; never poll logs or `sleep`.
- Keep all checks and test suites local through explicit PR validation. GitHub Actions publishes edge and stable releases; it does not validate PRs. Release compilation, checksums and Homebrew formula validation remain in publishing workflows.
- Use only the explicitly managed `diffx-validation-engine-v0.21.11` engine. Run `mise run check:engine:setup` once. Never stop, restart, remove or prune another task's engine or volumes; cancellation closes only this invocation's session. Keep `DAGGER_LEAVE_OLD_ENGINE=true` and never fall back to automatic provisioning.
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

- Pushes do not run the full suite. Validate clean committed HEAD with `mise run check:pr` before creating or updating a PR, then run `mise run check:status` immediately before submission and confirm HEAD has not changed. Report executed/reused results. PR creation remains separate. `check:push` is a compatibility alias for explicit `check:pr`.
