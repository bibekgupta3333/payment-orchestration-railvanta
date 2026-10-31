# Railvanta

Sandbox payment infrastructure built with Go, GORM, SQLite, Next.js, and Tailwind CSS. See the [project brief](idea.md), [architecture](docs/architecture.md), and [WBS](docs/wbs.md).

M1 source now includes the API scaffold, versioned storage, authentication/validation boundary, frontend shell, and test/CI harness. Dependency-backed compilation, browser tests, and CI still require verification. Payment execution, routing, ledger posting services, and provider integrations belong to later milestones. The UI deliberately shows unavailable states for those capabilities.

## Prerequisites and installation

- Node.js 22 (see `.nvmrc`) and pnpm 11.3.0.
- Go 1.26 and a C compiler for the SQLite driver.
- Python 3 for the E2E harness's controlled offline corruption fixtures.
- Network access for initial dependency downloads and Chromium installation.

Run commands from the repository root:

```sh
pnpm install
pnpm deps:api
pnpm generate:api
pnpm --filter @railvanta/e2e exec playwright install chromium
pnpm format
pnpm check
pnpm test:e2e
```

The first dependency installation must update `pnpm-lock.yaml` and generate `backend/go.sum`; these could not be completed in the restricted authoring environment. Once generated, retain both files and use `pnpm install --frozen-lockfile` for reproducible installs. Do not handwrite dependency checksums. The API type generator writes `frontend/lib/api/schema.d.ts`; `pnpm check:generated` rejects a missing or stale artifact.

## Docker Compose sandbox

The required container workflow uses separate images for the Go API and Next.js frontend, plus an unprivileged Nginx proxy. After resolving the M1 dependency files above, use Docker Desktop / Docker Engine with Compose v2:

```sh
pnpm compose:config
pnpm compose:up
```

The underlying command is `docker compose up --build --detach --wait`. Open `http://localhost:3000`; set `RAILVANTA_PORT` to choose another host port. Only the proxy is published on host loopback. API `/v1` requests preserve their path, and the private API health port is not published. Readiness health checks order startup.

```sh
pnpm compose:logs
pnpm compose:down
```

Stopping the stack retains the `railvanta-data` volume. Only the API mounts it, including SQLite's WAL/SHM files. Do not scale API replicas while using SQLite. Demo seeding stays disabled in containers because its trust boundary is a literal loopback API bind; the unauthenticated shell remains available and protected API requests require separately provisioned credentials.

Both application images use multi-stage builds and non-root runtimes. The frontend copies standalone output and static/public assets. Its nonsecret `API_INTERNAL_ORIGIN=http://api:8080` rewrite destination is set during the image build; changing that destination requires rebuilding. Source credentials and local database files are excluded by `.dockerignore`.

**Verification status:** `pnpm compose:config` passes. Actual builds/startup are unverified: the authoring sandbox cannot access the Docker daemon socket, the pnpm lockfile still needs updating, and `backend/go.sum` is missing. The API Dockerfile deliberately requires those real Go checksums, and the web Dockerfile requires the frozen lockfile. Backup/restore and production image digest pinning remain WBS 5.2 work. The current E2E harness still starts isolated native processes; running the suite against Compose is a separate pending WBS 5.2 gate.

## Native development alternative

Start the API in one terminal:

```sh
APP_ENV=sandbox HTTP_ADDR=127.0.0.1:8080 pnpm dev:api
```

Start Next.js in another:

```sh
API_INTERNAL_ORIGIN=http://127.0.0.1:8080 pnpm dev:web
```

Open `http://localhost:3000`. Next.js preserves `/v1/*` when forwarding to Go. The default private health listener is `127.0.0.1:8081`; `/health/ready`, `/health/live`, and `/health/startup` are not public API routes. The SQLite path defaults to `backend/data/railvanta.db` when run through the root scripts. Migrations finish before the service becomes ready.

The binary requires an explicit `APP_ENV=sandbox`. Any other or missing value fails startup. Environment files are examples only; the binary does not automatically load them. Supported settings are in [backend/.env.example](backend/.env.example) and [frontend/.env.example](frontend/.env.example).

## Authenticated API development

