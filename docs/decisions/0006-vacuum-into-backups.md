# ADR 0006: Backups use VACUUM INTO on a read-only connection

Status: accepted (implemented in `backend/internal/backup`, WBS 5.2).

## Context

[Deployment](../operations/deployment.md) planned to use SQLite's online backup API through API-owned tooling. The Go stack uses `mattn/go-sqlite3` through GORM; the backup API is not exposed through `database/sql`, and driving it needs raw driver handles. The tooling also had to run against a database a live API is writing, and had to be usable from a container without copying the `-wal` and `-shm` files.

## Decision

`railvanta-api backup` opens the database with `mode=ro` and runs `VACUUM INTO '<tmp>'` in one read transaction. That yields a transactionally consistent, compacted copy while the API keeps writing. The command then verifies the copy (`integrity_check`, `foreign_key_check`), fsyncs, renames it into place and writes a JSON manifest (`snapshot_at`, `schema_version`, `sha256`, `size_bytes`, release). `restore` verifies the manifest checksum, refuses a newer schema or a non-empty target, copies into a clean directory and runs the full domain check; `check` adds `ledger.Verify` and structural checks.

## Consequences

- No driver internals and no copying of live database files; backups can run beside the API.
- `VACUUM INTO` rewrites the database, so cost scales with its size, and a snapshot taken mid-flight contains in-flight work. The drill recorded a snapshot with 600 payments but 207 journals; restart recovery completed it.
- The command must open the live database file, so it runs where the volume is mounted (inside the API container); it cannot back up a database over the network.
- No retention policy, schedule, encryption or off-volume upload exists; the scripts only copy to a host directory.
