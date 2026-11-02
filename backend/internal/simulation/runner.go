package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/provider/mock"
	"github.com/railvanta/railvanta/backend/internal/reconciliation"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Webhooks is the event plan of the webhook scenarios.
	Webhooks []WebhookPlan `json:"webhooks,omitempty"`
	// Duplicate is what the duplicate_request burst observed.
	Duplicate *DuplicateEvidence `json:"duplicate,omitempty"`
	// Reconcile is what the settlement_mismatch investigation recorded.
	Reconcile *ReconcileEvidence `json:"reconcile,omitempty"`
}

// DuplicateBurst is how many concurrent requests share one Idempotency-Key.
const DuplicateBurst = 10

// DuplicateEvidence records the acceptance answers the burst observed.
type DuplicateEvidence struct {
	Sent        int `json:"sent"`
	FirstAccept int `json:"first_accept"` // answered 202 and created the payment
	Replays     int `json:"replays"`      // answered 202 with the stored response
	Errors      int `json:"errors"`
	// ConflictStatus and ConflictCode are the answer to the changed-amount request.
	ConflictStatus int    `json:"conflict_status"`
	ConflictCode   string `json:"conflict_code"`
}

// LedgerCounts are payment-scoped row counts compared before and after a
// reconciliation run.
type LedgerCounts struct {
	Journals    int64 `json:"journals"`
	Entries     int64 `json:"entries"`
	Transitions int64 `json:"transitions"`
	Attempts    int64 `json:"attempts"`
}

