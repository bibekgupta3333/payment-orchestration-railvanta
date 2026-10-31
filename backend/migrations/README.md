# Versioned SQLite migrations

The API embeds these numbered SQL files and applies them in order before readiness. `internal/storage` owns the transaction, connection PRAGMAs, and `schema_migrations` version/name/checksum history. Do not add transaction statements or PRAGMAs to these files, edit an already applied migration, or replace them with GORM AutoMigrate.

Migration `0001` defines payment-domain storage; `0002` adds authentication and future mock/simulation storage. The combined reference is `docs/backend/schema.sql`. M1 creates the schema but does not implement payment execution or simulation services.
