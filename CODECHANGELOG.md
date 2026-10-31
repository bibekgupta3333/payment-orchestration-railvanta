# Code Changelog

A record of every change to code, configuration or design docs, newest first. Each entry says what changed, which [WBS](docs/wbs.md) item it serves, and what was actually run to verify it. Only record results you observed. If a check was skipped or failed, say so.

## Entry template

```md
## YYYY-MM-DD — Short imperative summary

**WBS:** 2.1.3, 2.1.4 · **PR/commit:** <link or hash>

**Changed**
- `path/to/file` — what and why

**Verification**
- `pnpm check` — pass / fail (details)
- `pnpm test:e2e` — N passed, M failed (spec names); include earlier packages' specs
- Manual check — what was observed

**Notes:** follow-ups, known limits, deferred items
```

---

## 2026-10-31 — Commit the foundation work and open the M1 PR

**WBS:** 1.1–1.4, 5.2 (prep) · **PR/commit:** #1 (`feat/m1-foundation`), not merged

**Changed**
- Git history: an empty root commit on `main` serves as the PR base. The previously uncommitted work is split into chronological commits on `feat/m1-foundation` (docs, WBS, monorepo, backend storage, auth/API, frontend, E2E, CI, containers, project docs). Commit dates follow the dates of the entries below.

**Verification**
- `docker compose config -q` — exit 0. No other checks were rerun. The dependency-backed results in the entries below still apply, and CI on the PR is the first full run.

## 2026-10-31 — Add required backend/frontend Docker images and Compose workflow

**WBS:** 1.1.16, 5.2.1–5.2.4 · **PR/commit:** uncommitted

**Changed**
- `infrastructure/docker/{api.Dockerfile,web.Dockerfile,nginx.conf}` — separate multi-stage non-root API/frontend images, private API health check, Next.js standalone assets, and unprivileged same-origin proxy.
- `docker-compose.yml` — image/build definitions, health-ordered startup, host-loopback proxy port, sandbox-only API configuration, and a persistent SQLite volume mounted only by the API. Compose down retains the volume.
- `.dockerignore` — exclude credentials, local database files, dependencies and build/test output from image contexts.
- `frontend/next.config.ts` — standalone output with monorepo tracing root; Docker supplies a nonsecret API rewrite destination at build time.
- `package.json` — `compose:config`, `compose:build`, `compose:up`, `compose:down`, `compose:logs`; `pnpm check` includes daemon-independent Compose validation.
- `idea.md`, `README.md`, `AGENTS.md`, `docs/{architecture.md,README.md,wbs.md,operations/deployment.md}` — require separate images with Compose, explain native development/testing as an alternative, and document unresolved image/runtime gates. M1 now has 51/53 source tasks checked; backup/restore, container E2E, digest pinning and all runtime acceptance gates remain open.

**Verification**
- `pnpm compose:config` — PASS (Docker Compose 2.40.0).
- Parsed resolved Compose JSON — PASS: both separate image/build definitions, no host app/health ports, one API-owned data volume, sandbox settings, health dependency ordering, and loopback-only public origin.
- Local document links, WBS source-task count, and `git diff --check` — PASS.
- `docker info` — FAIL: sandbox denies access to the Docker daemon socket.
- `pnpm compose:build` — FAIL before building: sandbox denies writes to Docker Buildx activity metadata under `~/.docker`. No image-build, Nginx runtime, proxy, persistence or container-E2E pass is claimed.

**Remaining:** finish M1 dependency lock/checksum files, build and start images with Docker access, verify health/routing/volume permissions/persistence, and complete WBS 5.2 recovery and release gates. No Docker settings were changed and no resources were deployed.


## 2026-10-31 — Check implemented M1 tasks while retaining verification gates

**WBS:** 1.1–1.4 checklist · **PR/commit:** uncommitted

**Changed**
- `docs/wbs.md` — at the user's request, task checkboxes now track implementation separately from acceptance/E2E outcomes. Checked 50 of 52 M1 source tasks. Kept 1.1.15 open for passing root checks and 1.3.13 open for missing generated contracts. Added per-package totals; all package verification/merge gates remain open. M2 and later checklists are unchanged.

**Verification**
- Checklist consistency — PASS: 50 implemented M1 tasks; two partial tasks open; no M1 acceptance/E2E outcome marked passed.
- Local WBS links and `git diff --check` — PASS.
- Application tests — not rerun for this documentation-only update.


## 2026-10-31 — Detail M1 implementation and verification status in the WBS

**WBS:** 1.1–1.4 tracking · **PR/commit:** uncommitted

**Changed**
- `docs/wbs.md` — added task-level implementation evidence, recorded pass/fail/blocker results, the remaining root-command completion sequence, and distinct status descriptions for each M1 package. Kept original task/acceptance boxes unchecked because runtime, CI and merge gates remain unmet.

