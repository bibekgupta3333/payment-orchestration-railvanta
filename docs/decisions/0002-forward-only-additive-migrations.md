# ADR 0002: Forward-only additive migrations after 0001

Status: accepted (implemented in migrations 0002–0005).

## Context

The design baseline described one reference schema (`docs/backend/schema.sql`) of 15 tables, then a revised 23-table M1 schema. [Persistence](../backend/persistence.md) planned a checksummed runner and warned against `AutoMigrate`, but it did not say how later milestones would change a database that already holds data. M1 rewrote 0001 in place before any release; M2–M4 could not.

## Decision

Treat `0001_payment_domain.sql` as frozen. Every later change is a new numbered file that only adds: `ALTER TABLE ... ADD COLUMN` with a default or NULL, new tables, indexes and triggers.

- `0002` users, workspace members, sessions, API keys, `mock_provider_charges`, `simulation_runs`.
- `0003` simulation fault, invariant, fingerprint and failure columns (WBS 3.2.6).
- `0004` reconciliation counts, `reconciliation_resolution_events` and discrepancy/run integrity triggers (WBS 4.2).
- `0005` webhook inbox outcome, signature, investigation columns and triggers, plus listing indexes (WBS 4.3).

`schema_migrations` is created by the Go runner (`storage/migrate.go`), not by a migration file. `docs/backend/schema.sql` is the flattened result for reading and for ad-hoc validation; it must define exactly the same tables, indexes and triggers as migrations 0001–0005 plus `schema_migrations`.

## Consequences

- An existing database upgrades without data loss; a version-1 fixture upgrade is tested.
- A constraint added by `ALTER ... ADD COLUMN` cannot be rewritten later; column semantics in 0001 stay. SQLite cannot add a CHECK to an existing column, so tightening needs a table rebuild in a future migration.
- The reference schema drifted after 0002: `schema.sql` lacked 0003 and 0004 until WBS 5.4 compared the two by building both in SQLite and diffing `sqlite_master`. Run that comparison whenever a migration is added.
- Rollback is a restore from a verified backup, not a down-migration.
