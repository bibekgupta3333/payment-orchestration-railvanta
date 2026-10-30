# Deployment

Deployment targets, configuration, startup, health and backup/restore for the sandbox. Cluster resources for the optional Kubernetes stage are in [kubernetes](kubernetes.md).

## Required Docker Compose runtime

The repository now includes [docker-compose.yml](../../docker-compose.yml), separate [API](../../infrastructure/docker/api.Dockerfile) and [frontend](../../infrastructure/docker/web.Dockerfile) image definitions, and an unprivileged Nginx proxy. This is the required local container workflow, introduced alongside the M1 scaffold; the full recovery/release gates remain in WBS 5.2.

Use `pnpm compose:config`, then `pnpm compose:up` after the dependency lockfiles are ready. The public origin is `http://localhost:3000` (overridable with `RAILVANTA_PORT`). Only the proxy binds host loopback; it routes `/v1` to `api:8080` and other paths to `web:3000`. API health remains at container loopback `127.0.0.1:8081`. The API alone mounts the named SQLite volume; `pnpm compose:down` retains it.

Both application images run non-root with read-only roots and explicit writable mounts. Images are built from source; neither build nor runtime is claimed verified yet. Compose configuration validation passes, but Docker socket access and unresolved M1 dependency files currently block full validation. Container demo mode is off; loopback-only fixture bootstrapping must not be enabled on the container's wildcard API bind.