The default API has no automatic public credentials or login screen. Sessions use hashed server-side tokens, expiry, a Secure/HttpOnly/SameSite session cookie, and origin-bound CSRF tokens for writes. API keys use `prefix.secret`, hashed storage, workspace scope, and read/operator permissions.

For local fixtures, set `DEMO_MODE=true` with a literal loopback `HTTP_ADDR` and provide all three `DEMO_OPERATOR_KEY`, `DEMO_READER_KEY`, and `DEMO_OTHER_WORKSPACE_KEY` values. Give each a distinct prefix and a freshly generated random secret of at least 24 characters. Optional `DEMO_SESSION_TOKEN` and `DEMO_CSRF_TOKEN` must be supplied together. Generate secrets locally, keep them out of Git, and reuse them across restarts; seeding never resets revoked or existing keys.

The E2E harness generates these credentials in temporary storage automatically. Ordinary API startup does not seed demo records. Secure cookies require HTTPS for normal browser use (Chromium also treats loopback HTTP as secure, which the E2E harness relies on); do not remove the Secure flag to make local sessions work. Without a session the shell still renders, and the overview and payment pages show an error with the request ID instead of data.

The dashboard sends `X-CSRF-Token` on writes from a readable `railvanta_csrf` cookie; the session cookie itself stays HttpOnly. Whatever establishes a browser session must set both (the E2E helper `e2e/support/browser.ts` does this for the seeded demo session).

Implemented reads include `/v1/workspace`, `/v1/workspaces/{id}`, `/v1/payment-methods`, `/v1/payment-methods/{id}`, and `/v1/session`. Operator API keys can revoke themselves through `DELETE /v1/api-keys/current`. `POST /v1/payments` accepts payments idempotently (WBS 2.1). `GET /v1/payments` supports search (`q`: exact ID or merchant reference), status, currency, rail, provider, date and amount filters, and `sort=created_desc|created_asc` with sort-bound cursors. WBS 3.1 adds `GET /v1/payments/{id}/history` (state transitions, outbox events, masked idempotency key) and `GET /v1/dashboard/summary` and `/v1/dashboard/payment-volume` (`window=24h|7d|30d`; exact per-currency decimal-string volumes; `null` success rate and p95 without observations).

## Checks and tests

| Command | Purpose |
| --- | --- |
| `pnpm format` / `pnpm format:check` | Go and workspace formatting |
| `pnpm lint` | Go format check, vet, golangci-lint, frontend lint, OpenAPI lint |
| `pnpm typecheck` | Workspace TypeScript checks |
| `pnpm test` | File-backed Go integration tests and frontend unit tests |
| `pnpm test:api:foundation` | Dependency-free config and exact-money tests |
| `pnpm build` | Go and Next.js builds, including client-bundle secret sentinels |
| `pnpm generate:api` / `pnpm check:generated` | Generate/check frontend contracts from OpenAPI |
| `pnpm check:tracked` | Reject tracked credentials and database files |
| `pnpm check` | Whole-repository pre-PR gate |
| `pnpm test:e2e` | Cumulative black-box API and browser tests |

The E2E harness builds the API, starts isolated loopback services on free ports, uses a temporary on-disk SQLite database and generated credentials, and tears processes down. It tests graceful restart, SIGKILL recovery, checksum refusal, auth, proxy path preservation, responsive navigation, themes, and accessibility. Assertions use HTTP/browser interfaces; offline fixture setup may modify a stopped test database to test persistence and corrupt-history refusal. It adds no test-control endpoints to Go.

The workflow in `.github/workflows/ci.yml` installs dependencies and browsers, runs the checks and full E2E suite, and uploads Playwright reports. No PR or green CI run is claimed locally.

## Scope and safety

Only sandbox mode is supported. Use exact integer minor units, preserve idempotency boundaries, and reconcile ambiguous outcomes against the original provider. The single-owner SQLite design does not provide horizontal API scaling or database high availability. No cloud resources are created by these commands.

Record every change and observed check result in [CODECHANGELOG.md](CODECHANGELOG.md). Track completion against [docs/wbs.md](docs/wbs.md); implementation alone does not satisfy its verification and merge gates.
