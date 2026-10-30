# Railvanta Documentation

Design documentation for the Railvanta sandbox. Start with the [project brief](../idea.md).

**Status:** M1 source is added: Go startup/storage/auth, Next.js shell, tests, and CI configuration. Dependency downloads are blocked in the authoring environment, so full compilation, generated contracts, browser checks, and green CI remain unverified. Container image/Compose definitions are now added, with runtime validation pending. Payment execution and clusters are not implemented. Acceptance criteria remain open until verified and merged. See [setup](../README.md) and [actual results](../CODECHANGELOG.md).

## Layout

| Document | Contents |
| --- | --- |
| [Architecture](architecture.md) | Stack, repository layout, module boundaries, consistency and trust rules |
| [Roadmap](roadmap.md) | Deliverable hierarchy, milestones, deferred scope |
| [WBS checklist](wbs.md) | Tasks, acceptance gates, per-package E2E tests and status; verification matrix |
| [Code changelog](../CODECHANGELOG.md) | Every change with its WBS items and actual verification results |
| [ADR 0001](decisions/0001-sandbox-stack.md) | Why Go + GORM + SQLite + Next.js |
| **Backend** | |
| [Flows](backend/flows.md) | System context, payment sequence, idempotency, payment/attempt state machines, recovery |
| [Data model](backend/data-model.md) | Entity diagram, table responsibilities, financial invariants |
| [Persistence](backend/persistence.md) | SQLite connection policy, GORM mappings, migrations, transactions, leases |
| [schema.sql](backend/schema.sql) | Executable reference schema (23 STRICT tables) |
| **Frontend** | |
| [Design](frontend/design.md) | Next.js structure, API boundary, money handling, visual and accessibility rules |
| [Wireframes](frontend/wireframes.md) | Desktop/mobile wireframes per route and shared UI states |
| **Operations** | |
| [Deployment](operations/deployment.md) | Deployment stages, configuration, startup, health, backup and restore |
| [Kubernetes](operations/kubernetes.md) | Optional cluster resources, storage, security and lifecycle |

Diagrams use Mermaid. Wireframes are plain text.

## Reference schema

`backend/schema.sql` mirrors the numbered migrations in the root `backend/migrations/`. The API runs those embedded migrations with checksummed history; the reference DDL alone is not a bootstrapped runtime database. To check it, build a throwaway database (see [Persistence → Validating the schema](backend/persistence.md#validating-the-schema)).

Verified so far (SQLite 3.51.0 via Python):

- The revised M1 schema builds 23 STRICT tables. `PRAGMA integrity_check` returns `ok`, and `PRAGMA foreign_key_check` returns no rows.
- Independent Python schema checks passed; full Go/E2E execution remains blocked. The earlier 15-table baseline also passed 21 focused checks covering:
  - exact int64 storage
  - rejection of invalid money, status and currency values
  - workspace isolation
  - unique idempotency keys and at most one unresolved attempt per payment
  - balanced posting, with duplicate posting keys rejected
  - immutable posted journals and entries
  - aggregation overflow rejection

These checks cover only the schema. They prove nothing about application behavior.

## Writing rules for these docs

- Keep status honest: mark planned work as planned, and record only real test results.
- Each topic has a single home; link to it rather than repeating it. State machines live in [flows](backend/flows.md), and backup/restore lives in [deployment](operations/deployment.md).
- Follow the financial-correctness rules in [AGENTS.md](../AGENTS.md).
