CREATE TABLE users (
  id TEXT PRIMARY KEY NOT NULL,
  email TEXT NOT NULL COLLATE NOCASE UNIQUE,
  created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE workspace_members (
  user_id TEXT NOT NULL REFERENCES users(id),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  role TEXT NOT NULL CHECK (role IN ('read', 'operator')),
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, workspace_id)
) STRICT;
CREATE TABLE sessions (
  id TEXT PRIMARY KEY NOT NULL,
  user_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE CHECK (length(token_hash) = 64),
  csrf_hash TEXT NOT NULL CHECK (length(csrf_hash) = 64),
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (user_id, workspace_id) REFERENCES workspace_members(user_id, workspace_id)
) STRICT;
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE api_keys (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  name TEXT NOT NULL,
  prefix TEXT NOT NULL UNIQUE,
  key_hash TEXT NOT NULL CHECK (length(key_hash) = 64),
  role TEXT NOT NULL CHECK (role IN ('read', 'operator')),
  expires_at INTEGER,
  revoked_at INTEGER,
  created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE mock_provider_charges (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  provider_idempotency_key TEXT NOT NULL,
  provider_reference TEXT NOT NULL,
  amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
  currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
  status TEXT NOT NULL CHECK (status IN ('processing', 'succeeded', 'declined', 'unknown')),
  scenario TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (provider_id, provider_idempotency_key),
  UNIQUE (provider_id, provider_reference),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id)
) STRICT;
CREATE TABLE simulation_runs (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  payment_id TEXT,
  scenario TEXT NOT NULL,
  seed TEXT NOT NULL,
  input_json TEXT NOT NULL,
  result_json TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
  created_at INTEGER NOT NULL,
  completed_at INTEGER,
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id)
) STRICT;
