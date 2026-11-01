# Persistence: SQLite and GORM

How the Go API uses SQLite through GORM (implemented; this document describes the code in `backend/internal/storage`). Entities and invariants are in the [data model](data-model.md); the reference DDL is [schema.sql](schema.sql).

## Persistence boundary

The API is a Go modular monolith with GORM repositories. One API process owns one SQLite file and runs the outbox/inbox dispatcher in-process. Keep provider adapters behind domain interfaces. The frontend calls `/v1` through the same-origin reverse proxy (or the Next.js rewrite in development) and never opens the database.

SQLite WAL permits concurrent readers with a writer, but still serializes writes and requires same-host file access. Use a persistent local/block filesystem, not a network-shared database directory. The Kubernetes manifests use one API StatefulSet replica and one `ReadWriteOncePod` PVC; see [deployment](../operations/deployment.md) and [kubernetes](../operations/kubernetes.md). This is a sandbox availability tradeoff. Horizontal API scaling requires a different database design. These constraints follow [SQLite's WAL documentation](https://sqlite.org/wal.html).

## Connection policy

The connection DSN sets these options on every opened connection; startup reads them back and refuses mismatches:

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = FULL;
```

Set and verify connection-local settings on every newly opened connection, including migrations and tooling. The store uses a single pooled connection (`SetMaxOpenConns(1)`, `SetMaxIdleConns(1)`), so application transactions serialize and connection initialization stays explicit. Do not issue repository operations through the root handle inside a transaction: with one connection this can deadlock. A later read pool requires per-connection initialization and file-backed concurrency tests. [SQLite documents PRAGMA scope and behavior](https://sqlite.org/pragma.html).

WAL does not eliminate `SQLITE_BUSY`. Implemented policy: acceptance and outcome recording retry the whole short transaction up to four times with full-jitter exponential backoff (10–250 ms) and then answer `503` with `Retry-After`; a write failure such as disk full or a read-only database also answers `503` (`storage.IsWriteFailure`). A transaction is retried only when it has no external side effects, and a provider call is never repeated because a later commit failed. Not implemented: WAL-size and busy-error metrics (the in-process gauges cover unknown payments and the oldest unresolved payment; there is no exporter). `/health/ready` also runs `storage.CheckWritable`; a busy database stays ready.

## GORM mappings

Models use explicit IDs and integer timestamps and do not embed `gorm.Model`, whose default fields and soft-delete semantics do not match this schema. The HTTP layer uses separate DTOs with string money fields.

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

Repository operations carry workspace predicates and context deadlines. Transitions update using `WHERE id = ? AND workspace_id = ? AND version = ? AND status = ?`, increment `version`, and require one affected row. Insert the audit transition and outbox event in the same transaction. No generic client-driven field map updates payment amount, currency, ownership or status.

Use `db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { ... })` for a unit of work, pass `tx` to every repository in that unit, and return errors to roll back. GORM documents this behavior in its [transaction reference](https://gorm.io/docs/transactions.html). Default per-operation transactions do not make multiple independent repository calls atomic.

## Migrations

The binary embeds `0001_payment_domain.sql`, `0002_auth_and_simulation.sql`, `0003_simulation_runs.sql`, `0004_reconciliation.sql` and `0005_events_audit.sql`; later files only add ([ADR 0002](../decisions/0002-forward-only-additive-migrations.md)). The runner creates `schema_migrations` itself. `storage.Open` verifies the connection and runs `Migrate` before returning. The runner checks consecutive versions, names and SHA-256 checksums; rejects unmanaged, newer or incompatible databases; and installs pending migrations plus history in one transaction. Reopening an up-to-date database is a no-op. Tests exercise a version-1 fixture upgraded without losing its rows, checksum drift, incompatible history, and a failing migration that leaves the API refusing to start. Migrations run under exclusive deployment control: only the API process applies them. Do not use startup `AutoMigrate` as the release migration strategy: preserve the explicit checks, composite foreign keys, partial indexes, and triggers in [schema.sql](schema.sql). GORM's [migration documentation](https://gorm.io/docs/migration.html) describes its schema automation; this project instead chooses explicit SQL for auditability.

Back up before schema changes; validate a copy first. A reference schema applied to a nonempty database should fail rather than silently skip incompatible tables. Rollback can require restoring a verified backup; do not promise reversible migrations after writes under a new schema.

## Transactions, claims and leases

The end-to-end acceptance and dispatch sequence is in [flows](flows.md). Persistence-specific rules:

- Hash the canonical request (amount, currency, method, operation) with SHA-256. The unique `(workspace_id, operation, key)` index decides concurrent requests: the loser reads the committed record, and if work is still pending, it returns a documented retryable response without creating another payment.
- Never hold a transaction open across provider I/O.
- Persist the result, the payment transition, the journal posting and the completion/event records in one transaction.

Claims use a compare-and-swap `UPDATE` that matches the status and lease owner it read; `SELECT FOR UPDATE` is not a SQLite row-locking primitive and is not used. Every renew, completion, release and failure checks the lease token and returns `event.ErrLeaseLost` otherwise. Dispatch is at least once; each consumer and provider submission has its own deduplication (outbox deduplication keys, the stable provider key, the unique posting key). Webhook inbox deduplication is implemented; webhook **signature verification is not** (mock events are unsigned, see [ADR 0005](../decisions/0005-sandbox-only-mock-webhook.md)). Duplicate event IDs with changed payload hashes are flagged for investigation.

Each acquisition stores a fresh 32-byte `crypto/rand` token in `lease_owner`, never a pod or process name, and renewals (every lease/3) and completion compare it, so an expired owner cannot acknowledge a replacement's work. A row is parked as `failed` after 10 handler-error acquisitions, except `payment.lookup`, which retries without a limit ([ADR 0009](../decisions/0009-unknown-outcomes-resolved-only-by-evidence.md)).

Sandbox idempotency keys are retained indefinitely and no cleanup exists; deleting a key can make a late retry create a duplicate payment, so define a retention and tombstone policy before adding cleanup. Store sanitized responses and event payloads; avoid secrets or raw payment credentials.

## Validating the schema

Prerequisite: Python 3 with SQLite support. From the repository root, the following builds one temporary in-memory database from the migrations and one from the reference schema, checks integrity, and diffs table, index and trigger names and every table's columns. It installs no application database. Run it whenever a migration is added.

```bash
python3 - <<'PY'
import glob, sqlite3, pathlib

