package simulation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// SampleScenarios are the headline scenarios that SANDBOX_SAMPLE_DATA runs once
// at startup, in the same order as the dashboard's "Load sample data" card
// (frontend/lib/hooks/sample-data.ts).
var SampleScenarios = []string{
	"success", "decline", "duplicate_request", "timeout_after_success",
	"outage", "duplicate_posting", "duplicate_webhook", "settlement_mismatch",
}

// SampleActor is the audit actor reference of startup sample runs.
const SampleActor = "sample-data"

// SampleAmountMinor is the fixed $100.00 amount of every sample run.
const SampleAmountMinor = 10000

// Sample timing: how often a run is re-read, how long one run may take, and
// how often the wait for the demo workspace is logged.
const (
	samplePoll    = 250 * time.Millisecond
	sampleRunCap  = 90 * time.Second
	sampleWaitLog = 30 * time.Second
)

// SampleSeed is the deterministic seed that marks a scenario's sample run.
func SampleSeed(scenario string) string { return "sample-" + scenario }

// LatestSampleRun returns the most recent run in the workspace carrying the
// scenario's sample seed, or storage.ErrNotFound.
func LatestSampleRun(db *gorm.DB, workspaceID, scenario string) (storage.SimulationRun, error) {
	var run storage.SimulationRun
	err := db.Where("workspace_id = ? AND scenario = ? AND seed = ?", workspaceID, scenario, SampleSeed(scenario)).
		Order("created_at DESC, id DESC").Limit(1).Find(&run).Error
	if err != nil {
		return storage.SimulationRun{}, err
	}
	if run.ID == "" {
		return storage.SimulationRun{}, storage.ErrNotFound
	}
	return run, nil
}

// SeedSamples runs each sample scenario once, strictly one at a time, in the
// workspace. A scenario that already has a run with its sample seed (any
// status) is skipped, so a restart over a retained volume adds nothing. It
// first waits for the workspace's seeded mock methods, because a container
// stack seeds the demo workspace after the API starts. An error on one
// scenario is logged and the next one still runs. It never logs secrets.
func (r *Runner) SeedSamples(ctx context.Context, workspaceID string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if err := r.awaitSampleMethods(ctx, workspaceID, logger); err != nil {
		return err
	}
	created := 0
	for _, scenario := range SampleScenarios {
		if err := ctx.Err(); err != nil {
			return err
		}
		existing, err := LatestSampleRun(r.db.WithContext(ctx), workspaceID, scenario)
		switch {
		case err == nil:
			logger.Info("sample data present", "scenario", scenario, "run_id", existing.ID, "status", existing.Status)
			continue
		case !errors.Is(err, storage.ErrNotFound):
			logger.Warn("sample data lookup failed", "scenario", scenario, "error", err.Error())
			continue
		}
		run, err := r.Start(ctx, StartRequest{
			WorkspaceID: workspaceID, ActorReference: SampleActor, Scenario: scenario,
			Seed: SampleSeed(scenario), AmountMinor: SampleAmountMinor, Currency: "USD",
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Warn("sample data run not started", "scenario", scenario, "error", err.Error())
			continue
		}
		created++
		status, err := r.awaitSample(ctx, workspaceID, run.ID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.Warn("sample data run not finished", "scenario", scenario, "run_id", run.ID, "status", status, "error", err.Error())
			continue
		}
		logger.Info("sample data run finished", "scenario", scenario, "run_id", run.ID, "status", status)
	}
	logger.Info("sample data ready", "workspace_id", workspaceID, "created", created, "scenarios", len(SampleScenarios))
	return nil
}

// awaitSampleMethods blocks until every sample scenario's seeded mock payment
// method exists in the workspace, or ctx ends.
func (r *Runner) awaitSampleMethods(ctx context.Context, workspaceID string, logger *slog.Logger) error {
	started, lastLog := time.Now(), time.Time{}
	for {
		missing := ""
		for _, id := range SampleScenarios {
			scenario, ok := Lookup(id)
			if !ok {
				continue
			}
			if _, err := storage.FindPaymentMethod(r.db.WithContext(ctx), workspaceID, methodFor(scenario.MockScenario)); err != nil {
				missing = id
				break
			}
		}
		if missing == "" {
			return nil
		}
		if lastLog.IsZero() || time.Since(lastLog) >= sampleWaitLog {
			logger.Info("sample data waiting for the demo workspace to be seeded", "workspace_id", workspaceID, "waited_ms", time.Since(started).Milliseconds())
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(samplePoll):
		}
	}
}

// awaitSample polls the run until it leaves running, the cap passes or ctx ends.
func (r *Runner) awaitSample(ctx context.Context, workspaceID, runID string) (string, error) {
	deadline := time.Now().Add(sampleRunCap)
	for {
		run, err := storage.GetSimulationRun(r.db.WithContext(ctx), workspaceID, runID)
		if err != nil {
			return "", err
		}
		if run.Status != storage.SimulationRunning {
			return run.Status, nil
		}
		if time.Now().After(deadline) {
			return run.Status, errors.New("still running after " + sampleRunCap.String())
		}
		select {
		case <-ctx.Done():
			return run.Status, ctx.Err()
		case <-time.After(samplePoll):
		}
	}
}
