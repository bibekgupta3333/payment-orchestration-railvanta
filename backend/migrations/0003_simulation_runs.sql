-- WBS 3.2.6: extend simulation_runs with the injected fault, invariant results
-- with evidence, a state fingerprint for same-seed comparison and a failure
-- reason. Columns are nullable: a run has no invariants until the backend has
-- evidence for them.
ALTER TABLE simulation_runs ADD COLUMN injected_fault TEXT;
ALTER TABLE simulation_runs ADD COLUMN invariants_json TEXT;
ALTER TABLE simulation_runs ADD COLUMN fingerprint TEXT;
ALTER TABLE simulation_runs ADD COLUMN failure_reason TEXT;
ALTER TABLE simulation_runs ADD COLUMN updated_at INTEGER;
CREATE INDEX simulation_runs_workspace_created ON simulation_runs(workspace_id, created_at DESC, id DESC);