**Verification**
- Local file links in `docs/wbs.md` — PASS.
- `git diff --check` — PASS.
- Application tests — not rerun for this documentation-only update; the WBS labels previous results as historical observations.


## 2026-10-31 — Add M1 foundation source and verification harness

**WBS:** 1.1–1.4 · **PR/commit:** uncommitted; no PR, merge, or green CI claimed

**Authorization**
- The user explicitly requested M1 and all listed schema-review revisions. Items 1.2.1–1.2.8 are authorized for this implementation; no additional approval is needed to finish them. Five specialist agents contributed storage, auth/contracts, frontend, E2E, and CI/tooling, followed by parent integration review.

**Changed**
- `backend/go.mod`, `cmd/api/main.go`, `internal/config/`, and domain package documentation — Go module, explicit sandbox configuration, separate loopback health listener, readiness after migration, and bounded SIGTERM shutdown.
- `backend/internal/storage/`, `backend/migrations/`, and `docs/backend/{schema.sql,data-model.md,persistence.md}` — two embedded migrations, checksummed history, 23 STRICT tables, provider-neutral methods/tokens, durable mock/simulation storage, auth storage, immutable financial/audit records, connection verification, UUIDv7 IDs, explicit GORM models, transaction helper, and file-backed tests.
- `backend/internal/auth/`, `internal/transport/http/`, `internal/payment/`, and `backend/api/openapi.yaml` — hashed scoped session/API-key auth, expiry/revocation, roles, CSRF/origin checks, request/correlation IDs, bounded bodies/rate limits, safe errors/logging, exact money parsing/serialization, workspace/method reads, and self-revocation. Demo bootstrap refuses prefix collisions or silently changed credentials. Valid payment intents return honest `501` until M2.
- `backend/{.env.example,.golangci.yml,Makefile}`, `backend/tests/README.md` — placeholder configuration and formatting/vet/lint/build/test targets.
- `frontend/` — strict Next.js/React/TypeScript/Tailwind workspace, local Inter font, all planned routes plus detail placeholders, responsive accessible shell/dialog navigation, persisted themes, unavailable states, UI primitives, exact BigInt money helpers, Zod client, path-preserving API proxy, and Vitest/RTL tests. No invented financial metrics or enabled payment execution.
- `e2e/` — Playwright workspace with temporary on-disk database, runtime-generated credentials, free loopback ports, private harness-only restart/SIGKILL helpers, HTTP contract checks, controlled stopped-database corruption/durability fixtures, and scaffold/database/auth/responsive/keyboard/axe specs. Assertions use HTTP/browser interfaces; Go exposes no test-control endpoints.
- `package.json`, `pnpm-workspace.yaml`, `scripts/`, `redocly.yaml`, `.github/workflows/ci.yml`, `.gitignore` — root package commands, explicit installs, Go/OpenAPI lint, real contract-generation tooling, required-workspace/tracked-file checks, client-bundle secret sentinels, CI and E2E artifact upload. Ignore local pnpm cache and backend data.
- `README.md`, `AGENTS.md`, `idea.md`, `docs/{README.md,roadmap.md,wbs.md,frontend/design.md,operations/deployment.md}` — actual setup, implementation status, authorization record, checks and limits. WBS packages remain in progress and boxes remain unchecked until verified and merged.

**Verification observed**
- `GOCACHE=/tmp/railvanta-go-cache pnpm test:api:foundation` — PASS: configuration and exact-money test packages, including int64 serialization.
- `pnpm format:api` — PASS: Go source formatted.
- Independent Python/SQLite file-backed execution — PASS: all 22 rejection cases from storage tests, valid seed paths, migration 1→2 preserving existing data, 23 STRICT reference tables, `integrity_check=ok`, and no foreign-key violations. This is not a Go migration-runner pass.
- Go parser — PASS: 20 Go source files parse. TypeScript parser — PASS: 46 TypeScript/TSX/JavaScript source files parse. These are syntax checks, not dependency-backed compilation/typechecking.
- `node --check scripts/*.mjs` (each file checked separately) — PASS. Workspace and tracked-file policy scripts — PASS. OpenAPI YAML parses and internal references resolve across 20 paths; real Redocly lint remains unexecuted.
- Local documentation file links and `git diff --check` — PASS.
- `pnpm install --offline` — FAIL: missing cached Tailwind/PostCSS dependency metadata. Normal registry/Go downloads fail DNS resolution; the workspace permission policy rejected download escalation.
- `pnpm install --no-frozen-lockfile --config.fetchRetries=0` — FAIL: registry fetch for `openapi-typescript` is blocked; no updated lockfile was produced.
- `pnpm install --frozen-lockfile` — FAIL: existing lockfile lacks the new workspace/root dependencies. It must be regenerated with a real dependency install.
- `GOCACHE=/tmp/railvanta-go-cache GOMODCACHE=/tmp/railvanta-go-mod pnpm test:api` and `pnpm check` — FAIL: missing Go dependencies/checksums. The format, required-workspace and tracked-file steps passed before the dependency failure. An initial check without the temporary cache override also hit the sandbox's home-cache write restriction.
- `pnpm test:e2e` — FAIL before suite execution: workspace dependencies are not installed; the available global `playwright` executable does not provide the test runner. No browser test or axe result is claimed.
- Standard API type generator — BLOCKED: `openapi-typescript` is not installed. `frontend/lib/api/schema.d.ts` is not fabricated and remains to be generated.

