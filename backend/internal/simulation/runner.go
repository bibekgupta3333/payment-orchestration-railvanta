package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// ActorPrefix marks audit actors of simulated requests.
const ActorPrefix = "simulator:"

// Step is one recorded action the simulator itself performed (not provider or
// dispatcher behaviour, which is read from persisted rows).
type Step struct {
	Step   string `json:"step"`
	Detail string `json:"detail"`
	At     int64  `json:"at"`
}

// Input is the persisted request of a run (simulation_runs.input_json).
type Input struct {
	PaymentMethodID   string `json:"payment_method_id"`
	AmountMinor       string `json:"amount_minor"`
	Currency          string `json:"currency"`
	IdempotencyKey    string `json:"idempotency_key"`
	MerchantReference string `json:"merchant_reference"`
	Steps             []Step `json:"steps"`
}

// Result is the persisted outcome of a finished run (simulation_runs.result_json).
type Result struct {
	FinalStatus    string `json:"final_status"`
	ExpectedStatus string `json:"expected_status"`
	Steps          []Step `json:"steps"`
}

// Options configures a Runner. Zero values select defaults.
type Options struct {
	// Notify wakes the dispatcher after a payment is accepted.
	Notify func()
	// Poll is how often a worker re-reads persisted state (default 100ms).
	Poll time.Duration
	// Deadline bounds how long a run waits for the pipeline (default 90s).
	Deadline time.Duration
}

// Runner starts runs and drives them to completion by observing the pipeline.
type Runner struct {
	db       *gorm.DB
	payments *payment.Service
	opts     Options
	mu       sync.Mutex
	active   map[string]bool
}

// NewRunner creates a runner over the store's root handle.
func NewRunner(db *gorm.DB, payments *payment.Service, opts Options) *Runner {
	if opts.Poll <= 0 {
		opts.Poll = 100 * time.Millisecond
	}
	if opts.Deadline <= 0 {
		opts.Deadline = 90 * time.Second
	}
	return &Runner{db: db, payments: payments, opts: opts, active: map[string]bool{}}
}

// StartRequest is a validated simulation request. AmountMinor 0 and an empty
// Currency select the seed-derived amount and USD.
type StartRequest struct {
	WorkspaceID    string
	ActorReference string
	Scenario       string
	Seed           string
	AmountMinor    int64
	Currency       string
}

// ErrMethodUnavailable means the workspace has no seeded method for the scenario.
var ErrMethodUnavailable = errors.New("no seeded mock payment method for this scenario in the workspace")

func methodFor(mockScenario string) string {
	for _, m := range auth.ScenarioMethods {
		if m.Scenario == mockScenario {
			return m.ID
		}
	}
	return ""
}

// Start validates the scenario, persists the run, accepts the payment through
// the real acceptance path and begins watching the run.
func (r *Runner) Start(ctx context.Context, req StartRequest) (storage.SimulationRun, error) {
	scenario, ok := Lookup(req.Scenario)
	if !ok {
		return storage.SimulationRun{}, ErrUnknownScenario
	}
	if !scenario.Implemented {
		return storage.SimulationRun{}, &NotImplementedError{Scenario: scenario.ID, Dependency: scenario.MissingDependency}
	}
	amount, currency := req.AmountMinor, req.Currency
	if amount == 0 {
		amount = SeedAmount(req.Seed)
	}
	if currency == "" {
		currency = "USD"
	}
	methodID := methodFor(scenario.MockScenario)
	if _, err := storage.FindPaymentMethod(r.db.WithContext(ctx), req.WorkspaceID, methodID); err != nil {
		return storage.SimulationRun{}, ErrMethodUnavailable
	}
	runID, err := storage.NewID()
	if err != nil {
		return storage.SimulationRun{}, err
	}
	input := Input{
		PaymentMethodID: methodID, AmountMinor: strconv.FormatInt(amount, 10), Currency: currency,
		IdempotencyKey: "sim_" + runID, MerchantReference: "sim:" + runID,
	}
	now := time.Now().UnixMilli()
	input.Steps = append(input.Steps, Step{"run_accepted", fmt.Sprintf("Run accepted for scenario %s with seed %q", scenario.ID, req.Seed), now})
	run := storage.SimulationRun{
		ID: runID, WorkspaceID: req.WorkspaceID, Scenario: scenario.ID, Seed: req.Seed, Status: storage.SimulationRunning,
		InjectedFault: &scenario.InjectedFault, CreatedAt: now, UpdatedAt: &now,
	}
	run.InputJSON = mustJSON(input)
	if err := storage.InsertSimulationRun(r.db.WithContext(ctx), &run); err != nil {
		return storage.SimulationRun{}, err
	}
	accept := payment.AcceptRequest{
		WorkspaceID: req.WorkspaceID, ActorReference: ActorPrefix + req.ActorReference, IdempotencyKey: input.IdempotencyKey,
		AmountMinor: amount, Currency: currency, PaymentMethodID: methodID, MerchantReference: input.MerchantReference,
	}
	first, err := r.payments.Accept(ctx, accept)
	if err != nil {
		r.fail(run, input, "payment was not accepted: "+err.Error())
		return storage.SimulationRun{}, err
	}
	input.Steps = append(input.Steps, Step{"payment_accepted", "Payment " + first.PaymentID + " accepted through the real acceptance path", time.Now().UnixMilli()})
	if scenario.ID == DuplicateRequest {
		input.Steps = append(input.Steps, r.duplicateAccepts(ctx, accept, first.PaymentID)...)
	}
	run.PaymentID = &first.PaymentID
	run.InputJSON = mustJSON(input)
	updated := time.Now().UnixMilli()
	if _, err := storage.UpdateSimulationRun(r.db.WithContext(ctx), req.WorkspaceID, runID, storage.SimulationRunning,
		map[string]any{"payment_id": first.PaymentID, "input_json": run.InputJSON, "updated_at": updated}); err != nil {
		return storage.SimulationRun{}, err
	}
	if r.opts.Notify != nil {
		r.opts.Notify()
	}
	r.Watch(req.WorkspaceID, runID)
	return storage.GetSimulationRun(r.db.WithContext(ctx), req.WorkspaceID, runID)
}

