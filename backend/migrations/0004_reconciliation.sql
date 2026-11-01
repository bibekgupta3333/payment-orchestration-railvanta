-- WBS 4.2: reconciliation counts, append-only resolution history and
-- discrepancy integrity. The kind CHECK, the (run_id, fingerprint) and
-- (workspace_id, run_key) uniqueness and the resolved-fields CHECK already
-- exist in 0001 and are relied on, not repeated.

-- Count columns written when a run completes. matched = compared payments with
-- no discrepancy; unmatched = missing_posting + unknown_outcome +
-- missing_at_provider; mismatch = amount_mismatch + status_mismatch.
ALTER TABLE reconciliation_runs ADD COLUMN compared_count INTEGER NOT NULL DEFAULT 0 CHECK (compared_count >= 0);
ALTER TABLE reconciliation_runs ADD COLUMN matched_count INTEGER NOT NULL DEFAULT 0 CHECK (matched_count >= 0);
ALTER TABLE reconciliation_runs ADD COLUMN unmatched_count INTEGER NOT NULL DEFAULT 0 CHECK (unmatched_count >= 0);
ALTER TABLE reconciliation_runs ADD COLUMN mismatch_count INTEGER NOT NULL DEFAULT 0 CHECK (mismatch_count >= 0);
ALTER TABLE reconciliation_runs ADD COLUMN evidence_consumed_count INTEGER NOT NULL DEFAULT 0 CHECK (evidence_consumed_count >= 0);
ALTER TABLE reconciliation_runs ADD COLUMN requested_by TEXT;

-- Parent key for the history foreign key below.
CREATE UNIQUE INDEX reconciliation_discrepancy_scope ON reconciliation_discrepancies(id, workspace_id);
CREATE INDEX reconciliation_discrepancy_list ON reconciliation_discrepancies(workspace_id, created_at DESC, id);
CREATE INDEX reconciliation_run_list ON reconciliation_runs(workspace_id, created_at DESC, id);

-- One append-only row per status change, each with a note and an actor.
CREATE TABLE reconciliation_resolution_events (
  id TEXT PRIMARY KEY NOT NULL,
  workspace_id TEXT NOT NULL,
  discrepancy_id TEXT NOT NULL,
  from_status TEXT NOT NULL CHECK (from_status IN ('open', 'investigating', 'resolved')),
  to_status TEXT NOT NULL CHECK (to_status IN ('open', 'investigating', 'resolved')),
  note TEXT NOT NULL CHECK (length(trim(note)) > 0),
  actor_reference TEXT NOT NULL CHECK (length(trim(actor_reference)) > 0),
  created_at INTEGER NOT NULL,
  CHECK (from_status <> to_status),
  FOREIGN KEY (discrepancy_id, workspace_id) REFERENCES reconciliation_discrepancies(id, workspace_id)
) STRICT;
CREATE INDEX reconciliation_resolution_timeline ON reconciliation_resolution_events(discrepancy_id, created_at, id);

CREATE TRIGGER reconciliation_events_no_update BEFORE UPDATE ON reconciliation_resolution_events
BEGIN SELECT RAISE(ABORT, 'resolution history is append-only'); END;
CREATE TRIGGER reconciliation_events_no_delete BEFORE DELETE ON reconciliation_resolution_events
BEGIN SELECT RAISE(ABORT, 'resolution history is append-only'); END;

-- Evidence is immutable: only the resolution workflow columns may change.
CREATE TRIGGER reconciliation_discrepancy_evidence_immutable BEFORE UPDATE ON reconciliation_discrepancies
WHEN NEW.id IS NOT OLD.id OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.run_id IS NOT OLD.run_id
  OR NEW.payment_id IS NOT OLD.payment_id OR NEW.fingerprint IS NOT OLD.fingerprint
  OR NEW.kind IS NOT OLD.kind OR NEW.details IS NOT OLD.details OR NEW.created_at IS NOT OLD.created_at
BEGIN SELECT RAISE(ABORT, 'discrepancy evidence is immutable'); END;
CREATE TRIGGER reconciliation_discrepancy_no_delete BEFORE DELETE ON reconciliation_discrepancies
BEGIN SELECT RAISE(ABORT, 'discrepancies are never deleted'); END;
CREATE TRIGGER reconciliation_discrepancy_resolved_final BEFORE UPDATE ON reconciliation_discrepancies
WHEN OLD.status = 'resolved'
BEGIN SELECT RAISE(ABORT, 'resolved discrepancy cannot change'); END;
-- Resolution needs a non-blank note and resolver and a timestamp; the 0001
-- CHECK only rejects NULL.
CREATE TRIGGER reconciliation_discrepancy_resolution_complete BEFORE UPDATE OF status ON reconciliation_discrepancies
WHEN NEW.status = 'resolved' AND (NEW.resolution_note IS NULL OR length(trim(NEW.resolution_note)) = 0
  OR NEW.resolved_by IS NULL OR length(trim(NEW.resolved_by)) = 0 OR NEW.resolved_at IS NULL)
BEGIN SELECT RAISE(ABORT, 'resolution requires note, resolver and timestamp'); END;
-- Discrepancies are recorded open; every status change then writes history.
CREATE TRIGGER reconciliation_discrepancy_insert_open BEFORE INSERT ON reconciliation_discrepancies
WHEN NEW.status <> 'open' OR NEW.resolution_note IS NOT NULL OR NEW.resolved_by IS NOT NULL OR NEW.resolved_at IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'discrepancies are recorded open'); END;
-- A run's identity and window never change after creation.
CREATE TRIGGER reconciliation_run_window_immutable BEFORE UPDATE ON reconciliation_runs
WHEN NEW.window_start IS NOT OLD.window_start OR NEW.window_end IS NOT OLD.window_end OR NEW.run_key IS NOT OLD.run_key OR NEW.workspace_id IS NOT OLD.workspace_id
BEGIN SELECT RAISE(ABORT, 'reconciliation run window is immutable'); END;
CREATE TRIGGER reconciliation_run_no_delete BEFORE DELETE ON reconciliation_runs
BEGIN SELECT RAISE(ABORT, 'reconciliation runs are never deleted'); END;
