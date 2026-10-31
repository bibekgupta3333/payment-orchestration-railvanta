CREATE TABLE workspaces (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  environment TEXT NOT NULL DEFAULT 'sandbox' CHECK (environment = 'sandbox'),
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE providers (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  code TEXT NOT NULL CHECK (code IN ('mock', 'stripe', 'plaid')),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  created_at INTEGER NOT NULL,
  UNIQUE (workspace_id, code),
  UNIQUE (id, workspace_id)
) STRICT;

CREATE TABLE payment_methods (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  brand TEXT,
  last_four TEXT CHECK (last_four IS NULL OR (length(last_four) = 4 AND last_four NOT GLOB '*[^0-9]*')),
  rail TEXT NOT NULL CHECK (rail IN ('card', 'ach', 'mock')),
  created_at INTEGER NOT NULL,
  UNIQUE (id, workspace_id)
) STRICT;

CREATE TABLE payment_method_tokens (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  method_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  token_reference TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (method_id, provider_id),
  FOREIGN KEY (method_id, workspace_id) REFERENCES payment_methods(id, workspace_id),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id)
) STRICT;

CREATE TABLE routing_rules (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  provider_id TEXT NOT NULL,
  rail TEXT NOT NULL CHECK (rail IN ('card', 'ach', 'mock')),
  currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
  priority INTEGER NOT NULL CHECK (priority >= 0),
  min_amount_minor INTEGER NOT NULL DEFAULT 1 CHECK (min_amount_minor > 0),
  max_amount_minor INTEGER CHECK (max_amount_minor IS NULL OR max_amount_minor >= min_amount_minor),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (workspace_id, rail, currency, priority),
  UNIQUE (id, workspace_id),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id)
) STRICT;

CREATE TABLE payments (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  payment_method_id TEXT NOT NULL,
  amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
  currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
  merchant_reference TEXT,
  status TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'processing', 'succeeded', 'failed', 'canceled', 'unknown')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (id, workspace_id),
  UNIQUE (id, workspace_id, currency),
  FOREIGN KEY (payment_method_id, workspace_id) REFERENCES payment_methods(id, workspace_id)
) STRICT;
CREATE INDEX payments_merchant_reference ON payments(workspace_id, merchant_reference);
CREATE INDEX payments_list ON payments(workspace_id, created_at DESC, id);
CREATE INDEX payments_status ON payments(workspace_id, status, created_at DESC);

CREATE TABLE payment_attempts (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  payment_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  routing_rule_id TEXT,
  routing_reason TEXT NOT NULL,
  attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
  provider_idempotency_key TEXT NOT NULL,
  provider_reference TEXT,
  status TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'submitted', 'succeeded', 'failed', 'unknown')),
  error_code TEXT,
  submitted_at INTEGER,
  completed_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (payment_id, attempt_number),
  UNIQUE (provider_id, provider_idempotency_key),
  UNIQUE (provider_id, provider_reference),
  UNIQUE (id, workspace_id),
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id),
  FOREIGN KEY (routing_rule_id, workspace_id) REFERENCES routing_rules(id, workspace_id),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id)
) STRICT;
CREATE UNIQUE INDEX one_unresolved_attempt ON payment_attempts(payment_id)
  WHERE status IN ('created', 'submitted', 'unknown');

CREATE TABLE payment_state_transitions (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  payment_id TEXT NOT NULL,
  from_status TEXT CHECK (from_status IS NULL OR from_status IN ('created', 'processing', 'succeeded', 'failed', 'canceled', 'unknown')),
  to_status TEXT NOT NULL CHECK (to_status IN ('created', 'processing', 'succeeded', 'failed', 'canceled', 'unknown')),
  reason TEXT NOT NULL,
  actor_reference TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  CHECK (from_status IS NOT NULL OR to_status = 'created'),
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id)
) STRICT;
CREATE INDEX transitions_timeline ON payment_state_transitions(payment_id, created_at, id);

