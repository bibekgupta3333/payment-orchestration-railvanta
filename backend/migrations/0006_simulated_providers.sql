-- Allow the three clearly-labelled simulated demo providers (mock_fast,
-- mock_economy, mock_ach) next to mock/stripe/plaid. SQLite cannot relax a CHECK
-- in place, so providers is rebuilt with identical columns and rows. Child rows
-- keep referencing (id, workspace_id); their foreign-key checks are deferred to
-- commit, by which time every parent row has been restored.
PRAGMA defer_foreign_keys = ON;

CREATE TEMP TABLE providers_old AS SELECT id, workspace_id, code, enabled, created_at FROM providers;

DROP TABLE providers;

CREATE TABLE providers (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  code TEXT NOT NULL CHECK (code IN ('mock', 'mock_fast', 'mock_economy', 'mock_ach', 'stripe', 'plaid')),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  created_at INTEGER NOT NULL,
  UNIQUE (workspace_id, code),
  UNIQUE (id, workspace_id)
) STRICT;

INSERT INTO providers(id, workspace_id, code, enabled, created_at) SELECT id, workspace_id, code, enabled, created_at FROM providers_old;

DROP TABLE providers_old;