// ReconcileEvidence ties a settlement_mismatch run to its stored discrepancy.
type ReconcileEvidence struct {
	ReconciliationRunID string       `json:"reconciliation_run_id"`
	DiscrepancyID       string       `json:"discrepancy_id"`
	Before              LedgerCounts `json:"before"`
	After               LedgerCounts `json:"after"`
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
	input.Webhooks = PlanWebhooks(scenario.ID, runID, req.Seed)
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
	var first payment.AcceptResult
	if scenario.ID == DuplicateRequest {
		var steps []Step
		first, input.Duplicate, steps, err = r.duplicateBurst(ctx, accept)
		input.Steps = append(input.Steps, steps...)
		if err != nil {
			r.fail(run, input, "payment was not accepted: "+err.Error())
			return storage.SimulationRun{}, err
		}
	} else {
		first, err = r.payments.Accept(ctx, accept)
		if err != nil {
			r.fail(run, input, "payment was not accepted: "+err.Error())
			return storage.SimulationRun{}, err
		}
		input.Steps = append(input.Steps, Step{"payment_accepted", "Payment " + first.PaymentID + " accepted through the real acceptance path", time.Now().UnixMilli()})
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

// duplicateBurst sends DuplicateBurst concurrent requests with one
// Idempotency-Key through the real acceptance path, then one more with the
// same key and a changed amount. It records the answers it observed.
func (r *Runner) duplicateBurst(ctx context.Context, accept payment.AcceptRequest) (payment.AcceptResult, *DuplicateEvidence, []Step, error) {
	type answer struct {
		res payment.AcceptResult
		err error
	}
	answers := make([]answer, DuplicateBurst)
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i].res, answers[i].err = r.payments.Accept(ctx, accept)
		}()
	}
	wg.Wait()
	ev := &DuplicateEvidence{Sent: DuplicateBurst}
	var first payment.AcceptResult
	paymentIDs := map[string]bool{}
	var firstErr error
	for _, a := range answers {
		switch {
		case a.err != nil:
			ev.Errors++
			if firstErr == nil {
				firstErr = a.err
			}
		case a.res.Replayed:
			ev.Replays++
			paymentIDs[a.res.PaymentID] = true
		default:
			ev.FirstAccept++
			first = a.res
			paymentIDs[a.res.PaymentID] = true
		}
	}
	now := time.Now().UnixMilli()
	steps := []Step{{"duplicate_burst", fmt.Sprintf("Sent %d concurrent requests with one Idempotency-Key: %d first-accept (HTTP %d), %d replay(s) of the stored response, %d error(s); %d distinct payment id(s)",
		ev.Sent, ev.FirstAccept, first.StatusCode, ev.Replays, ev.Errors, len(paymentIDs)), now}}
	if first.PaymentID == "" {
		if firstErr == nil {
			firstErr = errors.New("no request was accepted as the first")
		}
		return first, ev, steps, firstErr
	}
	steps = append(steps, Step{"payment_accepted", "Payment " + first.PaymentID + " accepted through the real acceptance path", now})
	changed := accept
	changed.AmountMinor++
	_, err := r.payments.Accept(ctx, changed)
	switch {
	case errors.Is(err, payment.ErrIdempotencyConflict):
		ev.ConflictStatus, ev.ConflictCode = 409, "idempotency_conflict"
	case err != nil:
		ev.ConflictCode = "error: " + err.Error()
	default:
		ev.ConflictStatus, ev.ConflictCode = 202, "accepted"
	}
	steps = append(steps, Step{"changed_amount_request", fmt.Sprintf("Same Idempotency-Key with amount %d instead of %d answered HTTP %d %s",
		changed.AmountMinor, accept.AmountMinor, ev.ConflictStatus, ev.ConflictCode), time.Now().UnixMilli()})
	return first, ev, steps, nil
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
	if len(input.Webhooks) > 0 && p.Status == string(payment.StatusSucceeded) {
		steps, err := r.deliverWebhooks(db, p, input)
		if err != nil {
			return err
		}
		input.Steps = append(input.Steps, steps...)
	}
	if scenario.ID == SettlementMismatch && p.Status == string(payment.StatusUnknown) && input.Reconcile == nil {
		steps, evidence, err := r.settlementMismatch(ctx, db, run, p)
		if err != nil {
			return err
		}
		input.Steps = append(input.Steps, steps...)
		input.Reconcile = evidence
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
		// The view's timeline reads input_json, so persist the steps added at finalize.
		"input_json": mustJSON(input),
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

// deliverWebhooks sends each planned provider event through payment.ReceiveWebhook,
// the same inbox path the sandbox webhook route uses. It is skipped when the
// first event is already in the inbox, so a resumed run never double-counts.
func (r *Runner) deliverWebhooks(db *gorm.DB, p storage.Payment, input Input) ([]Step, error) {
	attempts, err := storage.ListPaymentAttempts(db, p.WorkspaceID, p.ID)
	if err != nil {
		return nil, err
	}
	var attempt storage.PaymentAttempt
	for _, a := range attempts {
		if a.Status == payment.AttemptSucceeded {
			attempt = a
		}
	}
	if attempt.ID == "" {
		return []Step{{"webhook_delivery", "No succeeded attempt, so no webhook was delivered", time.Now().UnixMilli()}}, nil
	}
	var existing int64
	if err := db.Model(&storage.WebhookInbox{}).Where("workspace_id = ? AND provider_id = ? AND provider_event_id = ?",
		p.WorkspaceID, attempt.ProviderID, input.Webhooks[0].EventID).Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing > 0 {
		return []Step{{"webhook_delivery", "Events were already delivered by an earlier worker", time.Now().UnixMilli()}}, nil
	}
	var steps []Step
	for _, plan := range input.Webhooks {
		event := payment.WebhookEvent{EventID: plan.EventID, Type: plan.Type, PaymentID: p.ID, AttemptID: attempt.ID}
		body, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		var notes []string
		for i := 1; i <= plan.Deliveries; i++ {
			res, err := payment.ReceiveWebhook(context.Background(), r.db, payment.WebhookDelivery{
				WorkspaceID: p.WorkspaceID, ProviderID: attempt.ProviderID, Body: body, Event: event, Now: time.Now().UnixMilli(),
			})
			if err != nil {
				return nil, err
			}
			note := fmt.Sprintf("#%d duplicate=%t", i, res.Duplicate)
			if !res.Duplicate && res.Row.Outcome != nil {
				note += " outcome=" + *res.Row.Outcome
				if res.Row.OutcomeReason != nil {
					note += "/" + *res.Row.OutcomeReason
				}
			}
			notes = append(notes, note)
		}
		steps = append(steps, Step{"webhook_delivery", fmt.Sprintf("Delivered %s event %s %d time(s) through the webhook inbox: [%s]",
			plan.Type, plan.EventID, plan.Deliveries, strings.Join(notes, "] [")), time.Now().UnixMilli()})
	}
	return steps, nil
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
		"journals": snapshot.JournalCount, "webhooks": snapshot.Webhooks, "invariants": items, "extra": snapshot.Extra,
	})))
	return hex.EncodeToString(sum[:])
}