CREATE TABLE idempotency_records (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  operation TEXT NOT NULL,
  key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 255),
  request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
  payment_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('pending', 'completed')),
  response_status INTEGER CHECK (response_status BETWEEN 200 AND 599),
  response_body TEXT,
  created_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE (workspace_id, operation, key),
  CHECK ((status = 'pending' AND response_status IS NULL AND response_body IS NULL AND completed_at IS NULL)
      OR (status = 'completed' AND response_status IS NOT NULL AND response_body IS NOT NULL AND completed_at IS NOT NULL)),
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id)
) STRICT;

CREATE TABLE ledger_accounts (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  code TEXT NOT NULL,
  currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
  provider_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('asset', 'liability', 'income', 'expense', 'equity')),
  created_at INTEGER NOT NULL,
  UNIQUE (workspace_id, code, currency),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id),
  UNIQUE (id, workspace_id, currency)
) STRICT;

CREATE TABLE ledger_journals (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  payment_id TEXT,
  currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
  posting_key TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'posted')),
  reversal_of TEXT,
  created_at INTEGER NOT NULL,
  posted_at INTEGER,
  CHECK ((status = 'draft' AND posted_at IS NULL) OR (status = 'posted' AND posted_at IS NOT NULL)),
  UNIQUE (workspace_id, posting_key),
  UNIQUE (reversal_of),
  UNIQUE (id, workspace_id, currency),
  FOREIGN KEY (payment_id, workspace_id, currency) REFERENCES payments(id, workspace_id, currency),
  FOREIGN KEY (reversal_of, workspace_id, currency) REFERENCES ledger_journals(id, workspace_id, currency)
) STRICT;

CREATE TABLE ledger_entries (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  journal_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  currency TEXT NOT NULL,
  side TEXT NOT NULL CHECK (side IN ('debit', 'credit')),
  amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
  created_at INTEGER NOT NULL,
  FOREIGN KEY (journal_id, workspace_id, currency) REFERENCES ledger_journals(id, workspace_id, currency),
  FOREIGN KEY (account_id, workspace_id, currency) REFERENCES ledger_accounts(id, workspace_id, currency)
) STRICT;
CREATE INDEX ledger_account_history ON ledger_entries(account_id, created_at, id);
CREATE INDEX ledger_journal_entries ON ledger_entries(journal_id);

-- Draft journals and entries are created and posted in one application transaction.
CREATE TRIGGER journals_require_draft BEFORE INSERT ON ledger_journals
WHEN NEW.status <> 'draft'
BEGIN SELECT RAISE(ABORT, 'insert journal as draft'); END;

CREATE TRIGGER journals_validate_posting BEFORE UPDATE OF status ON ledger_journals
WHEN NEW.status = 'posted' AND OLD.status = 'draft'
BEGIN
  SELECT CASE WHEN (SELECT count(*) FROM ledger_entries WHERE journal_id = OLD.id) < 2
    THEN RAISE(ABORT, 'journal requires at least two entries') END;
  SELECT CASE WHEN
    (SELECT coalesce(sum(amount_minor), 0) FROM ledger_entries WHERE journal_id = OLD.id AND side = 'debit') <>
    (SELECT coalesce(sum(amount_minor), 0) FROM ledger_entries WHERE journal_id = OLD.id AND side = 'credit')
    THEN RAISE(ABORT, 'journal is not balanced') END;
END;

CREATE TRIGGER journals_no_posted_update BEFORE UPDATE ON ledger_journals
WHEN OLD.status = 'posted'
BEGIN SELECT RAISE(ABORT, 'posted journal is immutable'); END;
CREATE TRIGGER journals_no_posted_delete BEFORE DELETE ON ledger_journals
WHEN OLD.status = 'posted'
BEGIN SELECT RAISE(ABORT, 'posted journal is immutable'); END;
CREATE TRIGGER entries_no_posted_insert BEFORE INSERT ON ledger_entries
WHEN (SELECT status FROM ledger_journals WHERE id = NEW.journal_id) = 'posted'
BEGIN SELECT RAISE(ABORT, 'posted entries are immutable'); END;
CREATE TRIGGER entries_no_posted_update BEFORE UPDATE ON ledger_entries
WHEN (SELECT status FROM ledger_journals WHERE id = OLD.journal_id) = 'posted'
  OR (SELECT status FROM ledger_journals WHERE id = NEW.journal_id) = 'posted'
