# AGENTS.md

One product, multi-project repo. Go CLI/server module at the root (`cmd/`,
`internal/`, `tools/`); embedded React/Vite app in `apps/web/`; OpenAPI contract
(Go embed + generated TS client) in `packages/api/`; agent plugins in
`packages/plugin-*`; user docs site in `docs-website/`; maintainer docs in
`docs/`; cross-package conformance, contract and release tests in `test/`.

- Keep changes minimal. Follow existing patterns.
- Preserve TypeScript type safety: no `any`, type assertions, or non-null assertions.
- Use pnpm. Install dependencies with `pnpm install --frozen-lockfile`.
- Use mise tasks or pnpm scripts for project commands; toolchain and orchestration live in `mise.toml`.
- Run relevant checks for changes; `mise run check` runs all checks.
- Keep full suites and architecture/design guards local through the pre-push hook; keep CI/release verification lightweight.
- `internal/webui/dist` is ignored on `main`; run `mise run web:stage` before Go builds/tests. CI commits it only on release tags and `edge`.
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

- Before pushing to any remote, run all CI checks locally with `mise run check`; resolve failures.
