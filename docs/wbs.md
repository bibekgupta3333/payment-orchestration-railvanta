# Work Breakdown Structure — Checklist

This is the actionable checklist for every work package. Milestones, the dependency diagram and deferred scope are in the [roadmap](roadmap.md).

## How to use

- **Task boxes track implementation:** `[x]` means the requested code/configuration is added, with source evidence in the M1 table below. It does not mean runtime tests passed or the change merged. Partially implemented tasks stay `[ ]`.
- **Acceptance and E2E boxes track verified outcomes:** tick them only when the stated check actually passes, with recorded evidence. A work package remains open until its acceptance/E2E gates pass and merge evidence is recorded.
- A work package is done only when every item in its **Acceptance gate** is ticked.
- Do not start a package until everything it depends on has passed its gate. Frontend shell work may run in parallel against clearly labelled fixtures. The subsequent user request also authorizes basic Docker images/Compose alongside M1; full delivery/recovery gates remain in 5.2.
- The implementation request on 2026-10-31 authorizes schema-review items 1.2.1–1.2.8. Their source changes are added; verification and merge gates still apply. The reference lives at `docs/backend/schema.sql`.
- **Every work package ends with an E2E test.** Its **E2E test** block names a spec under `e2e/` and the scenarios it must cover. The package is not done until that spec passes *and* the full suite (`pnpm test:e2e`, covering every earlier package's specs) still passes, so each step is also a regression check.
- E2E tests are black-box. The harness builds the Go API, then starts it with `APP_ENV=sandbox`, a fresh temporary SQLite file and the deterministic mock provider. `ui/` specs also start the Next.js app. Specs talk only over HTTP or through the browser. `e2e/api/` uses Playwright's `request` fixture, and `e2e/ui/` uses the browser.
- Log every merged change in [CODECHANGELOG.md](../CODECHANGELOG.md), including the E2E command and its actual result.
- When a package passes its gate, record that in the status table below.

## M1 implementation checkpoint — 2026-10-31

Source for 1.1–1.4 is present, implemented with five specialist agents plus integration review. Config and exact-money tests pass. SQLite schema checks pass independently. Dependency downloads are denied by the workspace network policy; the existing pnpm lockfile is stale, Go checksums and generated frontend contracts are missing, and full Go/frontend/E2E checks cannot pass yet. See [CODECHANGELOG.md](../CODECHANGELOG.md) for actual commands and results and [README.md](../README.md) for the completion sequence.

Implemented task boxes are checked below at the user's request; acceptance and E2E boxes remain open. No PR, merge, green CI, browser accessibility pass, or M1 completion is claimed. The schema revisions are authorized by the user request; they do not need approval again to finish this work.

### Implementation checklist totals

These counts describe source added, not verified package completion.

| Package | Implemented tasks | Still open |
| --- | --- | --- |
| 1.1 Scaffold | 15 / 16 | 1.1.15: full root commands must pass |
| 1.2 Database foundation | 14 / 14 | Go/runtime and package acceptance/E2E gates |
| 1.3 API and auth contract | 12 / 13 | 1.3.13: generate frontend contracts and verify tooling |
| 1.4 UI shell | 10 / 10 | Formatting, build, component/browser and package acceptance/E2E gates |
| **M1 source tasks** | **51 / 53** | **All four package gates remain open** |

### Implementation evidence by work package

| Package / task IDs | Source added | Verification still required |
| --- | --- | --- |
| 1.1.1–1.1.5 | [Go entry point](../backend/cmd/api/main.go), [configuration](../backend/internal/config/config.go), package boundaries, migration/test READMEs, [Makefile](../backend/Makefile), and golangci-lint configuration | Full Go build, vet/lint, live startup and graceful shutdown. Config unit tests and Go formatting passed. |
| 1.1.6–1.1.10 | [Frontend workspace](../frontend/package.json), strict TypeScript, Tailwind, ESLint/Prettier, Vitest/RTL tests, [proxy](../frontend/next.config.ts), and placeholder environment files | Install dependencies, format frontend source, then run lint/typecheck/unit tests/build and proxy E2E checks. |
| 1.1.11–1.1.14 | [CI workflow](../.github/workflows/ci.yml), [setup guide](../README.md), contributor commands, [Playwright configuration](../e2e/playwright.config.ts), [process harness](../e2e/support/global-setup.ts), and [restart helpers](../e2e/support/state.ts) | Execute the harness and full suite, then obtain actual PR/CI evidence. |
| 1.1.15 | [Root commands](../package.json) and [changelog](../CODECHANGELOG.md) updated | Regenerate the dependency lockfile/checksums and make `pnpm check` plus `pnpm test:e2e` pass. |
| 1.1.16 | [API image](../infrastructure/docker/api.Dockerfile), [web image](../infrastructure/docker/web.Dockerfile), [Compose](../docker-compose.yml), proxy, and root commands | `pnpm compose:config` passes; image builds/startup blocked by sandbox Docker access and unresolved M1 dependency outputs. |
| 1.2.1–1.2.9 | All requested schema revisions authorized and added to [numbered migrations](../backend/migrations/README.md), [reference SQL](backend/schema.sql), and [data model](backend/data-model.md): 23 STRICT tables | Review and execute the Go-backed constraint tests. Independent Python SQL constraint and upgrade checks passed. |
| 1.2.10–1.2.14 | [Migration runner](../backend/internal/storage/migrate.go), [connection/transaction helpers](../backend/internal/storage/storage.go), [explicit models](../backend/internal/storage/models.go), and [storage tests](../backend/internal/storage/storage_test.go) | Compile/run Go tests and `e2e/api/database.spec.ts`; Python SQL checks do not verify the Go runner or connection setup. |
| 1.3.1–1.3.12 | [OpenAPI](../backend/api/openapi.yaml), [exact-money helpers](../backend/internal/payment/money.go), [auth](../backend/internal/auth/auth.go), [HTTP boundary](../backend/internal/transport/http/handler.go), loopback demo bootstrap, and private health listener | Full auth/HTTP tests and `e2e/api/auth-contract.spec.ts`. Money/config unit tests passed; OpenAPI parsing/reference checks passed. |
| 1.3.13 | [Contract generator](../scripts/generate-api.mjs), OpenAPI lint command, and CI wiring added | **Generated types are missing:** install dependencies, run `pnpm generate:api`, then Redocly lint, generated-contract consistency, and TypeScript checks. |
| 1.4.1–1.4.10 | [Responsive shell](../frontend/components/layout/app-shell.tsx), themes, route placeholders, UI primitives, exact money formatting, validated API client, and component tests | Vitest, frontend build, client-bundle secret checks, and `e2e/ui/shell.spec.ts` at 375px/1440px, including keyboard/focus and axe checks. |

### Observed verification

| Check | Last observed result |
| --- | --- |
| `pnpm test:api:foundation` with a writable temporary `GOCACHE` | Passed: sandbox config and exact-money parsing/serialization |
| `pnpm format:api` | Passed |
| Independent file-backed Python/SQLite checks | Passed: 22 constraint rejection cases, valid fixtures, upgrade preservation, integrity/foreign keys, 23 STRICT tables |
| Go/TypeScript source syntax and OpenAPI local references | Passed syntax/structural checks; full compilation and Redocly lint remain unverified |
| Required-workspace and tracked-file policy checks | Passed |
| `pnpm install --frozen-lockfile` | Failed: lockfile does not contain the new dependencies |
| Dependency installation / `pnpm generate:api` | Blocked by registry access; Go checksums and generated frontend contracts remain missing |
| `pnpm check` | Failed at Go dependency resolution; subsequent gates did not run |
| `pnpm test:e2e` | Failed before suite execution because workspace dependencies are not installed |
| PR / CI / merge | No evidence yet |

These are the recorded implementation-session results, not fresh test runs for this documentation update. A successful syntax or SQL check does not close a package's runtime acceptance gate.

### Remaining M1 completion sequence

- [ ] Enable network access for dependency downloads and run `pnpm install` to update the lockfile.
- [ ] Run `pnpm deps:api` to resolve Go dependencies and produce `backend/go.sum`.
- [ ] Run `pnpm generate:api` to produce the missing frontend contracts.
- [ ] Install Chromium: `pnpm --filter @railvanta/e2e exec playwright install chromium`.
- [ ] Run `pnpm format`, then `pnpm install --frozen-lockfile` and `pnpm check`; fix and record actual failures.
- [ ] Run the full `pnpm test:e2e`; verify scaffold, database, auth-contract, and responsive shell specs together.
- [ ] Review the authorized schema changes and implementation; record PR/green CI/merge evidence before closing package acceptance and E2E gates.

## Status

| ID | Work package | Depends on | Milestone | Status |
| --- | --- | --- | --- | --- |
| 1.1 | Scaffold | Design baseline | M1 | Source added; dependency lockfiles, full checks and CI pending |
| 1.2 | Database foundation | 1.1 | M1 | Schema checks passed; Go runner and database E2E pending |
| 1.3 | API and auth contract | 1.1 | M1 | Auth source added; generated types and runtime tests pending |
| 1.4 | UI shell | 1.1 | M1 | Source/tests added; formatting, build and browser checks pending |
| 2.1 | Payment acceptance | 1.2, 1.3 | M2 | Done (verified 2026-10-31) |
| 2.2 | Routing and mock execution | 2.1 | M2 | Not started |
| 2.3 | Atomic ledger posting | 2.2 | M2 | Not started |
| 2.4 | Recovery and cancellation | 2.2, 2.3 | M2 | Not started |
| 3.1 | Payment-facing screens | 1.4, 2.1 | M3 | Not started |
| 3.2 | Routing and simulation UI | 2.2, 3.1 | M3 | Not started |
| 4.1 | Ledger explorer | 2.3, 3.1 | M4 | Not started |
| 4.2 | Reconciliation | 2.4, 4.1 | M4 | Not started |
| 4.3 | Events and audit | 2.4, 1.3 | M4 | Not started |
| 4.4 | Docs and settings UI | 3.1, 1.3 | M4 | Not started |
| 5.1 | End-to-end correctness | 3.2, 4.1–4.4 | M5 | Not started |
| 5.2 | Container delivery and recovery | 5.1 | M5 | Image/Compose source added; runtime and recovery gates pending |
| 5.3 | Kubernetes reference deployment (optional) | 5.2 | M6 | Not started |
| 5.4 | Release documentation | 5.2; 5.3 if used | M5 | Not started |

## 1. Foundation and contracts (M1)

### 1.1 Scaffold — backend + frontend
**Depends on:** Design baseline · **Deliverable:** Go module and Next.js TypeScript app with formatting, linting, tests and CI wired, and documented commands

**Tasks**
- [x] 1.1.1 Create the Go module in `backend/` with `cmd/api/main.go` and empty packages under `internal/` (`auth`, `payment`, `routing`, `provider`, `ledger`, `reconciliation`, `event`, `storage`, `transport/http`)
- [x] 1.1.2 Add `backend/migrations/` and `backend/tests/` directories with a README stating their purpose
- [x] 1.1.3 Configure `gofmt`, `go vet` and `golangci-lint` (`backend/.golangci.yml`) with a Makefile or script target for each
- [x] 1.1.4 Make `cmd/api` start an HTTP server on `HTTP_ADDR` (default `:8080`) and shut down gracefully on SIGTERM
- [x] 1.1.5 Load config from the environment (`APP_ENV`, `HTTP_ADDR`, `SQLITE_PATH`) and refuse to start unless `APP_ENV=sandbox`
- [x] 1.1.6 Scaffold `frontend/` as the `@railvanta/frontend` pnpm workspace (already listed in `pnpm-workspace.yaml`), with Next.js App Router, TypeScript (strict), Tailwind CSS and two-space indentation
- [x] 1.1.7 Configure ESLint and Prettier in `frontend/`, with `lint`, `format` and `typecheck` scripts that the root `pnpm lint`, `pnpm format` and `pnpm typecheck` pick up
- [x] 1.1.8 Add Vitest + React Testing Library to `frontend/` (a `test` script, run by the root `pnpm test:web`) with one placeholder test
- [x] 1.1.9 Add a dev proxy that forwards `/v1/*` to the Go API without stripping the path (Next.js rewrites using `API_INTERNAL_ORIGIN`)
- [x] 1.1.10 Add `.env.example` files for backend and frontend containing placeholders only, and add `.gitignore` entries for `.env*`, `*.db`, `*.db-wal` and `*.db-shm`
- [x] 1.1.11 Add a CI workflow that runs Go format check, vet, lint, `go test ./...`, frontend lint, typecheck, unit tests and build
- [x] 1.1.12 Write the root `README.md` setup section and update the AGENTS.md commands section with the real commands
- [x] 1.1.13 Create the `@railvanta/e2e` workspace (`e2e/package.json`, `e2e/playwright.config.ts`, `e2e/api/`, `e2e/ui/`). Its global setup builds and starts the Go API on a fresh temporary SQLite file with free ports, starts Next.js for `ui/` projects, and tears both down.
- [x] 1.1.14 Add E2E harness helpers in `e2e/support/`: `restartApi()` (graceful), `killApi()` (SIGKILL, then restart on the same database file), a typed API client, and fixtures for the seeded sandbox workspace and its mock payment methods
- [ ] 1.1.15 Add `CODECHANGELOG.md` entries for the scaffold, and keep the root `package.json` scripts working end to end (`pnpm check`, `pnpm test:e2e`) — **Partial:** source and log added; `pnpm check` and `pnpm test:e2e` have not passed.
- [x] 1.1.16 Add separate backend/frontend Docker images and `docker-compose.yml` with a same-origin proxy, health-ordered startup, and API-only persistent SQLite volume; expose root `pnpm compose:*` commands. Configuration validates; image builds and runtime verification remain open.

**Acceptance gate**
- [ ] `cd backend && go build ./... && go test ./...` passes on a clean checkout
- [ ] `pnpm install --frozen-lockfile && pnpm check` passes from the repository root (Go and frontend lint, typecheck, test and build)
- [ ] CI is green on the scaffolding PR
- [ ] The API exits with a non-zero status when `APP_ENV` is anything other than `sandbox`
- [ ] A request to `/v1/...` through the Next.js dev server reaches the Go process with the path preserved
- [ ] No secrets or database files are tracked by Git (`git ls-files` check)

**Container acceptance gates**
- [ ] `pnpm compose:build` builds both application images using the committed lockfile/checksums.
- [ ] `pnpm compose:up` starts healthy non-root API/frontend containers behind one loopback browser origin.
- [ ] Browser and authenticated `/v1` requests work through the proxy without path stripping; private API health is not published.
- [ ] SQLite data survives ordinary Compose stop/start, and the API is the only volume owner.

**E2E test** — `e2e/api/scaffold.spec.ts`
- [ ] The harness boots the API, and `GET /health/ready` returns 200
- [ ] A request to `/v1/<probe>` through the Next.js server reaches the Go API with the path preserved (asserted with a request-ID header or the Go error envelope)
- [ ] Starting the API with `APP_ENV=production` exits non-zero, and the harness reports that clearly
- [ ] The `/` page renders through the browser project (a smoke check that the UI harness works)
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 1.2 Database foundation — backend
**Depends on:** 1.1 · **Deliverable:** versioned SQL migrations derived from the revised `docs/backend/schema.sql`, plus GORM mappings and an explicitly configured SQLite connection

**Tasks**
- [x] 1.2.1 Payment methods: make `payment_methods` provider-neutral (rail, brand, last 4) and add `payment_method_tokens(method_id, provider_id, token_reference)`; routing eligibility is a token joined with a rule (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.2 Mock provider and simulator tables: add `mock_provider_charges`, keyed by provider idempotency key and owned only by the mock adapter, and add a `simulation_runs` table (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.3 Immutability: add triggers that make `amount_minor`, `currency`, `workspace_id` and `payment_method_id` immutable, block status changes after a terminal state, and make `payment_state_transitions` append-only (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.4 Allow NULL `from_status` only when `to_status = 'created'` (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.5 Convert all tables to `STRICT` and drop the now-redundant `typeof` checks (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.6 Add `users`, `workspace_members` (with role), `sessions` (hashed token) and `api_keys` (hashed, with a visible prefix) (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.7 Should-fix items (schema revision authorized 2026-10-31; verification pending):
  - attempt `submitted_at`/`completed_at`
  - routing `min_amount_minor`/`max_amount_minor`
  - outbox lease CHECK and `last_error`
  - inbox `duplicate_count`/`last_received_at`
  - per-provider clearing accounts
  - CHECK on discrepancy `kind`
  - `payments.merchant_reference` with an index
- [x] 1.2.8 Housekeeping: strip PRAGMAs and `BEGIN`/`COMMIT` from migration files, track versions in `schema_migrations` only, and generate IDs as UUIDv7 (schema revision authorized 2026-10-31; verification pending)
- [x] 1.2.9 Split the revised schema into numbered files `backend/migrations/0001_*.sql` …, and update `docs/backend/schema.sql` and `data-model.md` to match
- [x] 1.2.10 Write a migration runner in `internal/storage`:
  - ordered application
  - checksum recorded in `schema_migrations`
  - abort on checksum drift
  - fail fast on a non-empty, incompatible database
- [x] 1.2.11 Open connections with WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=FULL` and `SetMaxOpenConns(1)`, then verify each setting after opening
- [x] 1.2.12 Add GORM models with explicit `TableName()`, int64 money and int64 millisecond timestamps, and no `gorm.Model`. Add a `WithTx` unit-of-work helper that passes `tx` to every repository.
- [x] 1.2.13 Write file-backed SQLite tests in a temporary directory, one per new constraint or trigger (each rejection and each valid path), plus `integrity_check`/`foreign_key_check` on a fresh database
- [x] 1.2.14 Add an upgrade fixture: apply migration N-1, insert sample rows, apply N, and run the integrity checks

**Acceptance gate**
- [ ] Each schema-review change is approved, or explicitly deferred, in the PR description
- [ ] A fresh database built by the runner passes `PRAGMA integrity_check` (`ok`) and `foreign_key_check` (no rows)
- [ ] Constraint tests pass against a file-backed database. They must show that each of these is rejected:
  - a cross-workspace foreign key
  - a non-integer or out-of-range amount
  - an edit to an immutable payment field
  - deleting an audit row
  - an unbalanced or mixed-currency journal
  - a duplicate posting key
- [ ] Startup refuses to proceed when a connection PRAGMA does not take effect
- [ ] Re-running the migrations is a no-op, and a modified migration file fails the checksum check
- [ ] The upgrade fixture passes

**E2E test** — `e2e/api/database.spec.ts`
- [ ] On an empty temporary directory, the API creates the database, applies every migration and becomes ready
- [ ] `restartApi()` on the same file is a no-op migration and the API becomes ready again
- [ ] Seeded rows survive `killApi()` followed by a restart (WAL durability)
- [ ] A tampered migration checksum leaves the API unready (non-zero exit or failed `/health/ready`)
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 1.3 API and auth contract — backend
**Depends on:** 1.1 · **Deliverable:** OpenAPI contract plus HTTP middleware for request IDs, an error envelope, decimal-string money and workspace-scoped authentication

**Tasks**
- [x] 1.3.1 Write `backend/api/openapi.yaml` covering all brief endpoints:
  - request/response schemas
  - `Idempotency-Key` header
  - cursor pagination (`items`, `next_cursor`)
  - error codes
- [x] 1.3.2 Define money in the contract as `amount_minor` decimal strings plus an ISO-4217 `currency`, with a per-currency exponent table in the domain layer
- [x] 1.3.3 Implement strict money parsing in `transport/http`:
  - Reject fractions, signs, leading `+`, empty strings and values above int64.
  - Reject amounts not allowed by the currency's exponent.
  - Serialize int64 back to a string.
- [x] 1.3.4 Implement the error envelope `{code, message, request_id, field_errors[]}` with no stack traces or internals
- [x] 1.3.5 Add request-ID and correlation-ID middleware with sanitized structured logging that excludes secrets, tokens and full idempotency keys
- [x] 1.3.6 Implement session authentication: secure HttpOnly cookie, server-side hashed session lookup, and expiry
- [x] 1.3.7 Implement API-key authentication: `Authorization: Bearer`, hashed-key lookup by prefix, constant-time comparison
- [x] 1.3.8 Derive workspace authority only from the session or API key, and ignore or reject any workspace ID supplied by the client
- [x] 1.3.9 Add role permissions (read vs operator), enforced per route group
- [x] 1.3.10 Add CSRF protection for cookie-authenticated writes, plus a request body size limit and a basic rate limit
- [x] 1.3.11 Add an explicit loopback-only demo mode with a fixed seeded workspace, gated by config and refused on non-loopback binds
- [x] 1.3.12 Add `/health/startup`, `/health/live` and `/health/ready` on a private listener (not routed under `/v1`)
- [ ] 1.3.13 Add a CI lint step for the OpenAPI document, and generate typed frontend contracts into `frontend/lib/api/` — **Partial:** generator/lint/CI wiring added; generated contract file is still missing.

**Acceptance gate**
- [ ] The OpenAPI document validates in CI, and the generated frontend types compile
- [ ] Money tests pass for these cases:
  - `"9223372036854775807"` round-trips
  - `"9223372036854775808"`, `"1.5"`, `"-1"`, `"0"` and `" 1"` are rejected
- [ ] Requests without credentials get `401`; credentials for workspace A reading a resource in workspace B get `404`/`403`
- [ ] A read-role caller is denied operator routes
- [ ] A cookie-authenticated write without a CSRF token is rejected
- [ ] Every response carries a request ID, and error bodies match the envelope schema
- [ ] Demo mode refuses to start when bound to a non-loopback address
- [ ] Logs captured during the tests contain no secrets, raw tokens or full idempotency keys

**E2E test** — `e2e/api/auth-contract.spec.ts`
- [ ] An unauthenticated `/v1` request returns 401 with the error envelope and a `request_id`
- [ ] With a session for workspace A, an ID belonging to workspace B returns not-found, never B's data
- [ ] A non-string, fractional or out-of-int64 `amount_minor` returns 400 with a field error
- [ ] Every response in the spec validates against the OpenAPI document
- [ ] An API key authenticates server-to-server; a revoked key is rejected
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 1.4 UI shell — frontend
**Depends on:** 1.1 · **Deliverable:** responsive, accessible dashboard shell with a sandbox badge, theming and reusable controls; pages show labeled fixtures or unavailable states

**Tasks**
- [x] 1.4.1 Set up the root `app/layout.tsx`: Inter or Geist font, global providers (TanStack Query with mutation retries disabled), skip link and landmarks
- [x] 1.4.2 Define design tokens in Tailwind: navy `#14253E`, teal `#12A89D`, background `#F8FAFC`, semantic status, focus and contrast-checked text colors, plus a dark theme
- [x] 1.4.3 Build the `app/dashboard/layout.tsx` desktop shell: persistent sidebar with all routes, workspace context, an always-visible Sandbox badge, theme switch and account menu
- [x] 1.4.4 Build the mobile shell: a modal navigation drawer that traps focus, closes on Escape and restores focus, with no horizontal scroll at 375px
- [x] 1.4.5 Create placeholder pages for every route in the design, each showing a clear "unavailable until backend exists" state
- [x] 1.4.6 Build `components/ui` primitives: Button, Badge, StatusPill (text plus icon), Dialog, Table with a labeled scroll region, Skeleton, EmptyState, ErrorState (with request ID), UnavailableState
- [x] 1.4.7 Write `lib/money` helpers: format decimal strings using integer/BigInt-only arithmetic driven by currency exponents, with no `Number` conversion
- [x] 1.4.8 Build a `lib/api` client: same-origin relative `/v1`, error-envelope parsing, response validation with Zod, and no secrets in `NEXT_PUBLIC_*`
- [x] 1.4.9 Map payment states to labels, showing `unknown` as "Outcome unknown" with no retry action
- [x] 1.4.10 Add component tests for money formatting (including values above `Number.MAX_SAFE_INTEGER`), status labels and drawer focus behavior

**Acceptance gate**
- [ ] Every navigation destination can be reached by keyboard, with visible focus, at both 375px and 1440px
- [ ] The Sandbox badge is visible on every dashboard route in both themes
- [ ] Money formatter tests pass for exponent-0, exponent-2 and exponent-3 currencies, and for `"9007199254740993"`, without precision loss
- [ ] The Playwright smoke and axe checks pass in CI with no critical violations
- [ ] No page shows success, health or metric values that aren't labelled as fixtures or unavailable
- [ ] A bundle check finds no server-only environment values in the client output

**E2E test** — `e2e/ui/shell.spec.ts`
- [ ] At 375px and 1440px, every dashboard route renders the shell with the Sandbox badge visible
- [ ] Navigation works by keyboard alone; the mobile drawer traps focus, closes on Escape and restores focus
- [ ] An axe scan reports no critical violations on any route
- [ ] Routes for capabilities that aren't built yet show an unavailable state, never fake data or working-looking actions
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

## 2. Payment correctness (M2)

M2 is the first correctness milestone: a payment survives response loss and process restart, produces one balanced posting, and exposes ambiguous outcomes safely. Complete it before any external provider integration.

### 2.1 Payment acceptance — backend
**Depends on:** 1.2, 1.3 · **Deliverable:** Idempotent `POST /v1/payments` plus workspace-scoped list/detail reads, with the payment, audit transition, outbox command and stored response committed atomically.

**Tasks**
- [x] 2.1.1 Define the `Payment` aggregate and status enum in `backend/internal/payment/` (`created`, `processing`, `succeeded`, `failed`, `canceled`, `unknown`) with a pure transition-validation function and table-driven unit tests for every legal and illegal transition.
- [x] 2.1.2 Implement amount parsing in `backend/internal/payment/money.go`: decimal-string `amount_minor` → positive int64, rejecting fractions, zero, negatives, leading `+`, whitespace, and values beyond int64; validate currency code and exponent against a domain currency list.
- [x] 2.1.3 Implement request canonicalization and SHA-256 hashing in `backend/internal/payment/idempotency.go` over operation, amount, currency, payment method and merchant reference; document field order and normalization.
- [x] 2.1.4 Add GORM repositories in `backend/internal/storage/` for payments, state transitions, idempotency records and outbox events; every query carries a `workspace_id` predicate and accepts the caller's `*gorm.DB` transaction handle.
- [x] 2.1.5 Implement the acceptance unit of work in `backend/internal/payment/service.go`: one short `db.Transaction` that reserves `(workspace_id, operation, key)`, inserts the payment, writes the initial transition (`from_status` NULL → `created`, per 1.2 schema revision), appends a `payment.dispatch` outbox command, and stores the `202` response body.
- [x] 2.1.6 Handle key reuse: same key and hash replays the stored status and body; changed hash returns `409 idempotency_conflict`; a losing concurrent insert reads the committed winner instead of creating a second payment.
- [x] 2.1.7 Validate that the payment method belongs to the authenticated workspace and has at least one provider token (`payment_method_tokens`, per 1.2 schema revision) before accepting.
- [x] 2.1.8 Add HTTP handlers in `backend/internal/transport/http/payments.go`: `POST /v1/payments` (requires `Idempotency-Key`, 1–255 chars), `GET /v1/payments` (cursor pagination, status filter), `GET /v1/payments/{id}`, and `GET /v1/payments/{id}/attempts`; serialize money as decimal strings.
- [x] 2.1.9 Return the shared error envelope with request ID for validation, auth, not-found and conflict cases; ensure validation and auth failures never create idempotency records.
- [x] 2.1.10 Add bounded `SQLITE_BUSY` retry with jitter for the acceptance transaction only (it has no external side effects); emit busy-error and acceptance-latency metrics.
- [x] 2.1.11 Add the operations to the OpenAPI document and contract tests confirming responses match the schema.
- [x] 2.1.12 Write integration tests in `backend/tests/` against a temporary file-backed SQLite database with WAL enabled.

**Acceptance gate**
- [x] 50 concurrent `POST /v1/payments` requests with the same key and body yield exactly one payment row, one idempotency record, one outbox command, and identical `202` bodies.
- [x] Same key with a changed amount, currency or method returns `409`, and the original payment is unchanged.
- [x] Restarting the process between requests still replays the stored response for a reused key.
- [x] An amount of `"9223372036854775807"` round-trips through the API exactly; `"9223372036854775808"`, `"0"`, `"-1"` and `"1.5"` return `400`.
- [x] A request using another workspace's payment ID or method ID returns `404` and reads or writes nothing.
- [x] Injecting a failure after the payment insert rolls back the whole transaction (no payment, record, transition or outbox row remains).
- [x] Every created payment has exactly one initial audit transition.

**E2E test** — `e2e/api/payment-acceptance.spec.ts`
- [x] `POST /v1/payments` returns 202 with a payment ID and a `Location` header, and `GET` on that location returns the payment
- [x] Replaying the same key with the same body returns a byte-identical stored response; a changed body returns 409
- [x] 20 concurrent requests with the same key produce exactly one payment (checked with the list endpoint)
- [x] A request without an `Idempotency-Key` is rejected, and nothing is created
- [x] After `restartApi()`, replaying the original key still returns the stored response
- [x] Amounts above `Number.MAX_SAFE_INTEGER` round-trip exactly as decimal strings
- [x] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 2.2 Routing and mock execution — backend
**Depends on:** 2.1; 1.2 schema revisions (`payment_method_tokens`, attempt `submitted_at`/`completed_at`, `mock_provider_charges`) · **Deliverable:** Persisted routing decisions plus an in-process dispatcher that executes attempts through a deterministic mock provider, with provider I/O outside database transactions.

**Tasks**
- [x] 2.2.1 Define the provider interface in `backend/internal/provider/` (`Submit`, `Lookup` by provider idempotency key, `Capabilities`) with typed outcomes `Succeeded`, `Declined` (proven no effect), and `Ambiguous`; keep adapters free of payment-domain imports.
- [x] 2.2.2 Implement the mock adapter in `backend/internal/provider/mock/` with durable `mock_provider_charges` keyed by provider idempotency key, read and written only by the adapter, so its record of what executed survives restarts.
- [x] 2.2.3 Add deterministic scenarios to the mock: success, decline, delayed success, timeout before execution, timeout after success, and outage; select the scenario from persisted simulation input, never from wall-clock randomness.
- [x] 2.2.4 Make the mock honor its idempotency contract: resubmitting the same key returns the original charge instead of executing again.
- [x] 2.2.5 Implement routing in `backend/internal/routing/`: eligible providers = providers holding a token for the method ∩ enabled rules matching rail, currency and amount bounds, ordered by priority; return the chosen provider, rule ID and a human-readable reason.
- [ ] 2.2.6 Implement `POST /v1/routing/evaluate` as a read-only evaluation that writes nothing and has no money-moving effect.
- [ ] 2.2.7 Implement the outbox dispatcher in `backend/internal/event/dispatcher.go`: claim due rows with a compare-and-swap update, a fresh unpredictable lease token in `lease_owner`, and a bounded `lease_until`; renew and complete only when the token matches.
- [ ] 2.2.8 In one short transaction, move the payment `created → processing` (version-guarded), insert the attempt with routing decision and a stable provider idempotency key, and mark it `submitted` with `submitted_at` before any network call.
- [ ] 2.2.9 Call the provider outside every transaction with a request deadline; then persist the outcome in a new transaction using a status-predicate guarded update and set `completed_at`.
- [ ] 2.2.10 Map outcomes: `Succeeded` hands off to ledger posting (2.3); `Declined` sets attempt and payment `failed`; `Ambiguous` or a timeout sets both to `unknown` and schedules a same-provider lookup outbox command.
- [ ] 2.2.11 Ensure no code path submits an `unknown` or stale `submitted` payment to a different provider; add a guard and a test asserting it.
- [ ] 2.2.12 Start and stop the dispatcher with the API lifecycle in `backend/cmd/api/`: stop claiming on shutdown, drain bounded in-flight work, close the database.
- [ ] 2.2.13 Emit metrics for dispatcher lag, attempt latency (`completed_at − submitted_at`), outcomes by provider, and unknown-outcome count.

**Acceptance gate**
- [ ] Each mock scenario produces the documented payment and attempt states in a file-backed SQLite integration test.
- [ ] A test proves no database transaction is open while the provider call runs (for example, a concurrent write succeeds during a blocked mock submit).
- [ ] Two dispatchers racing on the same outbox row result in exactly one claim and one attempt.
- [ ] A dispatcher whose lease expired cannot complete or renew the row after another owner reclaims it.
- [ ] A timeout-after-success leaves the payment `unknown`, the attempt keeping its provider idempotency key, and zero attempts at any other provider.
- [ ] Resubmitting under the same provider key creates one `mock_provider_charges` row.
- [ ] `POST /v1/routing/evaluate` leaves the database byte-for-byte unchanged and reports the same provider and reason as a real dispatch for the same input.

**E2E test** — `e2e/api/dispatch.spec.ts`
- [ ] A payment with the seeded success mock method reaches `succeeded`; its attempt shows the provider, the routing reason and `submitted_at`/`completed_at`
- [ ] The decline mock method ends in `failed`, with no further attempts
- [ ] `POST /v1/routing/evaluate` returns the provider and reason without creating a payment or attempt
- [ ] Using the delayed-success method, `killApi()` while the attempt is `submitted` and then restarting does not submit a second charge (the attempt's provider key is unchanged and the mock records one charge)
- [ ] An `unknown` attempt is never retried with a different provider
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 2.3 Atomic ledger posting — backend
**Depends on:** 2.2; 1.2 schema revision (per-provider clearing accounts) · **Deliverable:** A confirmed success, its balanced journal, the audit transition and outbox completion commit together, and repeated results never post twice.

**Tasks**
- [x] 2.3.1 Implement the posting service in `backend/internal/ledger/` that builds a draft journal, inserts entries, checks totals with overflow-safe int64 arithmetic, and marks the journal posted, all inside the caller's transaction handle.
- [x] 2.3.2 Define the success posting rule: debit `provider_clearing` for the executing provider, credit `merchant_payable`, same currency and amount as the payment.
- [x] 2.3.3 Seed or lazily create per-workspace, per-currency, per-provider ledger accounts with unique codes; reject posting to an account in a different currency or workspace.
- [x] 2.3.4 Use a deterministic posting key (`payment:{id}:success`) so the unique `(workspace_id, posting_key)` constraint rejects duplicates; treat a duplicate-key error as "already posted" and confirm the existing journal matches.
- [ ] 2.3.5 Combine the success transition (`processing|unknown → succeeded`, version-guarded), attempt success, journal posting, audit transition and outbox completion into one transaction in `backend/internal/payment/`.
- [x] 2.3.6 Implement reversal journals: new journal with opposite sides and `reversal_of`, plus an application check that it exactly offsets the original; no edits to posted rows.
- [x] 2.3.7 Add a ledger invariant checker (`backend/internal/ledger/verify.go`) that confirms every posted journal balances, every succeeded payment has exactly one success journal, and no failed or canceled payment has one.
- [ ] 2.3.8 Expose read-only `GET /v1/ledger/transactions` with cursor pagination and decimal-string amounts.
- [ ] 2.3.9 Write integration tests covering duplicate success delivery, rollback mid-posting, mixed currency, unbalanced entries, int64 overflow, and attempts to update or delete posted rows.

**Acceptance gate**
- [ ] Delivering the same success result three times (including concurrently) produces one journal with two balanced entries.
- [ ] Forcing an error after entries are inserted but before commit leaves no journal, no entries, and the payment unchanged.
- [x] Unbalanced, mixed-currency, single-entry and overflow-sum postings are rejected by both the service and the database triggers.
- [x] Updating or deleting a posted journal or entry fails at the database layer.
- [ ] The invariant checker passes after every scenario in the 2.2 and 2.4 test suites.
- [x] A reversal produces a linked journal; a second reversal of the same journal is rejected.

**E2E test** — `e2e/api/ledger-posting.spec.ts`
- [ ] A succeeded payment has exactly one posted journal in `GET /v1/ledger/transactions`, with debits equal to credits in the payment's currency
- [ ] The debit goes to that provider's clearing account and the credit to merchant payable
- [ ] A re-delivered success result (a repeated dispatcher completion) leaves exactly one journal
- [ ] Failed and canceled payments have no journal
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 2.4 Recovery and cancellation — backend
**Depends on:** 2.2, 2.3 · **Deliverable:** Unknown outcomes and crash leftovers resolve only against the original provider, and cancellation safely races dispatch.

**Tasks**
- [ ] 2.4.1 Implement the lookup worker in `backend/internal/payment/recovery.go` that processes scheduled lookup commands by calling `Lookup` on the original provider with the stored provider idempotency key, outside any transaction.
- [ ] 2.4.2 Resolve `unknown` → `succeeded` (with atomic posting via 2.3) on confirmed success, `unknown` → `failed` only on proof of no effect, and leave it `unknown` with backoff when the provider cannot answer.
- [ ] 2.4.3 On startup, scan for attempts stuck in `submitted` with expired leases, treat them as potentially executed, and move them into the lookup path rather than resubmitting.
- [ ] 2.4.4 Allow same-provider replay only when the adapter declares an idempotent-replay contract, reusing the same provider key; otherwise lookup only.
- [ ] 2.4.5 Implement cancellation (`created → canceled`) as a version- and status-guarded update that loses safely to a dispatch claim; return `409` once processing has started.
- [ ] 2.4.6 Make dispatch claims skip canceled payments and abort cleanly if the payment changed state between claim and attempt creation.
- [ ] 2.4.7 Ensure terminal states never regress: guarded updates reject late or reordered results against `succeeded`, `failed` or `canceled`.
- [ ] 2.4.8 Add a crash-injection test harness in `backend/tests/` that kills the process (or aborts the goroutine and reopens the database) at each failure point in the recovery checklist.
- [ ] 2.4.9 Record recovery metrics: unknown-outcome age, lookup attempts, resolutions by result, and the oldest unresolved payment.
- [ ] 2.4.10 Document the recovery behavior and its limits in `docs/backend/flows.md` once implemented.

**Acceptance gate**
- [ ] Crash before submit: after restart the original attempt is resolved or resubmitted under the same key, and the mock records at most one charge.
- [ ] Crash after mock success, before the result commits: after restart the payment becomes `succeeded` with one journal and one mock charge.
- [ ] Timeout after success with the provider temporarily unavailable for lookup stays `unknown` (never `failed`) until the lookup succeeds.
- [ ] No test path creates an attempt at a different provider for a payment that was ever `unknown` or had a stale `submitted` attempt.
- [ ] 100 concurrent cancel-vs-dispatch races each end with exactly one winner and a consistent payment, attempt and audit history.
- [ ] A late success result arriving after the payment is `failed` is rejected and recorded for reconciliation, not applied.
- [ ] The end-to-end demo (accept → lose the response → `unknown` → resolve → one balanced journal) passes as an automated test.

**E2E test** — `e2e/api/recovery.spec.ts`
- [ ] **Central demo:** the timeout-after-success method shows `unknown`, then resolves to `succeeded` through a lookup against the original provider, with one mock charge and one balanced journal
- [ ] `killApi()` after provider acceptance and before the result is recorded: after a restart, the payment resolves through a lookup with no second charge
- [ ] Canceling while `created` gives `canceled`; canceling once processing has started is rejected; racing cancel against dispatch leaves exactly one winner
- [ ] An `unknown` payment exposes no retry action through the API, and no new attempt appears while it is unresolved
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

## 3. Payment operations UI (M3)

### 3.1 Payment-facing screens — frontend
**Depends on:** 1.4, 2.1 · **Deliverable:** Landing page, overview, payment explorer and payment detail wired to the real sandbox API.

**Tasks**
- [ ] 3.1.1 Build the public landing page in `frontend/app/page.tsx` (hero, architecture strip, capability cards, copyable API example, "Sandbox only / no real funds" notice); mark planned capabilities as planned.
- [ ] 3.1.2 Add typed payment queries in `frontend/lib/hooks/payments.ts` (`usePayments`, `usePayment`, `usePaymentAttempts`) with cache keys that include the workspace and environment; set a bounded retry for safe reads and no retry for 401/403/404/422.
- [ ] 3.1.3 Implement `frontend/lib/money/` helpers that format decimal-string minor units using exact BigInt arithmetic and a per-currency exponent table, plus unit tests in `frontend/tests/components/money.test.ts`.
- [ ] 3.1.4 Build the overview at `frontend/app/dashboard/page.tsx` from `GET /v1/dashboard/summary` and `/v1/dashboard/payment-volume`: metric cards for volume per currency (no cross-currency totals), a success rate, and a p95 card that shows "No observations" when data is missing.
- [ ] 3.1.5 Add a payment-volume chart in `frontend/components/charts/PaymentVolumeChart.tsx` (Recharts) with a text summary and a data table alternative; show the data source, time range and last-updated time.
- [ ] 3.1.6 Add a "Needs attention" banner on the overview that lists `unknown` payments with links to their detail pages; it offers no retry action.
- [ ] 3.1.7 Build the payment list at `frontend/app/dashboard/payments/page.tsx`: search by payment ID or `merchant_reference`, filter by status/rail/provider/date/amount, sort, and paginate with a cursor. All of these live in URL query parameters.
- [ ] 3.1.8 Build a responsive payment table in `frontend/components/payments/PaymentTable.tsx`: a labelled scroll region or summary cards at 375px, tabular numerals, and status labels that pair text with an icon.
- [ ] 3.1.9 Build the create-payment dialog in `frontend/components/payments/CreatePaymentDialog.tsx` (React Hook Form + Zod): amount, currency, rail, existing payment-method reference, optional `merchant_reference`, and inline field errors; it never collects card or bank data.
- [ ] 3.1.10 Implement retry-safe submission in `frontend/lib/hooks/useCreatePayment.ts`: one idempotency key per payment intent, kept across transport retries and replaced when the intent is edited, with mutation `retry: false`; the button is disabled while pending, and no success is shown before the API confirms it.
- [ ] 3.1.11 Build payment detail at `frontend/app/dashboard/payments/[id]/page.tsx` with Summary, Attempts, Events and Journal tabs, a state-history timeline, the masked idempotency key and the correlation ID.
- [ ] 3.1.12 Poll non-terminal payments (`created`, `processing`, `unknown`) with a manual refresh and stop at terminal states; show `unknown` as "Outcome unknown — checking status" with an explanation of reconciliation.
- [ ] 3.1.13 Add loading skeletons, empty states, filtered-empty states, read-error states (with request ID and Retry) and stale-data states to the overview, list and detail pages.

**Acceptance gate**
- [ ] A double-click or a resend after a network timeout creates exactly one payment, and the request log shows the same `Idempotency-Key`.
- [ ] Amounts above 2^53 round-trip and display exactly; no money value passes through a floating-point `Number`.
- [ ] Before the API confirms a terminal state, no payment shows as succeeded.
- [ ] An `unknown` payment shows no retry or alternate-provider action on any screen.
- [ ] Filters, search (including `merchant_reference`), sort and cursor survive a page refresh and back navigation.
- [ ] Overview volumes are grouped by currency, and simulated or seeded series are labelled as such.
- [ ] Each screen renders correct loading, empty, error and stale states when the API is slow, returns nothing, returns a 5xx, or is offline.
- [ ] At 375px and 1440px there is no horizontal page scroll, and every action can be reached by keyboard.
- [ ] axe reports no serious or critical violations; dialogs trap focus, close with Escape and return focus to their trigger.
- [ ] The Vitest and Playwright suites pass in CI against the real mock-backed API.

**E2E test** — `e2e/ui/payments.spec.ts`
- [ ] Creating a sandbox payment in the dialog shows it as pending, then shows its real final state on the detail page (never an optimistic success)
- [ ] Double-clicking submit, and reloading while pending, still produce one payment because the idempotency key is preserved
- [ ] An `unknown` payment shows "Outcome unknown" with an explanation and no retry button
- [ ] List filters, sorting and cursor pagination survive reload and back navigation (state lives in the URL)
- [ ] With the API stopped, pages show an error with the request ID and a Retry action, and no stale data presented as current
- [ ] Overview metrics show their source and never sum different currencies
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 3.2 Routing and simulation UI — frontend + backend
**Depends on:** 2.2, 3.1 · **Deliverable:** Routing inspection and evaluation, provider diagnostics, and a backend-driven simulator with persisted runs.

**Tasks**
- [ ] 3.2.1 Implement `GET /v1/routing/rules` and a non-executing `POST /v1/routing/evaluate` in `backend/internal/routing` and `backend/internal/transport/http`; evaluation returns the matched rule, the capability checks and a reason, and writes nothing.
- [ ] 3.2.2 Build the routing page at `frontend/app/dashboard/routing/page.tsx`: a read-only rule table (priority, rail, currency, amount range, provider, enabled) and an evaluator form showing the request → matching rule → capability check → result path.
- [ ] 3.2.3 Show rule create, edit and reorder controls as disabled, with the reason "requires rule mutation contract".
- [ ] 3.2.4 Implement `GET /v1/providers/health`, computed from attempt `submitted_at`/`completed_at` (success rate, p95, error counts per window); return "no observations" instead of zeros when there is no data.
- [ ] 3.2.5 Build the providers list and detail pages at `frontend/app/dashboard/providers/page.tsx` and `[id]/page.tsx`: capability cards, Mock marked "Simulated", Stripe and Plaid marked "Not configured", and a health badge only when there is evidence.
- [ ] 3.2.6 Add the `simulation_runs` migration (scenario, payment_id, injected fault, invariant results, status, timestamps) and the repository in `backend/internal/storage`.
- [ ] 3.2.7 Implement `POST /v1/sandbox/simulations` and `GET /v1/sandbox/simulations/{id}`. Only scenarios the backend actually implements are accepted (success, decline, timeout before execution, timeout after success, duplicate request, duplicate webhook, reordered events, outage, unknown-outcome recovery, duplicate ledger post); anything else returns 422.
- [ ] 3.2.8 Have the backend check each run's invariants: one payment per key, one balanced journal, no cross-provider attempt after `unknown`, and no regression of a terminal state. Store pass/fail with evidence in `simulation_runs`.
- [ ] 3.2.9 Build the simulator page at `frontend/app/dashboard/simulator/page.tsx`: configuration form, execution timeline, and inspector tabs (Request, Response, Routing, Attempts, Events, Journal, Invariants) under a persistent "SIMULATED / SANDBOX ONLY" badge.
- [ ] 3.2.10 Drive the timeline by polling the backend with a manual refresh, announcing run acceptance once through `aria-live`; it shows no made-up progress animation, and invariants stay "pending" until the backend returns evidence.
- [ ] 3.2.11 Fetch the scenario list from the backend so that unimplemented scenarios render disabled with the missing dependency named.
- [ ] 3.2.12 Add Go integration tests (file-backed SQLite) for evaluate-has-no-side-effects and for each simulation scenario's invariant results.

**Acceptance gate**
- [ ] Calling `POST /v1/routing/evaluate` creates no payment, attempt, outbox or ledger rows, as shown by row counts before and after.
- [ ] The UI shows an "unsafe failover blocked" explanation when a prior attempt is unresolved.
- [ ] Re-running a scenario with the same seed gives identical states and invariant results.
- [ ] The "timeout after success" run ends with one payment, one provider charge, one balanced posted journal, and no attempt with a second provider.
- [ ] Every simulator, provider and metric surface carries a visible "Simulated" or "Not configured" label, and no green health badge appears without observations.
- [ ] Disabled controls explain what they depend on; no enabled control fakes a successful operation.
- [ ] The timeline and inspector tabs work at 375px (labelled scroll region) and 1440px, and are keyboard-operable; screen readers announce run acceptance once, not on every poll.
- [ ] The Go integration tests and Playwright flows pass in CI.

**E2E test** — `e2e/ui/simulator.spec.ts`, `e2e/ui/routing.spec.ts`
- [ ] Simulator "timeout after success": the timeline shows `unknown`, recovery resolves it to `succeeded`, the Invariants tab shows every check passed, and the Journal tab shows exactly one journal
- [ ] Each enabled scenario completes with the backend-reported result; scenarios that aren't implemented are disabled with a reason
- [ ] A simulator run persists: reloading the page shows the same run from `simulation_runs`
- [ ] Routing evaluation shows the matched rule and reason and creates no payment
- [ ] Provider cards distinguish "Simulated", "Not configured" and "No observations"
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

## 4. Financial operations (M4)

### 4.1 Ledger explorer — frontend + backend
**Depends on:** 2.3, 3.1 · **Deliverable:** Read-only ledger account and journal views with posting verification.

**Tasks**
- [ ] 4.1.1 Implement `GET /v1/ledger/accounts` (code, kind, currency, provider, balance computed from posted entries) with workspace scoping.
- [ ] 4.1.2 Implement `GET /v1/ledger/transactions` (posted journals only) with cursor pagination and filters for currency and payment ID; amounts are returned as decimal strings.
- [ ] 4.1.3 Implement `GET /v1/ledger/transactions/{id}` returning entries, debit and credit totals, the reversal link (`reversal_of` and reversed-by), and the related payment.
- [ ] 4.1.4 Compute balances and totals with checked int64 arithmetic; on overflow, return an explicit error rather than a rounded value.
- [ ] 4.1.5 Build the ledger page at `frontend/app/dashboard/ledger/page.tsx` with Accounts and Journal tabs, currency and payment-ID filters held in the URL, and per-currency grouping (no cross-currency totals).
- [ ] 4.1.6 Build journal detail at `frontend/app/dashboard/ledger/[id]/page.tsx` showing a "POSTED / IMMUTABLE" badge, entry lines with side and amount, and a "debits equal credits" check computed from the returned lines.
- [ ] 4.1.7 Show reversals as separate journals linked in both directions; there is no edit or delete action anywhere.
- [ ] 4.1.8 Link the payment detail Journal tab to the ledger detail page, and link ledger detail back to the payment.
- [ ] 4.1.9 Add Go tests: draft journals are never returned, balances match the sum of entries, workspaces are isolated, and overflow is rejected.
- [ ] 4.1.10 Add component tests for journal detail (balanced, reversal-linked, and large-amount cases) in `frontend/tests/components/ledger.test.tsx`.

**Acceptance gate**
- [ ] For every journal, the debit and credit totals shown equal the database sums, as checked by a test against seeded postings.
- [ ] Draft journals never appear in the API or the UI.
- [ ] No UI path or API endpoint can modify or delete a posted journal or entry.
- [ ] Balances are never added together across currencies.
- [ ] Requests using another workspace's journal IDs return 404.
- [ ] Ledger tables have correct loading, empty and error states and remain readable at 375px (entry cards) and 1440px.
- [ ] axe reports no serious or critical violations, and amounts use tabular numerals.

**E2E test** — `e2e/ui/ledger.spec.ts`
- [ ] The journal for a succeeded payment shows balanced debit and credit lines, with a "Posted / immutable" label
- [ ] Navigation works in both directions: payment detail to journal, and journal to payment
- [ ] The ledger has no edit or delete control anywhere; balances are shown per currency only
- [ ] Large amounts display exactly (beyond `Number.MAX_SAFE_INTEGER`)
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 4.2 Reconciliation — backend + frontend
**Depends on:** 2.4, 4.1 · **Deliverable:** Rerunnable reconciliation runs against mock-provider evidence, with stored discrepancies and resolution history.

**Tasks**
- [ ] 4.2.1 Add a migration with a CHECK on `reconciliation_discrepancies.kind` (`missing_posting`, `unknown_outcome`, `amount_mismatch`, `status_mismatch`, `missing_at_provider`) and add count columns or a view for matched, unmatched and mismatch totals.
- [ ] 4.2.2 Implement a reconciliation service in `backend/internal/reconciliation` that compares mock-provider charges, payment state and posted journals over a time window.
- [ ] 4.2.3 Make runs idempotent using `run_key` and `(run_id, fingerprint)`, so that re-running the same window creates no duplicate discrepancies.
- [ ] 4.2.4 Ensure the service never creates journals or changes payment state to resolve a mismatch; resolution only records evidence.
- [ ] 4.2.5 Implement `GET /v1/reconciliation/runs` and `GET /v1/reconciliation/discrepancies` (filtered by run, kind and status, with a cursor).
- [ ] 4.2.6 Define an authorized, idempotent `POST /v1/reconciliation/runs` contract in OpenAPI, and implement it only once operator permission is enforced.
- [ ] 4.2.7 Implement discrepancy resolution (status change plus `resolution_note`, `resolved_by` and `resolved_at`), restricted to operators and append-only in history.
- [ ] 4.2.8 Build the reconciliation page at `frontend/app/dashboard/reconciliation/page.tsx`: run history selector, count cards, discrepancy table, and a detail pane (evidence → resolution history → related journal).
- [ ] 4.2.9 Show the "Run sandbox reconciliation" control as disabled until 4.2.6 ships; afterwards it shows a confirmation, active run status, and handling for duplicate runs.
- [ ] 4.2.10 Add Go tests covering each discrepancy kind, rerun idempotency, the no-invented-adjustment rule, and resolution permissions.

**Acceptance gate**
- [ ] Running the same window twice leaves identical discrepancy rows (same count and fingerprints).
- [ ] A run creates no ledger journals and no payment transitions, as shown by row counts before and after.
- [ ] Each kind of discrepancy is detected by its fixture test.
- [ ] A discrepancy cannot be marked resolved without a note, a resolver and a timestamp (enforced in the database and the API).
- [ ] Mock evidence is labelled "Simulated" in the UI.
- [ ] Exact amounts and explanation text survive the 375px card layout; the 1440px table is keyboard-navigable.
- [ ] Loading, empty ("no discrepancies") and error states render correctly.

**E2E test** — `e2e/ui/reconciliation.spec.ts`
- [ ] A seeded `unknown` payment where the provider confirmed success shows a `status_mismatch`/`unknown_outcome` discrepancy linked to the payment
- [ ] Re-running with the same run key creates no duplicate discrepancies
- [ ] Resolving a discrepancy requires a note, records who resolved it and when, and creates no adjusting journal
- [ ] The run history shows matched and unmatched counts that agree with the API
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 4.3 Events and audit — backend + frontend
**Depends on:** 2.4, 1.3 · **Deliverable:** Durable inbox deduplication, an append-only payment audit, and a read-only event explorer.

**Tasks**
- [ ] 4.3.1 Add a migration for `webhook_inbox.duplicate_count` and `last_received_at`; on a duplicate delivery, increment the count instead of inserting again.
- [ ] 4.3.2 Record a duplicate event ID whose payload hash differs as an investigation item (a flag plus the stored hash), never as a silent overwrite.
- [ ] 4.3.3 Make inbox processing apply payment and attempt transitions guarded by status; out-of-order events must never move a terminal state backwards.
- [ ] 4.3.4 Enforce that `payment_state_transitions` is append-only (trigger) and readable as a timeline for each payment.
- [ ] 4.3.5 Implement `GET /v1/events` combining inbox and outbox records, with filters for payment or correlation ID, state and date, and cursor pagination.
- [ ] 4.3.6 Implement `GET /v1/events/{id}` returning only safe fields: provider event ID, processing status, duplicate count, related payment and attempts, and a redacted payload summary.
- [ ] 4.3.7 Mark mock events "Signature: Not applicable"; only a backend-recorded verification may show "Verified" or "Rejected".
- [ ] 4.3.8 Build the events page at `frontend/app/dashboard/events/page.tsx` with filters in the URL, a Refresh button, a source and updated-time label, and a duplicate column fed by `duplicate_count`.
- [ ] 4.3.9 Build event detail without rendering raw JSON as HTML; show an "Open trace" link only when a trace destination is configured.
- [ ] 4.3.10 Add Go tests covering duplicate webhooks (count increments, single effect), changed-hash duplicates (flagged), reordered events (no regression), and attempts to delete audit rows (rejected).

**Acceptance gate**
- [ ] N duplicate deliveries produce one inbox row with `duplicate_count = N-1` and exactly one financial effect.
- [ ] A late `failed` event after `succeeded` leaves the payment `succeeded`, and the event is recorded as ignored.
- [ ] Rows in `payment_state_transitions` cannot be updated or deleted (the database rejects it).
- [ ] No secrets, tokens, full idempotency keys or raw payloads appear in API responses or the DOM.
- [ ] Mock events never claim cryptographic verification.
- [ ] The events list and detail pass checks for loading, empty and error states and keyboard access, at both 375px and 1440px.

**E2E test** — `e2e/ui/events.spec.ts`
- [ ] The duplicate-webhook scenario shows a duplicate count of 2 on the events page, and there is exactly one ledger posting
- [ ] Reordered events never move a terminal payment back to an earlier state (checked through the payment's state history)
- [ ] An event detail page shows only safe fields, links to its payment and correlation ID, and never claims a signature on mock events
- [ ] The audit timeline on payment detail matches the API's transition history
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 4.4 Docs and settings UI — frontend
**Depends on:** 3.1, 1.3 · **Deliverable:** Developer docs that match the OpenAPI contract, and a settings page exposing only supported actions.

**Tasks**
- [ ] 4.4.1 Build the docs page at `frontend/app/docs/page.tsx`, server-rendered, with sections for getting started, authentication, payments, idempotency, webhooks, routing, errors and limitations.
- [ ] 4.4.2 Generate or verify the request/response examples (cURL, TypeScript, Go) from the OpenAPI document, using placeholder credentials and sandbox paths only.
- [ ] 4.4.3 Document retry safety: reuse the same key after a timeout, use a new key for a new intent, and an `unknown` outcome means poll rather than retry or switch provider.
- [ ] 4.4.4 Separate supported endpoints from roadmap items (capture, refunds, rule editing are marked planned).
- [ ] 4.4.5 Add copy buttons that announce success or failure through `aria-live`, and collapsible section navigation for mobile.
- [ ] 4.4.6 Build the settings page at `frontend/app/dashboard/settings/page.tsx` with theme selection (System/Light/Dark, stored locally), the workspace name, and environment shown as fixed "Sandbox".
- [ ] 4.4.7 Show provider configuration as read-only (Mock enabled, Stripe and Plaid not configured); API key, role and live-activation controls are shown as unavailable.
- [ ] 4.4.8 Enforce workspace membership on every settings read in the API; never return secret values.
- [ ] 4.4.9 Add a CI check that the docs examples validate against the OpenAPI schema.
- [ ] 4.4.10 Add component tests for the theme toggle, the unavailable states, and copy announcements.

**Acceptance gate**
- [ ] Every code example on `/docs` passes validation against the current OpenAPI document in CI.
- [ ] No secrets or real credentials appear in the docs, the settings page or the frontend bundle (CI checks the bundle).
- [ ] The settings page has no fake key generation, membership management or live-activation buttons.
- [ ] Requests for another workspace's settings are rejected.
- [ ] The theme choice persists locally and both themes meet contrast requirements.
- [ ] Docs and settings work at 375px and 1440px, are keyboard-navigable, and axe reports no serious or critical violations.

**E2E test** — `e2e/ui/docs-settings.spec.ts`
- [ ] Every example on `/docs` runs against the live sandbox API and returns the documented response
- [ ] The settings page offers only supported actions (theme); live activation and key generation are absent or shown as unavailable
- [ ] The theme choice persists across reload, and the Sandbox badge stays visible in both themes
- [ ] Secret values never appear in page HTML or network responses
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

## 5. Verification and delivery (M5–M6)

### 5.1 End-to-end correctness — QA + engineers
**Depends on:** 3.2, 4.1–4.4 · **Deliverable:** Automated Go integration/failure suites, Vitest component tests and Playwright critical flows running in CI against the deterministic mock backend.

**Tasks**
- [ ] 5.1.1 Add a shared Go test helper in `backend/tests/` that creates a temporary file-backed SQLite database, applies all migrations, and applies the production connection PRAGMAs
- [ ] 5.1.2 Write concurrency tests: N goroutines `POST /v1/payments` with the same `Idempotency-Key` → exactly one payment, one outbox row, identical replayed responses
- [ ] 5.1.3 Write failure-injection tests for crash before submit, crash after provider acceptance, timeout after success, and stale lease recovery, using the mock adapter's durable state
- [ ] 5.1.4 Write accounting tests: duplicate success result, rollback during posting, mixed-currency entries, int64 overflow, unbalanced journal, edits to posted rows
- [ ] 5.1.5 Write workspace isolation tests: forged workspace IDs in body/path, cross-workspace foreign references, list/detail reads with another workspace's session
- [ ] 5.1.6 Write webhook/event tests: duplicate delivery, reordered events, and payload-hash mismatch on a duplicate event ID; terminal states never regress
- [ ] 5.1.7 Add Vitest + React Testing Library tests in `frontend/tests/components/` for money formatting (above `Number.MAX_SAFE_INTEGER`), unknown-outcome display, double-submit prevention, and loading/empty/error states
- [ ] 5.1.8 Add Playwright specs in `e2e/ui/` for create payment → detail, simulator "timeout after success" → resolved, ledger journal balance view, and API-offline error states
- [ ] 5.1.9 Run Playwright at 375 px and 1440 px widths with keyboard-only navigation and an automated accessibility scan (e.g. axe) on every dashboard route
- [ ] 5.1.10 Add a CI workflow (`.github/workflows/ci.yml`) that runs `gofmt` check, `go vet`, `go test ./...`, `pnpm lint`, `pnpm typecheck`, `pnpm build`, Vitest and `pnpm test:e2e` against the Go API started with the mock provider
- [ ] 5.1.11 Publish test reports and Playwright traces as CI artifacts; record actual results in `docs/roadmap.md`

**Acceptance gate**
- [ ] CI is green on `main`, with Go integration, Vitest and Playwright jobs all executed (not skipped)
- [ ] Every row of the verification matrix below links to a named test or a recorded manual result
- [ ] Concurrent same-key test reliably produces exactly one payment across ≥100 repeated runs
- [ ] Timeout-after-success flow ends with one succeeded payment, one posted balanced journal, and zero additional provider charges
- [ ] Integration tests use a file-backed SQLite database (no `:memory:`-only coverage for concurrency)

**E2E test** — all of `e2e/` (consolidation)
- [ ] The full suite runs in CI on every PR against the Go API with the mock provider, with traces and reports uploaded
- [ ] The suite passes 3 consecutive CI runs without retries (a flake check), with the central-demo spec repeated 20 times in one of those runs
- [ ] Every row of the verification matrix that can be covered end to end links to a named spec
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 5.2 Container delivery and recovery — platform
**Depends on:** 5.1 for full delivery acceptance; basic image/Compose source is explicitly authorized alongside M1. **Deliverable:** Reproducible images and a `docker compose` sandbox with one API-owned data volume, backup tooling, and a verified restore drill.

Image definitions, Compose wiring and the volume mount are added; `pnpm compose:config` passes. Actual image builds, non-root runtime permissions, persistence, backup/restore and digest pinning remain unverified or unimplemented. The Docker socket is inaccessible to this authoring sandbox; M1 dependency lockfiles also remain unresolved.

**Tasks**
- [x] 5.2.1 Write `infrastructure/docker/api.Dockerfile`: multi-stage Go build, static binary, numeric non-root user, `/var/lib/railvanta` as the only writable data path
- [x] 5.2.2 Write `infrastructure/docker/web.Dockerfile`: Next.js standalone output, non-root user, no secrets or `NEXT_PUBLIC_*` credentials baked in
- [x] 5.2.3 Write `docker-compose.yml` with `web`, `api`, and a same-origin reverse proxy that routes `/v1/*` (path preserved) to the API and everything else to the web container
- [x] 5.2.4 Mount one named volume for the whole data directory (`railvanta.db`, `-wal`, `-shm`) on the API container only; no other container mounts it
- [ ] 5.2.5 Implement `/health/startup`, `/health/live`, `/health/ready`; wire Compose healthchecks; keep health endpoints off the public proxy route
- [ ] 5.2.6 Implement graceful shutdown: go unready, stop dispatcher claims, drain bounded in-flight work, close the database, exit
- [ ] 5.2.7 Implement API-owned backup using the SQLite online backup API (or `VACUUM INTO`), writing snapshot time, schema version, checksum and release metadata
- [ ] 5.2.8 Document and script a restore: stop and fence the API, preserve the old volume, restore into a clean directory, run `PRAGMA integrity_check` / `foreign_key_check` and domain invariant checks, then start a matching release
- [ ] 5.2.9 Add a domain-check command (e.g. `railvanta-api check`) verifying all posted journals balance, no orphaned attempts, and payment/journal consistency
- [ ] 5.2.10 Add `.env.example` with placeholders only; confirm images and Git contain no secrets (secret scan in CI)
- [ ] 5.2.11 Build images in CI with pinned base-image digests and record image digests per release

**Acceptance gate**
- [ ] `docker compose up` on a clean machine yields a working sandbox reachable at one origin
- [ ] Killing the API container mid-dispatch and restarting recovers work without duplicate ledger postings
- [ ] Restore drill from a backup taken under write load passes integrity, foreign-key and domain checks; the result is recorded with date and release
- [ ] No backup procedure copies only the live `.db` file
- [ ] Containers run as non-root; secret scan reports no findings

**E2E test** — run `pnpm test:e2e` with `E2E_BASE_URL` set to the `docker compose` stack
- [ ] The full suite passes against the built containers, not just dev processes
- [ ] Backup, stop, restore into a clean volume, start: the restore spec confirms the payments, journals and idempotency replays recorded before the backup
- [ ] Restarting the API container mid-dispatch recovers without a second charge (the 2.4 crash spec run against containers)
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 5.3 Kubernetes reference deployment (optional) — platform
**Depends on:** 5.2 · **Deliverable:** Reviewed manifests matching [kubernetes.md](operations/kubernetes.md), validated in a disposable local cluster.

**Tasks**
- [ ] 5.3.1 Confirm a free, local disposable cluster (kind, k3d or minikube); no paid cloud resources without explicit authorization
- [ ] 5.3.2 Write manifests under `infrastructure/k8s/`: Namespace, ConfigMap, Secret placeholders, ServiceAccounts with token automount disabled
- [ ] 5.3.3 Write API StatefulSet with `replicas: 1`, headless Service, `volumeClaimTemplates` mounting `/var/lib/railvanta`, and no HPA
- [ ] 5.3.4 Write web Deployment and ClusterIP Services (`railvanta-web :3000`, `railvanta-api :8080`)
- [ ] 5.3.5 Write Ingress: `/v1` Prefix → API, `/` → web, no path stripping, TLS Secret; install a compatible ingress controller in the test cluster
- [ ] 5.3.6 Add NetworkPolicies (default deny plus listed flows) and verify with an enforcing CNI
- [ ] 5.3.7 Set pod security context: `runAsNonRoot`, dropped capabilities, `seccompProfile: RuntimeDefault`, read-only root filesystem with explicit writable mounts
- [ ] 5.3.8 Configure startup/liveness/readiness probes and `terminationGracePeriodSeconds` matching the API's drain policy
- [ ] 5.3.9 Choose PVC access mode (`ReadWriteOncePod` where supported) and reclaim policy `Retain`
- [ ] 5.3.10 Validate manifests in CI (`kubectl apply --dry-run=server` or kubeconform) with pinned image digests

**Acceptance gate**
- [ ] Manifests apply cleanly to the disposable cluster and the sandbox works through the Ingress over TLS
- [ ] Deleting the API pod results in a replacement that opens the volume only after the old pod exited, with no duplicate postings
- [ ] Backup/restore drill passes in the cluster on a separate volume
- [ ] No second API replica, worker Deployment, migration Job or backup CronJob writes the live database
- [ ] Cluster is torn down after validation; no billable resources remain

**E2E test** — run `pnpm test:e2e` with `E2E_BASE_URL` set to the disposable cluster's ingress (optional)
- [ ] Smoke and payment specs pass through the ingress over TLS, with `/v1` routed to the API and the path preserved
- [ ] Deleting the API pod and letting the StatefulSet replace it: the replay and recovery specs still pass on the same PVC
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

### 5.4 Release documentation — all
**Depends on:** 5.2; 5.3 if used · **Deliverable:** Tested README and docs describing implemented behavior, known limits and measured results.

**Tasks**
- [ ] 5.4.1 Write root `README.md` with prerequisites, local dev, Compose, test, lint and format commands, each run on a clean checkout
- [ ] 5.4.2 Update `AGENTS.md` build/test command section to the real scripts
- [ ] 5.4.3 Update `docs/architecture.md`, `docs/backend/*` and `docs/frontend/*` from "planned" to implemented behavior; mark remaining gaps explicitly
- [ ] 5.4.4 Publish the OpenAPI contract and confirm `/docs` examples match it
- [ ] 5.4.5 Record measured latency, recovery outcomes and test counts with the date, commit and environment they were measured in
- [ ] 5.4.6 Write a known-limitations section: single SQLite writer, no HA, mock provider only, no real funds, deferred capabilities
- [ ] 5.4.7 Document the backup/restore runbook with the date of the last successful drill
- [ ] 5.4.8 Write the short technical case study and record demo scenarios as reproducible scripts or Playwright specs
- [ ] 5.4.9 Add ADRs for any implementation decision that diverged from the design docs

**Acceptance gate**
- [ ] A reviewer following only the README reaches a running sandbox and green tests on a clean machine
- [ ] Every claimed metric or capability has linked evidence (test, CI run or recorded measurement)
- [ ] No doc describes unimplemented behavior as available

**E2E test** — `e2e/docs/quickstart.spec.ts`
- [ ] On a fresh clone, run the README quickstart commands exactly as written, then run the smoke and central-demo specs; both pass
- [ ] Each scripted demo scenario in the case study corresponds to a passing spec
- [ ] `pnpm test:e2e` passes in full: this spec plus every earlier package's specs

## Verification matrix

### Idempotency
- [ ] Concurrent requests with the same key — evidence: one payment row, one outbox row, identical `202` bodies across all responses
- [ ] Same key, changed payload — evidence: `409` with error code; no new payment or outbox row
- [ ] Lost response (client retries after commit) — evidence: replayed stored response with the same payment ID
- [ ] API restart between requests — evidence: replay after restart returns the original response

### Dispatch
- [ ] Crash before provider call — evidence: attempt resolved via original provider lookup; at most one mock charge
- [ ] Crash after possible provider acceptance — evidence: attempt `unknown` → resolved against original provider; no second submission or alternate provider
- [ ] Stale lease — evidence: expired owner's completion rejected by claim-token check; work recovered without blind re-execution

### Accounting
- [ ] Duplicate success result — evidence: one posted journal (unique posting key), second attempt rejected or a no-op
- [ ] Rollback during posting — evidence: no draft or partial journal remains; payment state unchanged
- [ ] Mixed currency — evidence: entry insert rejected by composite foreign key
- [ ] int64 overflow — evidence: amount or aggregation rejected; posting fails without rounding

### Isolation
- [ ] Forged workspace IDs — evidence: requests derive the workspace from the session; foreign IDs return 404/403
- [ ] Cross-workspace foreign keys — evidence: inserts referencing another workspace's rows rejected
- [ ] Cache switch between workspaces — evidence: frontend query cache keyed by workspace; no stale cross-workspace data rendered

### UI
- [ ] Double submit — evidence: one request sent; same idempotency key retained on retry
- [ ] Reload during pending payment — evidence: state reloaded from API; no optimistic success shown
- [ ] API outage — evidence: honest error state with request ID and safe retry for reads only
- [ ] Empty results — evidence: screen-specific empty state shown
- [ ] Mobile keyboard use — evidence: Playwright at 375 px with keyboard-only navigation, focus trapped and restored in dialogs and drawers

### Operations
- [ ] Disk full — evidence: writes fail with bounded errors; readiness reflects degraded state; no corrupt database
- [ ] SQLite busy — evidence: bounded retry of DB-only transactions; no provider call repeated
- [ ] Migration failure — evidence: API stays unready; database unchanged or restorable from pre-migration backup
- [ ] Restore — evidence: restored dataset passes integrity, foreign-key and domain checks

