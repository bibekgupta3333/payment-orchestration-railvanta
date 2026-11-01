-- WBS 4.3: event inbox investigation, signature, processing outcome and audit hardening.
-- duplicate_count and last_received_at already exist from 0001; this migration adds the
-- columns that explain what a delivery did and makes the inbox identity immutable.

ALTER TABLE webhook_inbox ADD COLUMN event_type TEXT NOT NULL DEFAULT 'unknown' CHECK (length(event_type) BETWEEN 1 AND 100);
ALTER TABLE webhook_inbox ADD COLUMN payment_id TEXT;
ALTER TABLE webhook_inbox ADD COLUMN attempt_id TEXT;
-- Only a backend-recorded verification may be anything other than not_applicable.
ALTER TABLE webhook_inbox ADD COLUMN signature_status TEXT NOT NULL DEFAULT 'not_applicable' CHECK (signature_status IN ('not_applicable', 'verified', 'rejected'));
-- applied: a state change was made. ignored: recorded with no state change (see outcome_reason).
ALTER TABLE webhook_inbox ADD COLUMN outcome TEXT CHECK (outcome IS NULL OR outcome IN ('applied', 'ignored'));
ALTER TABLE webhook_inbox ADD COLUMN outcome_reason TEXT CHECK (outcome_reason IS NULL OR length(outcome_reason) BETWEEN 1 AND 100);
-- A repeated provider event ID with a different payload hash is an investigation item.
ALTER TABLE webhook_inbox ADD COLUMN investigation_flag INTEGER NOT NULL DEFAULT 0 CHECK (investigation_flag IN (0, 1));
ALTER TABLE webhook_inbox ADD COLUMN conflicting_payload_hash TEXT CHECK (conflicting_payload_hash IS NULL OR length(conflicting_payload_hash) = 64);
ALTER TABLE webhook_inbox ADD COLUMN conflict_count INTEGER NOT NULL DEFAULT 0 CHECK (conflict_count >= 0);

CREATE INDEX webhook_inbox_workspace_received ON webhook_inbox(workspace_id, received_at, id);
CREATE INDEX webhook_inbox_payment ON webhook_inbox(workspace_id, payment_id) WHERE payment_id IS NOT NULL;
CREATE INDEX outbox_workspace_created ON outbox_events(workspace_id, created_at, id);

-- The first delivery's identity and payload are never overwritten; later deliveries only
-- change counters, timestamps and the investigation fields.
CREATE TRIGGER webhook_inbox_identity_immutable BEFORE UPDATE ON webhook_inbox
WHEN NEW.id IS NOT OLD.id OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.provider_id IS NOT OLD.provider_id
  OR NEW.provider_event_id IS NOT OLD.provider_event_id OR NEW.payload_hash IS NOT OLD.payload_hash
  OR NEW.payload IS NOT OLD.payload OR NEW.received_at IS NOT OLD.received_at
BEGIN SELECT RAISE(ABORT, 'webhook inbox identity and payload are immutable'); END;
CREATE TRIGGER webhook_inbox_no_delete BEFORE DELETE ON webhook_inbox
BEGIN SELECT RAISE(ABORT, 'webhook inbox is append-only'); END;
CREATE TRIGGER webhook_inbox_counters_monotonic BEFORE UPDATE ON webhook_inbox
WHEN NEW.duplicate_count < OLD.duplicate_count OR NEW.conflict_count < OLD.conflict_count OR NEW.last_received_at < OLD.last_received_at
  OR (OLD.investigation_flag = 1 AND NEW.investigation_flag = 0)
BEGIN SELECT RAISE(ABORT, 'webhook inbox counters and investigation flag only move forward'); END;
