package storage

import "gorm.io/gorm"

// Simulation run statuses stored in simulation_runs.status.
const (
	SimulationRunning   = "running"
	SimulationCompleted = "completed"
	SimulationFailed    = "failed"
)

// InsertSimulationRun stores a new run.
func InsertSimulationRun(tx *gorm.DB, run *SimulationRun) error {
	return tx.Create(run).Error
}

// GetSimulationRun reads one run scoped to the workspace.
func GetSimulationRun(tx *gorm.DB, workspaceID, runID string) (SimulationRun, error) {
	var run SimulationRun
	if err := tx.Where("workspace_id = ? AND id = ?", workspaceID, runID).Take(&run).Error; err != nil {
		return SimulationRun{}, notFound(err)
	}
	return run, nil
}

// UpdateSimulationRun applies column updates to a run that is still in the
// expected status (a finished run is never rewritten). It reports whether a
// row changed.
func UpdateSimulationRun(tx *gorm.DB, workspaceID, runID, fromStatus string, updates map[string]any) (bool, error) {
	result := tx.Model(&SimulationRun{}).Where("workspace_id = ? AND id = ? AND status = ?", workspaceID, runID, fromStatus).Updates(updates)
	return result.RowsAffected == 1, result.Error
}