def objects(db):
    return {(r[0], r[1]) for r in db.execute(
        "select type, name from sqlite_master where name not like 'sqlite_%'")}

migrated = sqlite3.connect(":memory:")
for path in sorted(glob.glob("backend/migrations/0*.sql")):
    migrated.executescript(pathlib.Path(path).read_text())
# schema_migrations is created by the Go runner (storage/migrate.go), not a migration.
migrated.executescript("CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY NOT NULL)")

reference = sqlite3.connect(":memory:")
reference.execute("PRAGMA foreign_keys=ON")
reference.executescript(pathlib.Path("docs/backend/schema.sql").read_text())
assert reference.execute("PRAGMA foreign_key_check").fetchall() == []
assert reference.execute("PRAGMA integrity_check").fetchone()[0] == "ok"

a, b = objects(migrated), objects(reference)
assert a == b, (sorted(a - b), sorted(b - a))
for _, table in sorted(x for x in a if x[0] == "table"):
    if table == "schema_migrations":
        continue
    cols = lambda db: [tuple(r[1:4]) + (r[5],) for r in db.execute(f"pragma table_info({table})")]
    assert cols(migrated) == cols(reference), table
print("Reference schema matches migrations:", len(a), "objects")
PY
```

If schema execution fails, check the first SQLite error and use a fresh database. A foreign-key check that returns rows indicates invalid references. To validate the real migration runner instead, run `pnpm test:api`; its file-backed storage tests apply the embedded migrations through `storage.Open`.

## Verification

Run `pnpm test:api` for the file-backed storage and cross-package tests, including fresh database integrity, invalid constraints, transaction rollback, upgrade and no-op migrations, checksum drift, incompatible history, UUIDv7 IDs, safety-setting rejection, disk-full and busy handling, same-key concurrency, crash points, duplicate and reordered webhooks, cross-workspace isolation, and balanced or duplicate postings. `pnpm test:e2e` exercises the same behavior through HTTP and the browser. The [verification matrix](../verification-matrix.md) maps each row to a named test; recorded command results are in the [code changelog](../../CODECHANGELOG.md).

Remaining gaps: the pool is a single connection, so a read pool and its per-connection initialization are untested; busy-error and WAL-size metrics do not exist; an OS-process restart is covered by the container E2E, not the Go tests; CI has not run on GitHub.

Backup and restore procedures are in [deployment](../operations/deployment.md#backups-and-restoration).
