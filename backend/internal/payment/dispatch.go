package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/routing"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// DispatchActor is the audit actor for transitions made by the dispatcher.
const DispatchActor = "system:dispatcher"

// ProviderIdempotencyKey is the stable key sent to the provider for one
// attempt. It is derived from the payment and attempt number, persisted on the
// attempt before any network call, and reused for every lookup or replay.
func ProviderIdempotencyKey(paymentID string, attemptNumber int64) string {
	return "rv_" + paymentID + "_" + strconv.FormatInt(attemptNumber, 10)
}

// PreparedAttempt is a committed submitted attempt ready for the provider call.
type PreparedAttempt struct {
	WorkspaceID    string
	PaymentID      string
	AttemptID      string
	ProviderID     string
	ProviderCode   string
	IdempotencyKey string
	TokenReference string
	AmountMinor    int64
	Currency       string
	SubmittedAt    int64
}

// ExecutorHooks are test-only fault points. A returned error aborts Handle at
// that point without recording anything further (simulating a crash).
type ExecutorHooks struct {
	// AfterPrepare runs after the pre-submit transaction commits, before Submit.
	AfterPrepare func(ctx context.Context, attempt PreparedAttempt) error
	// BeforeRecord runs after Submit returns, before the outcome transaction.
	BeforeRecord func(ctx context.Context, attempt PreparedAttempt, result provider.Result) error
}

// ExecutorOptions configures an Executor. Zero values select defaults.
type ExecutorOptions struct {
	// Providers maps provider codes to adapters; it is also the Router's set.
	Providers ProviderSet
	// ProviderTimeout is the deadline for one Submit call (default 10s).
	ProviderTimeout time.Duration
	// PersistTimeout bounds each outcome transaction, which runs on a context
	// detached from shutdown cancellation (default 10s).
	PersistTimeout time.Duration
	Now            func() time.Time
	Metrics        *ExecutionMetrics
	Logger         *slog.Logger
	Hooks          ExecutorHooks
}

// Executor handles payment.dispatch commands: route, persist a submitted
// attempt, call the provider with no transaction open, then record the outcome.
type Executor struct {
	db     *gorm.DB
	opts   ExecutorOptions
	router Router
}