// heldLookupDuration is how long the simulator postpones the recovery lookup
// of the investigated payment so it stays unknown while it is reconciled.
const heldLookupDuration = time.Hour

func ledgerCounts(db *gorm.DB, workspaceID, paymentID string) (LedgerCounts, error) {
	var c LedgerCounts
	if err := db.Model(&storage.LedgerJournal{}).Where("workspace_id = ? AND payment_id = ?", workspaceID, paymentID).Count(&c.Journals).Error; err != nil {
		return c, err
	}
	if err := db.Table("ledger_entries AS e").Joins("JOIN ledger_journals AS j ON j.id = e.journal_id").
		Where("j.workspace_id = ? AND j.payment_id = ?", workspaceID, paymentID).Count(&c.Entries).Error; err != nil {
		return c, err
	}
	if err := db.Model(&storage.PaymentStateTransition{}).Where("workspace_id = ? AND payment_id = ?", workspaceID, paymentID).Count(&c.Transitions).Error; err != nil {
		return c, err
	}
	err := db.Model(&storage.PaymentAttempt{}).Where("workspace_id = ? AND payment_id = ?", workspaceID, paymentID).Count(&c.Attempts).Error
	return c, err
}

// settlementMismatch makes the provider-side charge succeeded while the
// payment stays unknown, reconciles the window and records an evidence-only
// investigation. The simulated provider record is written to the mock
// provider's own charge table and is labelled simulated; payments, attempts and
// journals are never written, only read before and after to prove it.
func (r *Runner) settlementMismatch(ctx context.Context, db *gorm.DB, run storage.SimulationRun, p storage.Payment) ([]Step, *ReconcileEvidence, error) {
	attempts, err := storage.ListPaymentAttempts(db, p.WorkspaceID, p.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(attempts) == 0 {
		return []Step{{"settlement_mismatch", "No attempt exists, so there is no provider charge to compare", time.Now().UnixMilli()}}, nil, nil
	}
	attempt := attempts[len(attempts)-1]
	now := time.Now().UnixMilli()
	// Hold the recovery lookup first so the payment is still unknown when the
	// reconciliation run reads it. Only the command's delivery time moves. A
	// lookup the dispatcher has already claimed is not held by that update: it
	// fails against the outage and reschedules itself, and its retry would find
	// the provider-side charge written below and resolve the payment. So hold,
	// then wait until no lookup is in flight before the charge exists; once held,
	// nothing can be claimed again.
	lookups := db.Model(&storage.OutboxEvent{}).
		Where("workspace_id = ? AND payment_id = ? AND event_type = ?", p.WorkspaceID, p.ID, payment.OutboxPaymentLookup)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := lookups.Session(&gorm.Session{}).Where("status = 'pending'").
			Update("available_at", time.Now().UnixMilli()+heldLookupDuration.Milliseconds()).Error; err != nil {
			return nil, nil, err
		}
		var inFlight int64
		if err := lookups.Session(&gorm.Session{}).Where("status = 'processing'").Count(&inFlight).Error; err != nil {
			return nil, nil, err
		}
		if inFlight == 0 {
			break
		}
		if time.Now().After(deadline) {
			return nil, nil, fmt.Errorf("recovery lookup for payment %s is still running; cannot hold it", p.ID)
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	id, err := storage.NewID()
	if err != nil {
		return nil, nil, err
	}
	charge := mock.Charge{
		ID: id, WorkspaceID: p.WorkspaceID, ProviderID: attempt.ProviderID, ProviderIdempotencyKey: attempt.ProviderIdempotencyKey,
		ProviderReference: mock.ProviderReference(attempt.ProviderID, attempt.ProviderIdempotencyKey), AmountMinor: p.AmountMinor, Currency: p.Currency,
		Status: mock.StatusSucceeded, Scenario: "simulated_settlement", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "provider_id"}, {Name: "provider_idempotency_key"}}, DoNothing: true}).Create(&charge).Error; err != nil {
		return nil, nil, err
	}
	steps := []Step{{"simulated_provider_settlement", fmt.Sprintf("SIMULATED: the mock provider records charge %s as succeeded for key %s while payment %s stays unknown; its recovery lookup is held for the investigation. No payment or journal row is written",
		charge.ProviderReference, attempt.ProviderIdempotencyKey, p.ID), now}}

	before, err := ledgerCounts(db, p.WorkspaceID, p.ID)
	if err != nil {
		return nil, nil, err
	}
	svc := reconciliation.NewService(r.db, nil)
	runKey := "sim_" + run.ID
	result, err := svc.Run(ctx, reconciliation.RunInput{
		WorkspaceID: p.WorkspaceID, RunKey: runKey, WindowStart: p.CreatedAt, WindowEnd: p.CreatedAt + 1, RequestedBy: ActorPrefix + SettlementMismatch,
	})
	if err != nil {
		return nil, nil, err
	}
	views, err := reconciliation.ListDiscrepancies(db, p.WorkspaceID, reconciliation.DiscrepancyFilter{RunID: result.Run.ID, Limit: 50})
	if err != nil {
		return nil, nil, err
	}
	var found *reconciliation.DiscrepancyView
	for i := range views {
		if views[i].PaymentID != nil && *views[i].PaymentID == p.ID {
			found = &views[i]
			break
		}
	}
	steps = append(steps, Step{"reconciliation_run", fmt.Sprintf("Reconciliation run %s compared %d payment(s) in the window and recorded %d discrepancy row(s)",
		result.Run.ID, result.Run.ComparedCount, len(views)), time.Now().UnixMilli()})
	evidence := &ReconcileEvidence{ReconciliationRunID: result.Run.ID, Before: before}
	if found == nil {
		steps = append(steps, Step{"discrepancy", "No discrepancy was recorded for the payment", time.Now().UnixMilli()})
		evidence.After, err = ledgerCounts(db, p.WorkspaceID, p.ID)
		return steps, evidence, err
	}
	evidence.DiscrepancyID = found.ID
	var details reconciliation.Evidence
	_ = json.Unmarshal([]byte(found.Details), &details)
	chargeStatus := "none"
	if details.ProviderCharge != nil {
		chargeStatus = details.ProviderCharge.Status
	}
	steps = append(steps, Step{"discrepancy", fmt.Sprintf("Discrepancy %s kind=%s status=%s; provider evidence (simulated=%t): %q; provider charge %s, payment %s",
		found.ID, found.Kind, found.Status, details.Simulated, details.Summary, chargeStatus, details.Payment.Status), time.Now().UnixMilli()})

	note := "Investigating: the provider recorded the charge as succeeded but the payment is still unknown. Evidence only: no adjusting journal is posted and the payment is not changed."
	resolved, err := svc.Resolve(ctx, reconciliation.Resolution{
		WorkspaceID: p.WorkspaceID, Role: "operator", ID: found.ID, ToStatus: reconciliation.StatusInvestigating, Note: note, Actor: ActorPrefix + SettlementMismatch,
	})
	switch {
	case err == nil:
		steps = append(steps, Step{"discrepancy_resolution", fmt.Sprintf("Discrepancy moved %s -> %s with note %q (evidence-only resolution path)", found.Status, resolved.Status, note), time.Now().UnixMilli()})
	case errors.Is(err, reconciliation.ErrInvalidTransition):
		steps = append(steps, Step{"discrepancy_resolution", "Discrepancy already carried this status", time.Now().UnixMilli()})
	default:
		return nil, nil, err
	}
	if evidence.After, err = ledgerCounts(db, p.WorkspaceID, p.ID); err != nil {
		return nil, nil, err
	}
	changed := absDiff(before.Journals, evidence.After.Journals) + absDiff(before.Entries, evidence.After.Entries) +
		absDiff(before.Transitions, evidence.After.Transitions) + absDiff(before.Attempts, evidence.After.Attempts)
	steps = append(steps, Step{"ledger_rows_changed", fmt.Sprintf("ledger rows changed: %d (journals %d->%d, entries %d->%d, payment transitions %d->%d, attempts %d->%d)",
		changed, before.Journals, evidence.After.Journals, before.Entries, evidence.After.Entries,
		before.Transitions, evidence.After.Transitions, before.Attempts, evidence.After.Attempts), time.Now().UnixMilli()})
	return steps, evidence, nil
}

func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}
