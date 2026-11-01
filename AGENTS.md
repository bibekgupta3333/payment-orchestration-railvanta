# Repository Guidelines

## Project Structure & Module Organization

Read `idea.md` for the Railvanta product and engineering brief. Design docs are in `docs/` (index: `docs/README.md`), grouped into `backend/`, `frontend/`, and `operations/`; the reference schema is `docs/backend/schema.sql`. Source exists in `backend/`, `frontend/`, and `e2e/`, with CI tooling under `.github/` and `scripts/`; Read `README.md` and `CODECHANGELOG.md` before claiming a gate has passed. Log every change in `CODECHANGELOG.md` and track work in `docs/wbs.md`.

The selected design uses Next.js in `frontend/` and a Go/GORM/SQLite backend in `backend/`. Follow the package boundaries in `docs/architecture.md`. Keep infrastructure in `infrastructure/` and architecture decisions in `docs/decisions/`. Payment execution against the sandbox mock provider, the ledger and the dashboard are implemented; real providers, high availability and cloud infrastructure remain out of scope (see `README.md` and `docs/wbs.md`).

## Build, Test, and Development Commands

The repo is a pnpm monorepo (`pnpm-workspace.yaml`: `frontend`, `e2e`, `packages/*`); the Go backend is driven from root scripts via `go -C backend`. Run everything from the repository root. Install dependencies explicitly; `verifyDepsBeforeRun: false` prevents Go commands from triggering implicit JS installs. Go 1.26 and a C compiler are required.

- `pnpm install`: install JS dependencies; use `--frozen-lockfile` after the lockfile is updated.
- `pnpm deps:api`: resolve Go dependencies and checksums.
- `pnpm generate:api` / `pnpm check:generated`: generate/check OpenAPI-derived frontend contracts.
- `pnpm test:api:foundation`: run config and exact-money tests without third-party Go dependencies.
- `APP_ENV=sandbox pnpm dev:api` / `pnpm dev:web`: run the Go API / Next.js dev server. See the root README for the proxy and loopback demo configuration.
- `pnpm build`, `pnpm lint`, `pnpm typecheck`, `pnpm format`: whole-repo build and checks.
- `pnpm test` (`test:api` + `test:web`): Go tests and frontend unit tests.
- `pnpm test:e2e` / `pnpm test:e2e:ci`: Playwright black-box suite in `e2e/` against a freshly started API (and Next.js for UI specs); `:ci` disables retries. With `E2E_BASE_URL` set (Compose stack from `docker-compose.yml` + `docker-compose.e2e.yml`) it runs in container mode and also runs `e2e/ops`.
- `pnpm test:e2e:quickstart`: opt-in and slow; re-runs the README quickstart (between its `quickstart:` markers) in a fresh temporary clone, then the smoke and central-demo specs. Needs network.
- `pnpm check:docs`: every `/docs` example validates against `backend/api/openapi.yaml`; `pnpm lint:openapi` lints the contract.
- `pnpm check`: tracked-file policy, lint, generated contracts, typecheck, tests and build; run before every PR.
- `pnpm compose:config`: validate the Compose definition (also part of `pnpm check`).
- `pnpm compose:up` / `pnpm compose:down`: build/start or stop separate API/frontend images and the proxy; the SQLite volume is retained.
- Docker Compose is the required container runtime. Verify image builds/startup/persistence separately from YAML validation. Backup, restore, drill and secret-scan scripts (`scripts/backup-db.sh`, `restore-backup.sh`, `restore-drill.sh`, `secret-scan.sh`) are described in `docs/operations/deployment.md`; `scripts/validate-k8s.sh` checks the optional Kubernetes manifests.

## Coding Style & Naming Conventions

Use TypeScript for frontend code and Go for the backend. Use two-space indentation for TypeScript and `gofmt` for Go. Use PascalCase for components and classes, camelCase for functions and variables, and descriptive domain names. Keep provider adapters separate from payment-domain logic. Configure automated formatting and linting during scaffolding.

## Testing Guidelines

Use Vitest and React Testing Library for component tests, Playwright for end-to-end flows, and Go tests with temporary file-backed SQLite databases for backend integration tests. Suggested names are `*.test.tsx`, `*.spec.ts` (in `e2e/api/` or `e2e/ui/`), and `*_test.go`, respectively. Every WBS work package ends with its E2E spec, and the full `pnpm test:e2e` suite must pass before the package is done. No coverage threshold exists yet. Prioritize idempotency, concurrent requests, duplicate webhooks, ambiguous provider outcomes, and balanced ledger entries. Report actual test results.

## Commit & Pull Request Guidelines

Add an entry to `CODECHANGELOG.md` for every change: WBS items, files changed, and the actual verification commands and results. A Claude Code `Stop`/`SubagentStop` hook (`.claude/hooks/require-docs.sh`) blocks an agent from finishing while non-doc files changed without a changelog entry; update affected docs (`docs/wbs.md`, `docs/*`, `README.md`) at the same time.

There is no commit history or established message convention. Use concise, imperative subjects such as `Add payment state transitions`. Keep changes focused. PRs should describe behavior, reference relevant requirements or issues, include validation results, and attach screenshots for UI changes.

## Security & Financial Correctness

Default to sandbox payments. Keep secrets out of Git and browser bundles; never store raw payment credentials. Represent money using integer minor units or justified exact decimals. Never automatically retry an ambiguous money-moving operation against another provider. Label seeded or simulated metrics clearly.