// NewExecutor creates an executor over the store's root handle.
func NewExecutor(db *gorm.DB, opts ExecutorOptions) *Executor {
	if opts.ProviderTimeout <= 0 {
		opts.ProviderTimeout = 10 * time.Second
	}
	if opts.PersistTimeout <= 0 {
		opts.PersistTimeout = 10 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Metrics == nil {
		opts.Metrics = NewExecutionMetrics()
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if opts.Providers == nil {
		opts.Providers = ProviderSet{}
	}
	return &Executor{db: db, opts: opts, router: Router{Providers: opts.Providers}}
}

// Router returns the router the executor dispatches with.
func (e *Executor) Router() Router { return e.router }

// Metrics returns the executor's metrics.
func (e *Executor) Metrics() *ExecutionMetrics { return e.opts.Metrics }

// Register binds the executor to the dispatcher's payment.dispatch command.
func (e *Executor) Register(d *event.Dispatcher) { d.Register(OutboxPaymentDispatch, e.Handle) }

// Handle is the event.Handler for payment.dispatch.
func (e *Executor) Handle(ctx context.Context, claim event.Claim) error {
	var command DispatchCommand
	if err := json.Unmarshal([]byte(claim.Payload), &command); err != nil || command.PaymentID == "" || command.WorkspaceID != claim.WorkspaceID {
		return fmt.Errorf("invalid %s payload for event %s", OutboxPaymentDispatch, claim.EventID)
	}
	var prepared *PreparedAttempt
	err := e.transact(ctx, func(tx *gorm.DB) error {
		var err error
		prepared, err = e.prepare(ctx, tx, claim, command)
		return err
	})
	if err != nil || prepared == nil {
		return err
	}
	e.opts.Metrics.attemptsStarted.Add(1)
	if hook := e.opts.Hooks.AfterPrepare; hook != nil {
		if err := hook(ctx, *prepared); err != nil {
			return err
		}
	}
	adapter := e.opts.Providers[prepared.ProviderCode]
	callCtx, cancel := context.WithTimeout(ctx, e.opts.ProviderTimeout)
	result := adapter.Submit(callCtx, provider.SubmitRequest{
		WorkspaceID: prepared.WorkspaceID, ProviderID: prepared.ProviderID, IdempotencyKey: prepared.IdempotencyKey,
		AmountMinor: prepared.AmountMinor, Currency: prepared.Currency, TokenReference: prepared.TokenReference,
	})
	cancel()
	if hook := e.opts.Hooks.BeforeRecord; hook != nil {
		if err := hook(ctx, *prepared, result); err != nil {
			return err
		}
	}
	return e.record(ctx, claim, *prepared, result)
}

// transact runs fn in a short transaction, retrying only on SQLITE_BUSY: the
// transactions here have no external side effects.
func (e *Executor) transact(ctx context.Context, fn func(tx *gorm.DB) error) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.opts.PersistTimeout)
	defer cancel()
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = e.db.WithContext(persistCtx).Transaction(fn)
		if !storage.IsBusy(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return err
}

// prepare is the pre-submit transaction (2.2.8). It returns nil when the
// command needed no provider call; the claim is then already completed in tx.
func (e *Executor) prepare(ctx context.Context, tx *gorm.DB, claim event.Claim, command DispatchCommand) (*PreparedAttempt, error) {
	now := e.opts.Now().UnixMilli()
	payment, err := storage.GetPayment(tx, command.WorkspaceID, command.PaymentID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, event.Complete(tx, claim, now)
	}
	if err != nil {
		return nil, err
	}
	switch Status(payment.Status) {
	case StatusCreated:
	case StatusProcessing:
		// A previous owner's lease expired after it committed a submitted
		// attempt. That attempt may have executed: never resubmit it elsewhere.
		return nil, e.reclaimStale(tx, claim, payment, now)
	default:
		// Unknown, terminal or canceled payments need no dispatch.
		return nil, event.Complete(tx, claim, now)
	}

	decision, err := e.router.Decide(ctx, tx, RouteInput{
		WorkspaceID: payment.WorkspaceID, PaymentMethodID: payment.PaymentMethodID, Currency: payment.Currency, AmountMinor: payment.AmountMinor,
	})
	if errors.Is(err, routing.ErrNoEligibleProvider) || errors.Is(err, routing.ErrMethodNotFound) {
		e.opts.Metrics.noRoute.Add(1)
		return nil, e.failUnroutable(tx, claim, payment, err, now)
	}
	if err != nil {
		return nil, err
	}
	if err := GuardNewAttempt(tx, payment.WorkspaceID, payment.ID, decision.ProviderID); err != nil {
		e.opts.Metrics.failoverBlocked.Add(1)
		return nil, err
	}
	token, err := storage.FindMethodToken(tx, payment.WorkspaceID, payment.PaymentMethodID, decision.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("token for routed provider: %w", err)
	}
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, From: StatusCreated, To: StatusProcessing, Version: payment.Version,
		Reason: "dispatch claimed; routed to provider " + decision.ProviderID, ActorReference: DispatchActor, Now: now,
	}); err != nil {
		return nil, err
	}
	var attemptNumber int64
	if err := tx.Model(&storage.PaymentAttempt{}).Where("workspace_id = ? AND payment_id = ?", payment.WorkspaceID, payment.ID).
		Select("coalesce(max(attempt_number), 0)").Scan(&attemptNumber).Error; err != nil {
		return nil, err
	}
	attemptNumber++
	attemptID, err := storage.NewID()
	if err != nil {
		return nil, err
	}
	ruleID := decision.RuleID
	attempt := storage.PaymentAttempt{
		ID: attemptID, WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, ProviderID: decision.ProviderID, RoutingRuleID: &ruleID,
		RoutingReason: decision.Reason, AttemptNumber: attemptNumber, ProviderIdempotencyKey: ProviderIdempotencyKey(payment.ID, attemptNumber),
		Status: AttemptCreated, CreatedAt: now, UpdatedAt: now,
	}
	if err := storage.InsertAttempt(tx, &attempt); err != nil {
		return nil, err
	}
	// Submission intent is durable before any network call.
	if err := storage.UpdateAttemptStatus(tx, storage.AttemptUpdate{
		WorkspaceID: payment.WorkspaceID, AttemptID: attempt.ID, From: []string{AttemptCreated}, To: AttemptSubmitted, SubmittedAt: &now, Now: now,
	}); err != nil {
		return nil, err
	}
	return &PreparedAttempt{
		WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, AttemptID: attempt.ID, ProviderID: decision.ProviderID,
		ProviderCode: decision.ProviderCode, IdempotencyKey: attempt.ProviderIdempotencyKey, TokenReference: token.TokenReference,
		AmountMinor: payment.AmountMinor, Currency: payment.Currency, SubmittedAt: now,
	}, nil
}