// duplicateAccepts replays the same request twice concurrently and records
// what the acceptance path answered.
func (r *Runner) duplicateAccepts(ctx context.Context, accept payment.AcceptRequest, original string) []Step {
	results := make([]string, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.payments.Accept(ctx, accept)
			switch {
			case err != nil:
				results[i] = "error: " + err.Error()
			case res.PaymentID != original:
				results[i] = "DIFFERENT payment " + res.PaymentID
			default:
				results[i] = fmt.Sprintf("replayed=%t payment=%s", res.Replayed, res.PaymentID)
			}
		}()
	}
	wg.Wait()
	return []Step{{"duplicate_requests", "Two concurrent replays with the same Idempotency-Key answered: [" + results[0] + "] [" + results[1] + "]", time.Now().UnixMilli()}}
}

// Watch starts a worker for the run unless one is already active in this
// process. GET handlers call it so a run interrupted by a restart resumes.
func (r *Runner) Watch(workspaceID, runID string) {
	r.mu.Lock()
	if r.active[runID] {
		r.mu.Unlock()
		return
	}
	r.active[runID] = true
	r.mu.Unlock()
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.active, runID)
			r.mu.Unlock()
		}()
		ctx := context.Background()
		for {
			done, err := r.Advance(ctx, workspaceID, runID)
			if err != nil || done {
				return
			}
			time.Sleep(r.opts.Poll)
		}
	}()
}

// Advance observes the pipeline once and finalizes the run when its end state
// is reached or the deadline passed. It reports whether the run is finished.
func (r *Runner) Advance(ctx context.Context, workspaceID, runID string) (bool, error) {
	db := r.db.WithContext(ctx)
	run, err := storage.GetSimulationRun(db, workspaceID, runID)
	if err != nil {
		return true, err
	}
	if run.Status != storage.SimulationRunning {
		return true, nil
	}
	scenario, _ := Lookup(run.Scenario)
	if run.PaymentID == nil {
		if time.Since(time.UnixMilli(run.CreatedAt)) > r.opts.Deadline {
			var input Input
			_ = json.Unmarshal([]byte(run.InputJSON), &input)
			r.fail(run, input, "no payment was accepted")
			return true, nil
		}
		return false, nil
	}
	p, err := storage.GetPayment(db, workspaceID, *run.PaymentID)
	if err != nil {
		return true, err
	}
	ready, err := r.endState(db, scenario, p)
	if err != nil {
		return true, err
	}
	expired := time.Since(time.UnixMilli(run.CreatedAt)) > r.opts.Deadline
	if !ready && !expired {
		return false, nil
	}
	reason := ""
	if !ready {
		reason = fmt.Sprintf("the pipeline did not reach the expected end state within %s (payment is %s)", r.opts.Deadline, p.Status)
	}
	return true, r.finalize(ctx, run, scenario, p, reason)
}