BEGIN SELECT RAISE(ABORT, 'posted entries are immutable'); END;
CREATE TRIGGER entries_no_posted_delete BEFORE DELETE ON ledger_entries
WHEN (SELECT status FROM ledger_journals WHERE id = OLD.journal_id) = 'posted'
BEGIN SELECT RAISE(ABORT, 'posted entries are immutable'); END;

CREATE TABLE webhook_inbox (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  provider_event_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL CHECK (length(payload_hash) = 64),
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'processed', 'failed')),
  lease_owner TEXT,
  lease_until INTEGER,
  duplicate_count INTEGER NOT NULL DEFAULT 0 CHECK (duplicate_count >= 0),
  last_received_at INTEGER NOT NULL,
  received_at INTEGER NOT NULL,
  processed_at INTEGER,
  UNIQUE (provider_id, provider_event_id),
  FOREIGN KEY (provider_id, workspace_id) REFERENCES providers(id, workspace_id)
) STRICT;

CREATE TABLE outbox_events (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  payment_id TEXT,
  event_type TEXT NOT NULL,
  deduplication_key TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'delivered', 'failed')),
  attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error TEXT,
  available_at INTEGER NOT NULL,
  lease_owner TEXT,
  lease_until INTEGER,
  created_at INTEGER NOT NULL,
  delivered_at INTEGER,
  CHECK ((status = 'processing' AND lease_owner IS NOT NULL AND length(lease_owner) > 0 AND lease_until IS NOT NULL AND lease_until >= 0)
    OR (status <> 'processing' AND lease_owner IS NULL AND lease_until IS NULL)),
  UNIQUE (workspace_id, deduplication_key),
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id)
) STRICT;
CREATE INDEX outbox_ready ON outbox_events(status, available_at, lease_until);

CREATE TABLE reconciliation_runs (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  run_key TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed')),
  window_start INTEGER NOT NULL,
  window_end INTEGER NOT NULL CHECK (window_end > window_start),
  created_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE (workspace_id, run_key),
  UNIQUE (id, workspace_id)
) STRICT;

CREATE TABLE reconciliation_discrepancies (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  payment_id TEXT,
  fingerprint TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('missing_posting', 'unknown_outcome', 'amount_mismatch', 'status_mismatch', 'missing_at_provider')),
  details TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'resolved')),
  resolution_note TEXT,
  resolved_by TEXT,
  created_at INTEGER NOT NULL,
  resolved_at INTEGER,
  UNIQUE (run_id, fingerprint),
  CHECK (status <> 'resolved' OR (resolution_note IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL)),
  FOREIGN KEY (run_id, workspace_id) REFERENCES reconciliation_runs(id, workspace_id),
  FOREIGN KEY (payment_id, workspace_id) REFERENCES payments(id, workspace_id)
) STRICT;


CREATE TRIGGER payments_immutable_fields BEFORE UPDATE ON payments
WHEN NEW.amount_minor IS NOT OLD.amount_minor OR NEW.currency IS NOT OLD.currency
  OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.payment_method_id IS NOT OLD.payment_method_id
BEGIN SELECT RAISE(ABORT, 'payment financial fields are immutable'); END;
CREATE TRIGGER payments_terminal_status BEFORE UPDATE OF status ON payments
WHEN OLD.status IN ('succeeded', 'failed', 'canceled') AND NEW.status IS NOT OLD.status
BEGIN SELECT RAISE(ABORT, 'terminal payment cannot change status'); END;
CREATE TRIGGER transitions_no_update BEFORE UPDATE ON payment_state_transitions
BEGIN SELECT RAISE(ABORT, 'payment audit is append-only'); END;
CREATE TRIGGER transitions_no_delete BEFORE DELETE ON payment_state_transitions
BEGIN SELECT RAISE(ABORT, 'payment audit is append-only'); END;
