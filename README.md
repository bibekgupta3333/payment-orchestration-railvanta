# Railvanta

Railvanta is a sandbox multi-rail payment orchestration platform: a Go/GORM/SQLite API (`backend/`), a Next.js operator dashboard (`frontend/`) and a Playwright black-box suite (`e2e/`). Payments are accepted idempotently, dispatched through a leased outbox to a deterministic mock provider, posted to a balanced double-entry ledger, and recovered by lookup against the original provider when an outcome is ambiguous. Only the sandbox mock provider exists; **no real funds move**.

Background: [project brief](idea.md), [architecture](docs/architecture.md), [documentation index](docs/README.md), [OpenAPI contract](backend/api/openapi.yaml) (also rendered with runnable examples on the dashboard's `/docs` page), [WBS and status](docs/wbs.md), [verification record](CODECHANGELOG.md).

## Prerequisites

| Tool | Version | Used for |
| --- | --- | --- |
| Node.js | 22 (`.nvmrc`) | frontend, scripts, Playwright |
| pnpm | 11.3.0 (`packageManager`; `corepack enable` selects it) | JS workspace |
| Go | 1.26 | API |
| C compiler (Xcode CLT, `build-essential`) | any | the CGO SQLite driver |
| Docker with Compose v2 | recent | container sandbox, container E2E, `pnpm check` (`docker compose config`) |
| Python 3 | 3.9+ | E2E harness offline-corruption fixtures |
| `openssl`, `curl` | any | the demo credential walkthrough |

Network access is needed once for dependency downloads and the Chromium install. Run every command from the repository root.

## Quickstart: install, verify, test

<!-- quickstart:start -->
```sh
pnpm install --frozen-lockfile
pnpm --filter @railvanta/e2e exec playwright install chromium
pnpm check
```
<!-- quickstart:end -->

`pnpm check` runs the workspace and tracked-file policy checks, `docker compose config`, lint (gofmt, vet, golangci-lint, ESLint, OpenAPI lint), generated-contract and `/docs`-example checks, typecheck, Go and Vitest tests, and the production builds. Then run the full black-box suite (builds the API, starts an isolated API and Next.js on free loopback ports with a temporary database and generated credentials, and cleans up):

```sh
pnpm test:e2e
```

Go dependencies are resolved from the committed `backend/go.sum`; `pnpm deps:api` (`go mod tidy`) is only needed when you change them.

## Run the sandbox natively

Terminal 1: the API in demo mode. Demo mode seeds a workspace, three API keys and one browser session, and is only allowed when the API binds a literal loopback address. Generate the secrets locally (never commit them); this uses a throwaway database so the keys always match it.

```sh
export APP_ENV=sandbox DEMO_MODE=true HTTP_ADDR=127.0.0.1:8080
export SQLITE_PATH=data/demo-$(date +%s).db
export DEMO_OPERATOR_KEY="oper.$(openssl rand -hex 16)"
export DEMO_READER_KEY="read.$(openssl rand -hex 16)"
export DEMO_OTHER_WORKSPACE_KEY="othr.$(openssl rand -hex 16)"
export DEMO_SESSION_TOKEN="$(openssl rand -hex 16)" DEMO_CSRF_TOKEN="$(openssl rand -hex 16)"
echo "operator key: $DEMO_OPERATOR_KEY"
pnpm dev:api
```

Terminal 2: the dashboard, proxying `/v1/*` to the API (open <http://localhost:3000>):

```sh
API_INTERNAL_ORIGIN=http://127.0.0.1:8080 pnpm dev:web
```

Terminal 3 (or any shell with the operator key from the `echo` line): create a payment against the seeded mock payment methods. Amounts are integer minor units sent as strings; `Idempotency-Key` makes retries safe.

```sh
KEY=<operator key printed above>
METHOD=00000000-0000-7000-8000-000000000121   # "Mock success"; list all: GET /v1/payment-methods
curl -s -H "Authorization: Bearer $KEY" localhost:3000/v1/payment-methods
curl -s -X POST localhost:3000/v1/payments \
  -H "Authorization: Bearer $KEY" -H "Idempotency-Key: demo-$(date +%s)" \
  -H "Content-Type: application/json" \
  -d "{\"amount_minor\":\"2500\",\"currency\":\"USD\",\"payment_method_id\":\"$METHOD\"}"
curl -s -H "Authorization: Bearer $KEY" localhost:3000/v1/payments
```

The other seeded methods drive the simulator scenarios (`...122` decline, `...123` delayed success, `...124` timeout before execution, `...125` timeout after success, `...126` outage); the `/docs` page lists runnable examples.

To see data in the dashboard, the browser needs the demo session. Without one the shell renders but data pages show an error with a request ID. In Chromium DevTools, Application, Cookies for `http://localhost:3000`, add `__Host-railvanta_session` = `$DEMO_SESSION_TOKEN` (Path `/`, Secure, HttpOnly, SameSite Strict) and `railvanta_csrf` = `$DEMO_CSRF_TOKEN` (Path `/`, Secure, SameSite Strict); Chromium treats loopback HTTP as secure. The `__Host-` prefix and `Secure` flag are deliberate; do not weaken them. The API health endpoints (`/health/live`, `/health/ready`, `/health/startup`) are on the private `HEALTH_ADDR` (default `127.0.0.1:8081`) and are not public routes.

The binary requires an explicit `APP_ENV=sandbox`; any other value fails startup. Environment files are examples only (`backend/.env.example`, `frontend/.env.example`); nothing auto-loads them. Seeding never resets existing keys, so reuse the same secrets with the same database across restarts, or pick a new `SQLITE_PATH`. Ordinary startup without `DEMO_MODE` seeds no credentials.

## Run the sandbox in Docker Compose

Separate images for the Go API and Next.js, plus an unprivileged Nginx proxy; only the proxy is published, on host loopback.

```sh
pnpm compose:up        # docker compose up --build --detach --wait
```

Open <http://localhost:3000> (`RAILVANTA_PORT=3100 pnpm compose:up` picks another host port). The data volume is named `railvanta_railvanta-data` regardless of project name, so a second stack needs both `COMPOSE_PROJECT_NAME=<name>` and `RAILVANTA_DATA_VOLUME=<name>-data`. Useful follow-ups:

```sh
pnpm compose:logs      # follow logs
pnpm compose:down      # stop; the railvanta-data volume (SQLite + WAL/SHM) is retained
docker compose down -v # also delete the volume
```

`DEMO_MODE` is off in containers (its trust boundary is a loopback bind), so the stack serves the shell and protected APIs need separately provisioned credentials: `docker compose exec` the API container's `railvanta-api seed-demo` with `DEMO_*` values in its environment (see [deployment](docs/operations/deployment.md#implemented-container-backup-restore-and-drill-wbs-52)). Do not scale API replicas with SQLite. Verify image builds, startup and persistence separately from `pnpm compose:config` (YAML validation only).

### Backup and restore

`scripts/backup-db.sh` takes a consistent online snapshot with a SHA-256 manifest; `scripts/restore-backup.sh <backup.db> [--start]` restores into a new volume after verification and preserves the old one; `scripts/restore-drill.sh` runs backup and restore under write load; `railvanta-api check` verifies integrity and ledger invariants. The runbook, drill record and supply-chain notes are in [docs/operations/deployment.md](docs/operations/deployment.md); optional Kubernetes manifests are in [docs/operations/kubernetes.md](docs/operations/kubernetes.md).

## Demo

Signed in as an operator, **Settings → API keys** generates a sandbox key (name, role, expiry), shows the secret once with a curl example, and can revoke it. For a live walkthrough of the Payment Failure & Recovery Playground: `pnpm demo:up` (start, seed throwaway credentials, print curl examples), `pnpm demo:open` (headed browser already signed in), `pnpm demo:down` (add `-v` to delete demo data). Script, Q&A, honest claims and fallbacks are in [docs/demo/README.md](docs/demo/README.md).

## Tests, lint and format

| Command | Purpose |
| --- | --- |
| `pnpm check` | Whole-repository pre-PR gate (see Quickstart) |
| `pnpm test` | Go tests (file-backed SQLite) and frontend unit tests |
| `pnpm test:api` / `pnpm test:web` | Go only / Vitest only |
| `pnpm test:api:foundation` | Dependency-free config and exact-money tests |
| `pnpm lint` / `pnpm typecheck` | Go format+vet+golangci-lint, ESLint, OpenAPI lint / workspace TypeScript |
| `pnpm format` / `pnpm format:check` | Apply / verify formatting (gofmt, Prettier) |
| `pnpm build` | Go and Next.js builds, including client-bundle secret sentinels |
| `pnpm generate:api` / `pnpm check:generated` | Regenerate / verify frontend types derived from the OpenAPI file |
| `pnpm check:docs` | Every `/docs` example validates against the OpenAPI contract |
| `pnpm check:tracked` | Reject tracked credentials and database files |
| `pnpm test:e2e` / `pnpm test:e2e:ci` | Full Playwright suite / same with retries disabled |
| `pnpm test:e2e:quickstart` | Opt-in: re-runs this README's Quickstart in a fresh clone, then the smoke and central-demo specs |

### End-to-end tests

Native mode (the default) needs no running services; the harness builds and starts everything it uses. Container mode runs the same suite, plus the `e2e/ops` specs (backup/restore, non-root runtime, private health, container kill), against a Compose stack on a **fresh** volume (run `docker compose down -v` first; a volume reused from `pnpm compose:up` made one spec fail in a trial run) with fast dispatcher timings. `down -v` deletes the data volume (`railvanta_railvanta-data` by default; set `RAILVANTA_DATA_VOLUME` to use another), and the backup/restore spec leaves a `*-restored-*` volume to remove by hand:

```sh
docker compose -f docker-compose.yml -f docker-compose.e2e.yml up --build --detach --wait
E2E_BASE_URL=http://localhost:3000 pnpm test:e2e
docker compose down -v
```

Details of the harness, its test-only control server and fixtures are in [e2e/README.md](e2e/README.md). The suite uses one worker and no retries because lifecycle specs restart shared processes. `pnpm test:e2e:quickstart` (sets `RUN_QUICKSTART=1`) is slow and needs network; it clones the repo into a temporary directory, runs the Quickstart block above as written, then the smoke (`api/scaffold.spec.ts`) and central-demo (`api/recovery.spec.ts`) specs, and deletes the clone.

CI (`.github/workflows/`) runs the same checks; no GitHub Actions run is claimed here. Measured results and their environments are recorded per change in [CODECHANGELOG.md](CODECHANGELOG.md), not in this README.

## Scope and safety

Sandbox only. Money is integer minor units (decimal strings in the API); idempotency boundaries are preserved; an ambiguous provider outcome is reconciled against the original provider and never automatically retried against another. The single-owner SQLite design offers no horizontal API scaling or database high availability. Commands here create no cloud resources. Every change is logged in [CODECHANGELOG.md](CODECHANGELOG.md) and tracked against [docs/wbs.md](docs/wbs.md).