**Remaining before M1 acceptance**
Enable dependency-download network access; run `pnpm install`, `pnpm deps:api`, `pnpm generate:api`, install Chromium, run `pnpm format`, then `pnpm install --frozen-lockfile`, `pnpm check` and the full `pnpm test:e2e`. Review/fix actual failures, retain the resulting lockfile/Go checksums/generated contracts, and record PR/CI evidence before checking WBS gates. No containers, cluster, real funds, or M2 execution were implemented.

## 2026-10-31 — Set up a pnpm monorepo workspace and per-step E2E gates

**WBS:** 1.1 (prep) · **PR/commit:** uncommitted (staged)

**Changed**
- `package.json` — root workspace manifest (private, `pnpm@11.3.0`, Node ≥ 22) whose scripts orchestrate both stacks:
  - Go: `dev:api`, `build:api`, `lint:api`, `test:api`, all via `go -C backend`
  - JS workspaces: `dev:web`, `build:web`, `lint:web`, `typecheck`, `test:web`, `test:e2e`
  - `check` runs lint, typecheck, test and build
- `pnpm-workspace.yaml` — workspaces `frontend` (`@railvanta/frontend`), `e2e` (`@railvanta/e2e`) and `packages/*`. Go stays outside the npm workspace graph.
- `pnpm-lock.yaml` — generated by `pnpm install` (no dependencies yet).
- `.gitignore` — ignores dependencies, build and test output, `.env*` except `.env.example`, and SQLite databases with their WAL/SHM sidecars.
- `.nvmrc` — Node 22.
- `docs/wbs.md`:
  - Every work package now ends with an **E2E test** block, a black-box spec under `e2e/`, and requires the full `pnpm test:e2e` suite to pass as a cumulative regression gate.
  - Standalone Playwright tasks were folded into those blocks.
  - 1.1 now creates the `e2e/` harness and switches its commands to pnpm.
- `AGENTS.md`, `docs/architecture.md`, `docs/frontend/design.md`, `docs/README.md` — updated to the monorepo layout, with E2E specs now in `e2e/`.
- `CODECHANGELOG.md` — this file.

**Verification**
- `pnpm install` — succeeded; created the lockfile and `node_modules/`.
- `pnpm run lint:web && pnpm run typecheck && pnpm run test:web` — exit 0. "No projects matched", as expected, since no JS workspaces exist yet.
- `pnpm run test:api` — exit 1 (`go: chdir backend: no such file or directory`). Expected: the Go module arrives in WBS 1.1.
- Link check across all Markdown files — 0 broken links.

**Notes:** `pnpm check` and `pnpm test:e2e` won't pass until WBS 1.1 scaffolds `backend/`, `frontend/` and `e2e/`.

## 2026-10-31 — Add the WBS checklist

**WBS:** planning · **PR/commit:** uncommitted (staged)

**Changed**
- `docs/wbs.md` — new checklist covering work packages 1.1–5.4 and the verification matrix. Schema-review items are marked as pending approval.
- `docs/roadmap.md`, `docs/README.md` — now link to the WBS instead of repeating its tables.

**Verification**
- Link check — 0 broken links. Every box is unchecked, since no implementation exists.

## 2026-10-30 — Reorganize the design docs

**WBS:** planning · **PR/commit:** uncommitted (staged)

**Changed**
- `docs/` — grouped into `backend/`, `frontend/` and `operations/`.
- Removed `progress.md` (its content merged into the README and roadmap) and `railvanta.sqlite` (rebuildable from `schema.sql`).
- Removed duplicated sections and per-file status disclaimers, and trimmed `idea.md` to a brief.

**Verification**
- Rebuilt `docs/backend/schema.sql` into a throwaway database with Python's SQLite 3.51.0: 15 tables, `integrity_check` returned `ok`, and `foreign_key_check` returned no rows.
- Link check — 0 broken links.