// endState reports whether the pipeline has produced the end state evidence the
// scenario waits for.
func (r *Runner) endState(db *gorm.DB, scenario Scenario, p storage.Payment) (bool, error) {
	status := payment.Status(p.Status)
	if status.Terminal() {
		return true, nil
	}
	if scenario.ExpectedStatus != payment.StatusUnknown || status != payment.StatusUnknown {
		return false, nil
	}
	// Unknown is the expected end state, but only once recovery has asked the
	// original provider at least once and the answer left it unknown.
	var n int64
	err := db.Model(&storage.OutboxEvent{}).
		Where("workspace_id = ? AND payment_id = ? AND event_type = ? AND attempts >= 1 AND status = 'pending' AND last_error IS NOT NULL",
			p.WorkspaceID, p.ID, payment.OutboxPaymentLookup).Count(&n).Error
	return n > 0, err
}

func (r *Runner) finalize(ctx context.Context, run storage.SimulationRun, scenario Scenario, p storage.Payment, failure string) error {
	db := r.db.WithContext(ctx)
	var input Input
	if err := json.Unmarshal([]byte(run.InputJSON), &input); err != nil {
		return err
	}
	if scenario.ID == DuplicatePosting && p.Status == string(payment.StatusSucceeded) {
		step, err := r.replayPosting(db, p)
		if err != nil {
			return err
		}
		input.Steps = append(input.Steps, step)
	}
	invariants, snapshot, err := CheckInvariants(db, scenario, run, input, p)
	if err != nil {
		return err
	}
	status := storage.SimulationCompleted
	if failure != "" {
		status = storage.SimulationFailed
	}
	now := time.Now().UnixMilli()
	input.Steps = append(input.Steps, Step{"run_finished", "Invariants evaluated against persisted evidence", now})
	result := Result{FinalStatus: p.Status, ExpectedStatus: string(scenario.ExpectedStatus), Steps: input.Steps}
	fingerprint := Fingerprint(scenario, input, snapshot, invariants)
	updates := map[string]any{
		"status": status, "result_json": mustJSON(result), "invariants_json": mustJSON(invariants),
		"fingerprint": fingerprint, "completed_at": now, "updated_at": now,
	}
	if failure != "" {
		updates["failure_reason"] = failure
	}
	_, err = storage.UpdateSimulationRun(db, run.WorkspaceID, run.ID, storage.SimulationRunning, updates)
	return err
}

// replayPosting posts the success journal twice more in separate transactions.
func (r *Runner) replayPosting(db *gorm.DB, p storage.Payment) (Step, error) {
	attempts, err := storage.ListPaymentAttempts(db, p.WorkspaceID, p.ID)
	if err != nil {
		return Step{}, err
	}
	providerID := ""
	for _, a := range attempts {
		if a.Status == payment.AttemptSucceeded {
			providerID = a.ProviderID
		}
	}
	if providerID == "" {
		return Step{"duplicate_ledger_post", "No succeeded attempt, so nothing was posted twice", time.Now().UnixMilli()}, nil
	}
	notes := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		var posting *ledger.Posting
		err := db.Transaction(func(tx *gorm.DB) error {
			var postErr error
			posting, postErr = ledger.PostPaymentSuccess(tx, p.WorkspaceID, p.ID, providerID, p.Currency, p.AmountMinor)
			return postErr
		})
		if err != nil {
			notes = append(notes, "error: "+err.Error())
			continue
		}
		notes = append(notes, fmt.Sprintf("already_posted=%t journal=%s", posting.AlreadyPosted, posting.Journal.ID))
	}
	return Step{"duplicate_ledger_post", "Posted the success journal again twice: [" + notes[0] + "] [" + notes[1] + "]", time.Now().UnixMilli()}, nil
}

func (r *Runner) fail(run storage.SimulationRun, input Input, reason string) {
	now := time.Now().UnixMilli()
	input.Steps = append(input.Steps, Step{"run_failed", reason, now})
	_, _ = storage.UpdateSimulationRun(r.db, run.WorkspaceID, run.ID, storage.SimulationRunning, map[string]any{
		"status": storage.SimulationFailed, "failure_reason": reason, "completed_at": now, "updated_at": now,
		"result_json": mustJSON(Result{Steps: input.Steps}),
	})
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// Fingerprint hashes the scenario-relevant end state (never IDs or times), so
// two runs with the same scenario and seed compare equal when the pipeline
// behaved identically.
func Fingerprint(scenario Scenario, input Input, snapshot Snapshot, invariants []Invariant) string {
	type inv struct{ ID, Status string }
	items := make([]inv, len(invariants))
	for i, v := range invariants {
		items[i] = inv{v.ID, v.Status}
	}
	sum := sha256.Sum256([]byte(mustJSON(map[string]any{
		"scenario": scenario.ID, "amount_minor": input.AmountMinor, "currency": input.Currency,
		"final_status": snapshot.FinalStatus, "transitions": snapshot.Transitions, "attempts": snapshot.Attempts,
		"journals": snapshot.JournalCount, "invariants": items,
	})))
	return hex.EncodeToString(sum[:])
}