The frontend enables Next.js standalone output with the monorepo tracing root and explicitly copies static/public assets, as required by [Next.js output documentation](https://nextjs.org/docs/app/api-reference/config/next-config-js/output). Its upstream address is a build argument because rewrites are recorded during build. Compose waits for image health checks through `service_healthy`; see [Docker startup ordering](https://docs.docker.com/compose/how-tos/startup-order/).

## Deployment stages

| Stage | Compute | Persistence | Purpose |
| --- | --- | --- | --- |
| Local development | Next.js development process and one Go API process | Local SQLite file | Native editing/debugging alternative; payment execution remains later work |
| Container sandbox | Separate frontend and API containers on one host | Durable local filesystem volume owned by API | Required local image/Compose runtime; recovery and restore verification pending |
| Optional Kubernetes sandbox | Frontend Deployment and one API StatefulSet pod | One persistent volume mounted by API | Learn scheduling, TLS, resource controls, and operational recovery |
| Future scale design | Stateless frontend and multiple API/worker processes | PostgreSQL after a separate migration project | Only after correctness, concurrency, and operational gates pass |

The API process owns database access and runs the durable background dispatcher. Do not deploy a separate worker against the SQLite volume. Kafka, Redis, and Temporal are not required for this MVP. Start with the deterministic mock provider; Stripe test mode and Plaid Sandbox are later integrations.

## Deployment diagram

```mermaid
flowchart TB
    Browser["Browser: sandbox user"]
    subgraph Host["Container host or optional Kubernetes cluster"]
        Edge["HTTPS reverse proxy / Ingress"]
        Frontend["Next.js frontend :3000"]
        API["One Go API process :8080<br/>HTTP handlers and background dispatcher"]
        Mock["Deterministic mock adapter<br/>inside Go process"]
        Disk[("Local filesystem on durable volume<br/>railvanta.db plus WAL / SHM")]
        Backup["Online backup operation<br/>initiated through API-owned tooling"]
    end
    Archive[("Encrypted backup storage<br/>separate failure domain")]
    Future["Future provider sandbox APIs"]
    Browser -->|HTTPS| Edge
    Edge -->|"/ and app assets"| Frontend
    Edge -->|"/v1/*; preserve path"| API
    Frontend -->|"Server-side requests when needed"| API
    API --> Mock
    API -->|"GORM / SQLite"| Disk
    API -.->|"Later: HTTPS"| Future
    API --> Backup
    Backup --> Archive
```

Use a single public origin. Preserve `/v1` when forwarding API requests; send other paths to Next.js. Server-side frontend requests use an internal API origin. Keep health endpoints private. A Kubernetes Ingress requires a compatible controller; the Ingress object alone is insufficient. [Kubernetes Ingress documentation](https://kubernetes.io/docs/concepts/services-networking/ingress/)

## Configuration contract

The M1 API config loader implements `APP_ENV`, `HTTP_ADDR`, `HEALTH_ADDR`, `SQLITE_PATH`, `PUBLIC_APP_ORIGIN`, and loopback-only `DEMO_MODE`. Next.js implements `API_INTERNAL_ORIGIN`. Container values and external provider credentials below remain deployment design; full runtime verification is pending.

| Key | Consumer | Intended value or rule |
| --- | --- | --- |
| `APP_ENV` | API | `sandbox`; reject live operation configuration |
| `HTTP_ADDR` | API | `:8080` by default; literal loopback required for demo mode |
| `HEALTH_ADDR` | API | `127.0.0.1:8081` by default; literal loopback only |
| `SQLITE_PATH` | API | `/var/lib/railvanta/railvanta.db` in containers |
| `API_INTERNAL_ORIGIN` | Next.js server | Internal API service URL; never a provider credential |
| `PUBLIC_APP_ORIGIN` | Server configuration | Deployed HTTPS origin for origin/redirect validation |
| Session and provider credentials | API only | Inject through secret management; no real-money credentials |

Public JavaScript uses relative `/v1` URLs. Never put provider secrets or session signing keys into `NEXT_PUBLIC_*` variables. Keep environment examples limited to placeholders. Database files, backup archives, and logs must not contain raw card or bank credentials.

## SQLite storage boundary

Mount the entire database directory, including its WAL and shared-memory files. Use a filesystem with reliable locking and durable writes, backed by local disk or a block device mounted on one node. Do not use NFS/RWX shared filesystem storage: SQLite WAL relies on same-host coordination. Plan capacity for the database, WAL growth, temporary files, and backups. [SQLite WAL documentation](https://www.sqlite.org/wal.html)

One API replica is an operational restriction, not a high-availability guarantee. A lost node or volume makes the API unavailable. A single-node local volume also constrains rescheduling; a reattachable block volume still depends on the storage provider's attachment and fencing behavior. Move to PostgreSQL before introducing independently scaled writers or separate workers.

## Startup and schema changes

1. Validate sandbox-only settings and secret availability before accepting traffic.
2. Mount the data directory with the API user/group able to create the database and sidecar files.
3. Confirm no previous API process or migration process can access the volume as an active owner.
4. Apply explicit, versioned migrations in the API startup sequence before starting HTTP readiness or the dispatcher. Initial database creation follows the same path.
5. Validate the schema version and required SQLite connection settings; see [persistence](../backend/persistence.md).
6. Start the dispatcher, HTTP handlers, and readiness reporting. A failed migration leaves the API unready.

For incompatible migrations, enter a maintenance window: disable writes, drain requests and dispatcher work, stop the owner, take a verified backup, migrate once, then start the compatible API image. Do not run a migration Job concurrently against the live SQLite owner. Image rollback is safe only when schema compatibility is proven; otherwise use a tested forward fix or controlled restore with explicit data-loss accounting.

## Health, release, and recovery checks

Proposed API probes are `/health/startup`, `/health/live`, and `/health/ready`. Startup becomes successful after initialization. Liveness checks process responsiveness without calling providers. Readiness checks initialization, compatible schema, database access, and whether the process is draining. A provider outage should surface as degraded service metrics rather than restart loops. Kubernetes distinguishes these probe roles and delays other probes while a startup probe is failing. [Probe documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)

On termination, first become unready and stop new dispatch claims, then finish or safely abandon bounded in-flight work, persist recoverable status, close the database, and exit. An interrupted provider call remains ambiguous until resolved through provider lookup or reconciliation; deployment must not turn that outcome into a cross-provider retry.

Before releasing a sandbox image, verify:

- Formatting, build, domain tests, API contract tests, and frontend tests pass in CI.
- A duplicate payment request creates one logical payment; workspace isolation holds.
- Restarting after dispatch recovers durable work without duplicate ledger posting.
- A restored backup passes SQLite integrity and application invariant checks.
- The frontend displays unavailable/error states when the API is offline and labels demo metrics.



## Backups and restoration

Use SQLite's online backup API through API-owned tooling to produce a consistent snapshot, then upload the completed archive to encrypted storage outside the volume's failure domain. Do not copy only an actively used `.db` file or let an independently scheduled pod open the live WAL database on another node. SQLite provides its backup API specifically for consistent online snapshots. [SQLite backup documentation](https://www.sqlite.org/backup.html)

Record snapshot time, schema version, checksum, and application release. Set backup frequency and retention from an agreed recovery point objective; no recovery target has been measured yet. Monitor backup age and available disk space.

To restore: stop and fence the old owner; preserve the current volume for investigation; restore into a clean directory; validate checksum, `PRAGMA integrity_check`, and `PRAGMA foreign_key_check`; run ledger and payment-state checks; start a matching release; then reconcile events occurring after the snapshot before resuming affected payments. Test this sequence on a separate volume. Never replace files underneath a live API process.
