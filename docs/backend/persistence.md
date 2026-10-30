# Persistence: SQLite and GORM

How the Go API uses SQLite through GORM. Entities and invariants are in the [data model](data-model.md); the reference DDL is [schema.sql](schema.sql).

## Persistence boundary

Use a Go modular monolith with GORM repositories. One API process owns one SQLite file and runs the outbox/inbox dispatcher in-process. Keep provider adapters behind domain interfaces. The frontend calls `/v1` through the same-origin reverse proxy and never opens the database.

SQLite WAL permits concurrent readers with a writer, but still serializes writes and requires same-host file access. Use a persistent local/block filesystem, not a network-shared database directory. In Kubernetes use one API StatefulSet replica and one PVC; see [deployment](../operations/deployment.md). This is a sandbox availability tradeoff. Horizontal API scaling requires a different database design. These constraints follow [SQLite's WAL documentation](https://sqlite.org/wal.html).

## Connection policy

The connection DSN sets these options on every opened connection; startup reads them back and refuses mismatches:

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = FULL;
```

Set and verify connection-local settings on every newly opened connection, including migrations and tooling. The store uses a single pooled connection (`SetMaxOpenConns(1)`, `SetMaxIdleConns(1)`), so application transactions serialize and connection initialization stays explicit. Do not issue repository operations through the root handle inside a transaction: with one connection this can deadlock. A later read pool requires per-connection initialization and file-backed concurrency tests. [SQLite documents PRAGMA scope and behavior](https://sqlite.org/pragma.html).

WAL does not eliminate `SQLITE_BUSY`. Bound retries with jitter and request deadlines; retry an entire short database transaction only when it has no external side effects. Never retry a provider call merely because a subsequent database commit failed. Measure busy errors, write latency, pending jobs, WAL size, and oldest unresolved payment.

## GORM mappings

Keep explicit IDs and integer timestamps. Avoid embedding `gorm.Model`: its default fields and soft-delete semantics do not match this reference schema. Use a separate API DTO with string money fields.

All tables have explicit flat GORM mappings in `backend/internal/storage/models.go`. For example:

```go
type Payment struct {
    ID              string `gorm:"primaryKey"`
    WorkspaceID     string
    PaymentMethodID string
    AmountMinor     int64
    Currency        string
    Status          string
    Version         int64
    CreatedAt       int64 `gorm:"autoCreateTime:milli"`
    UpdatedAt       int64 `gorm:"autoUpdateTime:milli"`
}

func (Payment) TableName() string { return "payments" }
```

Use explicit repository operations with workspace predicates and context deadlines. For transitions, update using `WHERE id = ? AND workspace_id = ? AND version = ? AND status = ?`, increment `version`, and require one affected row. Insert the audit transition and outbox event in the same transaction. Never permit generic client-driven field maps to update payment amount, currency, ownership, or status.

Use `db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { ... })` for a unit of work, pass `tx` to every repository in that unit, and return errors to roll back. GORM documents this behavior in its [transaction reference](https://gorm.io/docs/transactions.html). Default per-operation transactions do not make multiple independent repository calls atomic.

## Migrations

The binary embeds `0001_payment_domain.sql` and `0002_auth_and_simulation.sql`. `storage.Open` verifies the connection and runs `Migrate` before returning. The runner checks consecutive versions, names and SHA-256 checksums; rejects unmanaged, newer or incompatible databases; and installs pending migrations plus history in one transaction. Reopening an up-to-date database is a no-op. Tests exercise a version-1 fixture upgraded to version 2 without losing its rows. Run migrations under exclusive deployment control. Do not use startup `AutoMigrate` as the release migration strategy: preserve the explicit checks, composite foreign keys, partial indexes, and triggers in [schema.sql](schema.sql). GORM's [migration documentation](https://gorm.io/docs/migration.html) describes its schema automation; this project instead chooses explicit SQL for auditability.

Back up before schema changes; validate a copy first. A reference schema applied to a nonempty database should fail rather than silently skip incompatible tables. Rollback can require restoring a verified backup; do not promise reversible migrations after writes under a new schema.

## Transactions, claims and leases

The end-to-end acceptance and dispatch sequence is in [flows](flows.md). Persistence-specific rules:

- Hash the canonical request (amount, currency, method, operation) with SHA-256. The unique `(workspace_id, operation, key)` index decides concurrent requests: the loser reads the committed record, and if work is still pending, it returns a documented retryable response without creating another payment.
- Never hold a transaction open across provider I/O.
- Persist the result, the payment transition, the journal posting and the completion/event records in one transaction.

Claims use a short write transaction (or compare-and-swap update); do not rely on `SELECT FOR UPDATE`, which is not a SQLite row-locking primitive. Every completion checks the lease owner/token before acknowledging. Dispatch is at least once; each consumer and provider submission needs its own deduplication strategy. Webhook signatures are verified on raw request bytes before durable inbox insertion; duplicate event IDs with changed payload hashes require investigation.

Store a fresh unpredictable claim token in `lease_owner` for each acquisition, rather than reusing a pod or process name. Compare that token on renewals and completion so an expired owner cannot acknowledge a replacement's work.

Retain sandbox idempotency keys indefinitely in the MVP; deleting a key can make a late retry create a duplicate payment. Define an explicit retention and tombstone policy before introducing cleanup. Store sanitized responses and event payloads; avoid secrets or raw payment credentials.

## Validating the schema

Prerequisite: Python 3 with SQLite support. From the repository root, the following creates a temporary database, checks integrity, and removes it automatically. It does not install an application database.

```bash
python3 - <<'PY'
import pathlib, sqlite3, tempfile
with tempfile.TemporaryDirectory() as tmp:
    db = sqlite3.connect(str(pathlib.Path(tmp) / "railvanta.db"))
    db.execute("PRAGMA foreign_keys=ON")
    db.executescript(pathlib.Path("docs/backend/schema.sql").read_text())
    assert db.execute("PRAGMA foreign_key_check").fetchall() == []
    assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
    print("Reference schema integrity checks passed")
    db.close()
PY
```

If schema execution fails, check the first SQLite error and use a fresh temporary database; do not rerun against a partially modified valuable file. For lock timeouts, close competing writers and verify transaction duration. Foreign-key checks returning rows indicate invalid references that require investigation before proceeding.

## Verification and remaining implementation tests

Run `pnpm test:api` for file-backed storage tests, including fresh database integrity, invalid constraints, transaction rollback, upgrade/no-op migrations, checksum drift, incompatible history, UUIDv7 IDs, and safety-setting rejection. `pnpm test:e2e` exercises startup/restart through the API once the full harness is available. Record observed command results in the changelog.

Payment orchestration tests remain later work:

- Same-key concurrent requests create one payment; changed payload returns `409`; restart preserves replay.
- Cross-workspace payment methods, attempts, accounts, and journal links are rejected.
- Fractional, zero, negative, and out-of-int64 payment amounts are rejected; API round-trip preserves amounts beyond JavaScript's safe integer range.
- Unbalanced, mixed-currency, duplicate-key journals and changes to posted lines fail; reversal uses a new balanced journal.
- Crash before submit, after provider acceptance, and before acknowledgement produces one logical financial effect or explicit `unknown` reconciliation.
- Duplicate/reordered webhooks and expired dispatcher leases do not regress terminal states or duplicate ledger postings.
- Real file-backed connections exercise busy handling, optimistic update conflicts, backup, restore, and migration failure recovery.

Backup and restore procedures are in [deployment](../operations/deployment.md#backups-and-restoration).
