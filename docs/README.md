# Railvanta Documentation

Design and reference documentation for the Railvanta sandbox. Start with the [project brief](../idea.md). The backend, frontend and architecture documents describe the implemented system; anything not built is marked "not implemented".

**Status:** M1–M6 are implemented and verified locally; each gate's actual result, and what has not been run (for example GitHub Actions), is recorded in the [code changelog](../CODECHANGELOG.md) and the [verification matrix](verification-matrix.md). Unticked boxes in the [WBS](wbs.md) name the gates still open. See [setup](../README.md).

## Layout

| Document | Contents |
| --- | --- |
| [Architecture](architecture.md) | Stack, repository layout, module boundaries, consistency and trust rules |
| [Roadmap](roadmap.md) | Deliverable hierarchy, milestones, deferred scope |
| [WBS checklist](wbs.md) | Tasks, acceptance gates, per-package E2E tests and status; verification matrix |
| [Code changelog](../CODECHANGELOG.md) | Every change with its WBS items and actual verification results |
| [Verification matrix](verification-matrix.md) | Each verification row mapped to a named test or recorded result |
| [ADRs](#architecture-decisions) | Architecture decisions, including those that diverged from the design |
| **Backend** | |
| [Flows](backend/flows.md) | System context, payment sequence, idempotency, payment/attempt state machines, recovery |
| [Data model](backend/data-model.md) | Entity diagram, table responsibilities, financial invariants |
| [Persistence](backend/persistence.md) | SQLite connection policy, GORM mappings, migrations, transactions, leases |
| [schema.sql](backend/schema.sql) | Executable reference schema (24 STRICT tables: 23 from migrations 0001-0005 plus the runner's `schema_migrations`) |
| **Frontend** | |
| [Design](frontend/design.md) | Next.js structure, API boundary, money handling, visual and accessibility rules |
| [Wireframes](frontend/wireframes.md) | Desktop/mobile wireframes per route and shared UI states |
| **Operations** | |
| [Deployment](operations/deployment.md) | Deployment stages, configuration, startup, health, backup and restore |
| [Kubernetes](operations/kubernetes.md) | Optional cluster resources, storage, security and lifecycle |
| **Release evidence (WBS 5.4)** | |
| [Measurements](measurements.md) | Recorded latency, recovery outcomes and test counts, with date, commit and environment |
| [Known limitations](known-limitations.md) | Single writer, no HA, mock only, deferred capabilities, unverified items |
| [Case study](case-study.md) | Design choices, lessons, and demo scenarios mapped to passing specs |

Diagrams use Mermaid. Wireframes are plain text.

## Architecture decisions

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](decisions/0001-sandbox-stack.md) | Go + GORM + SQLite + Next.js sandbox | Accepted, implemented |
| [0002](decisions/0002-forward-only-additive-migrations.md) | Forward-only additive migrations after 0001; `schema.sql` is the flattened result | Accepted |
| [0003](decisions/0003-mock-scenario-by-token-prefix.md) | Mock provider scenario chosen by token reference prefix | Accepted |
| [0004](decisions/0004-late-outcomes-as-outbox-rows.md) | Late provider outcomes recorded as handler-less outbox rows | Accepted |
| [0005](decisions/0005-sandbox-only-mock-webhook.md) | Operator-authenticated, unsigned sandbox webhook endpoint | Accepted |
| [0006](decisions/0006-vacuum-into-backups.md) | Backups use `VACUUM INTO` on a read-only connection | Accepted |
| [0007](decisions/0007-private-api-owned-health.md) | API-owned health on a private loopback listener | Accepted |
| [0008](decisions/0008-evidence-only-reconciliation.md) | Reconciliation is synchronous, evidence-only and reads the mock's records | Accepted |
| [0009](decisions/0009-unknown-outcomes-resolved-only-by-evidence.md) | Unknown outcomes resolve only on provider evidence, with unbounded lookup | Accepted |
| [0010](decisions/0010-original-frontend-components.md) | Original Tailwind components, no icon library, read-only shell | Accepted |
| [0011](decisions/0011-sandbox-api-key-generator.md) | Sandbox API key generator in Settings | Accepted |
| [0012](decisions/0012-dashboard-hub-and-swagger-reference.md) | Dashboard hub and embedded Swagger API reference | Accepted |

Each ADR has Context, Decision, Consequences and Status. ADR 0001 predates this template and adds an Alternatives section.

## Reference schema

`schema.sql` mirrors the numbered migrations in `backend/migrations/` (0001-0005). The API runs those embedded migrations with checksummed history; the reference DDL alone is not a bootstrapped runtime database. To check it, run the comparison in [Persistence -> Validating the schema](backend/persistence.md#validating-the-schema): it builds one database from the migrations and one from `schema.sql`, runs `integrity_check` and `foreign_key_check`, and diffs every table, index, trigger and column.

Last comparison (WBS 5.4.3, SQLite 3.51.0 via Python): `schema.sql` lacked migrations 0003 and 0004 (one table, five indexes and nine triggers, plus columns on `simulation_runs` and `reconciliation_runs`). That drift was fixed; the comparison now reports identical objects and columns. These checks cover only the schema. They prove nothing about application behavior.

## Writing rules for these docs

- Keep status honest: mark planned work as planned, and record only real test results.
- Each topic has a single home; link to it rather than repeating it. State machines live in [flows](backend/flows.md), and backup/restore lives in [deployment](operations/deployment.md).
- Follow the financial-correctness rules in [AGENTS.md](../AGENTS.md).