// reclaimStale moves a processing payment whose attempt is still submitted to
// unknown and schedules a lookup at that attempt's provider (2.2.11).
func (e *Executor) reclaimStale(tx *gorm.DB, claim event.Claim, payment storage.Payment, now int64) error {
	attempts, err := storage.ListPaymentAttempts(tx, payment.WorkspaceID, payment.ID)
	if err != nil {
		return err
	}
	for _, a := range attempts {
		if a.Status == AttemptSubmitted {
			e.opts.Metrics.staleReclaims.Add(1)
			e.opts.Metrics.unknownOutcomes.Add(1)
			e.opts.Metrics.observeOutcome(a.ProviderID, "stale_submitted")
			_, err := ApplyUnknown(tx, Outcome{
				WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, AttemptID: a.ID, Code: "stale_submitted",
				Reason:         "dispatch lease expired with the attempt submitted; treated as possibly executed, lookup scheduled at the original provider",
				ActorReference: DispatchActor, Now: now, Claim: &claim,
			})
			return err
		}
	}
	return fmt.Errorf("payment %s is processing without a submitted attempt", payment.ID)
}

// failUnroutable records created -> processing -> failed with no attempt: no
// provider was contacted, so nothing can have executed.
func (e *Executor) failUnroutable(tx *gorm.DB, claim event.Claim, payment storage.Payment, cause error, now int64) error {
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, From: StatusCreated, To: StatusProcessing, Version: payment.Version,
		Reason: "dispatch claimed", ActorReference: DispatchActor, Now: now,
	}); err != nil {
		return err
	}
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: payment.WorkspaceID, PaymentID: payment.ID, From: StatusProcessing, To: StatusFailed, Version: payment.Version + 1,
		Reason: "routing failed before any provider call: " + cause.Error(), ActorReference: DispatchActor, Now: now,
	}); err != nil {
		return err
	}
	return event.Complete(tx, claim, now)
}

// record persists the provider outcome in a new transaction (2.2.9, 2.2.10).
func (e *Executor) record(ctx context.Context, claim event.Claim, prepared PreparedAttempt, result provider.Result) error {
	var applied Applied
	err := e.transact(ctx, func(tx *gorm.DB) error {
		o := Outcome{
			WorkspaceID: prepared.WorkspaceID, PaymentID: prepared.PaymentID, AttemptID: prepared.AttemptID,
			ProviderReference: result.ProviderReference, Code: result.Code, ActorReference: DispatchActor,
			Now: e.opts.Now().UnixMilli(), Claim: &claim,
		}
		var err error
		switch result.Outcome {
		case provider.OutcomeSucceeded:
			applied, err = ApplySuccess(tx, o)
		case provider.OutcomeDeclined:
			applied, err = ApplyDecline(tx, o)
		default:
			applied, err = ApplyUnknown(tx, o)
		}
		return err
	})
	if errors.Is(err, ErrLateOutcome) {
		e.opts.Metrics.lateOutcomes.Add(1)
	}
	if err != nil {
		e.opts.Logger.Warn("dispatch outcome not recorded", "payment_id", prepared.PaymentID, "attempt_id", prepared.AttemptID, "outcome", string(result.Outcome), "error", err.Error())
		return err
	}
	outcome := string(result.Outcome)
	e.opts.Metrics.observeOutcome(prepared.ProviderID, outcome)
	if result.Outcome != provider.OutcomeSucceeded && result.Outcome != provider.OutcomeDeclined {
		e.opts.Metrics.unknownOutcomes.Add(1)
	}
	if applied.Attempt.CompletedAt != nil && applied.Attempt.SubmittedAt != nil {
		e.opts.Metrics.observeAttemptLatency(*applied.Attempt.CompletedAt - *applied.Attempt.SubmittedAt)
	}
	return nil
}
