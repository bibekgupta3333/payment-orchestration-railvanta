package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/provider"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// RecoveryActor is the audit actor for transitions made by the lookup worker
// and the startup scan.
const RecoveryActor = "system:recovery"

// StaleSubmittedCode is the attempt error code for a submitted attempt found
// without a live dispatch lease (crash or lost lease). It is treated as
// possibly executed.
const StaleSubmittedCode = "stale_submitted"

// RecoveryHooks are test-only fault points. A returned error aborts Handle at
// that point without recording anything (simulating a crash).
type RecoveryHooks struct {
	// BeforeResolve runs after the original provider gave a definitive answer
	// and before the resolving transaction.
	BeforeResolve func(ctx context.Context, command LookupCommand, result provider.Result) error
}

// RecoveryOptions configures a Recovery worker. Zero values select defaults.
type RecoveryOptions struct {
	// Providers maps provider codes to adapters (the executor's set).
	Providers ProviderSet
	// LookupTimeout bounds one Lookup or same-key replay call (default 10s).
	LookupTimeout time.Duration
	// PersistTimeout bounds each resolving transaction (default 10s).
	PersistTimeout time.Duration
	// BaseBackoff and MaxBackoff bound the jittered exponential delay between
	// lookups while the outcome stays unknown (defaults 1s and 5m).
	BaseBackoff, MaxBackoff time.Duration
	// LookupDelay postpones the first lookup the startup scan schedules.
	LookupDelay time.Duration
	Now         func() time.Time
	Metrics     *RecoveryMetrics
	Logger      *slog.Logger
	Hooks       RecoveryHooks
}

// Recovery resolves unknown attempts (WBS 2.4). It handles payment.lookup
// commands by asking the ORIGINAL provider about the attempt's stored provider
// idempotency key, outside any transaction, and never creates an attempt.
type Recovery struct {
	db   *gorm.DB
	opts RecoveryOptions
}

// NewRecovery creates a recovery worker over the store's root handle.
func NewRecovery(db *gorm.DB, opts RecoveryOptions) *Recovery {
	if opts.LookupTimeout <= 0 {
		opts.LookupTimeout = 10 * time.Second
	}
	if opts.PersistTimeout <= 0 {
		opts.PersistTimeout = 10 * time.Second
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Minute
	}
	if opts.MaxBackoff < opts.BaseBackoff {
		opts.MaxBackoff = opts.BaseBackoff
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Metrics == nil {
		opts.Metrics = NewRecoveryMetrics()
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if opts.Providers == nil {
		opts.Providers = ProviderSet{}
	}
	return &Recovery{db: db, opts: opts}
}

// Metrics returns the worker's in-process counters.
func (r *Recovery) Metrics() *RecoveryMetrics { return r.opts.Metrics }

// Register binds the worker to the dispatcher's payment.lookup command.
func (r *Recovery) Register(d *event.Dispatcher) { d.Register(OutboxPaymentLookup, r.Handle) }

// errStillUnknown marks answers that leave the outcome unknown.
var errStillUnknown = errors.New("outcome still unknown at the original provider")

// Handle is the event.Handler for payment.lookup (2.4.1, 2.4.2, 2.4.4).
//
//   - The provider confirms success: unknown -> succeeded with the journal, in
//     one transaction that also completes the command (ApplySuccess).
//   - The provider proves no effect (a recorded decline): unknown -> failed.
//   - The provider cannot answer, reports the charge as still pending, or has
//     no record and cannot replay: the payment stays unknown and the command is
//     retried with backoff, without a MaxAttempts limit. "No record" alone is
//     never treated as proof of no effect.
//   - The provider has no record and declares an idempotent-replay contract:
//     the attempt is resubmitted to the SAME provider with the SAME key; its
//     definitive answer resolves the payment as above.
func (r *Recovery) Handle(ctx context.Context, claim event.Claim) error {
	var command LookupCommand
	if err := json.Unmarshal([]byte(claim.Payload), &command); err != nil || command.PaymentID == "" || command.AttemptID == "" ||
		command.ProviderID == "" || command.WorkspaceID != claim.WorkspaceID {
		return fmt.Errorf("invalid %s payload for event %s", OutboxPaymentLookup, claim.EventID)
	}
	db := r.db.WithContext(ctx)
	closed := false // the attempt was already resolved as failed
	payment, err := storage.GetPayment(db, command.WorkspaceID, command.PaymentID)
	if err != nil {
		return err
	}
	attempt, err := storage.GetAttempt(db, command.WorkspaceID, command.AttemptID)
	if err != nil {
		return err
	}
	switch {
	case attempt.PaymentID != payment.ID:
		return fmt.Errorf("lookup attempt %s does not belong to payment %s", attempt.ID, payment.ID)
	case attempt.ProviderID != command.ProviderID:
		// Never ask (or replay at) any provider but the attempt's own.
		return fmt.Errorf("%w: lookup names provider %s but attempt %s ran at %s", ErrUnsafeFailover, command.ProviderID, attempt.ID, attempt.ProviderID)
	case attempt.Status == AttemptSucceeded || Status(payment.Status).Terminal() && attempt.Status != AttemptFailed:
		// Already resolved (for example by a redelivered dispatch result).
		r.opts.Metrics.observeResolution("already_resolved", -1)
		return nil
	case attempt.Status == AttemptFailed:
		// Resolved as failed. Ask the provider once more: a recorded success
		// would be a contradiction that must reach reconciliation (late
		// outcome). If the provider cannot answer, there is nothing to resolve.
		closed = true
	case attempt.Status != AttemptUnknown || payment.Status != string(StatusUnknown):
		return event.RetryAfterUnbounded(r.backoff(claim.Attempts), fmt.Errorf("attempt %s is %s and payment is %s; lookup waits for unknown", attempt.ID, attempt.Status, payment.Status))
	}
	providerRow, err := storage.FindProvider(db, command.WorkspaceID, attempt.ProviderID)
	if err != nil {
		return err
	}
	adapter := r.opts.Providers[providerRow.Code]
	if adapter == nil {
		r.opts.Metrics.stillUnknown.Add(1)
		return event.RetryAfterUnbounded(r.backoff(claim.Attempts), fmt.Errorf("%w: no adapter for provider code %q", errStillUnknown, providerRow.Code))
	}
	result, err := r.ask(ctx, adapter, payment, attempt, closed)
	if err != nil && closed {
		r.opts.Metrics.observeResolution("already_resolved", -1)
		return nil
	}
	if err != nil {
		r.opts.Metrics.stillUnknown.Add(1)
		r.opts.Logger.Info("payment outcome still unknown", "payment_id", payment.ID, "attempt_id", attempt.ID, "lookups", claim.Attempts, "error", err.Error())
		return event.RetryAfterUnbounded(r.backoff(claim.Attempts), err)
	}
	if hook := r.opts.Hooks.BeforeResolve; hook != nil {
		if err := hook(ctx, command, result); err != nil {
			return err
		}
	}
	return r.resolve(ctx, claim, command, attempt, result)
}

// ask queries the original provider with the stored key, outside any
// transaction, and returns a definitive (succeeded or declined) result or an
// error meaning the outcome is still unknown.
func (r *Recovery) ask(ctx context.Context, adapter provider.Provider, payment storage.Payment, attempt storage.PaymentAttempt, lookupOnly bool) (provider.Result, error) {
	capabilities := adapter.Capabilities()
	if capabilities.LookupByIdempotencyKey {
		r.opts.Metrics.lookups.Add(1)
		callCtx, cancel := context.WithTimeout(ctx, r.opts.LookupTimeout)
		found, err := adapter.Lookup(callCtx, provider.LookupRequest{
			WorkspaceID: attempt.WorkspaceID, ProviderID: attempt.ProviderID, IdempotencyKey: attempt.ProviderIdempotencyKey,
		})
		cancel()
		if err != nil {
			r.opts.Metrics.lookupUnavailable.Add(1)
			return provider.Result{}, fmt.Errorf("%w: lookup failed: %w", errStillUnknown, err)
		}
		if found.Found {
			if definitive(found.Result) {
				return found.Result, nil
			}
			return provider.Result{}, fmt.Errorf("%w: provider reports %s (%s)", errStillUnknown, found.Result.Outcome, found.Result.Code)
		}
	}
	if lookupOnly {
		// An already-failed attempt is only audited, never resubmitted.
		return provider.Result{}, fmt.Errorf("%w: no provider record for a failed attempt", errStillUnknown)
	}
	if !capabilities.IdempotentReplay {
		return provider.Result{}, fmt.Errorf("%w: provider has no record and declares no idempotent replay", errStillUnknown)
	}
	// 2.4.4: same provider, same key; the provider returns the original charge
	// or executes at most once.
	token, err := storage.FindMethodToken(r.db.WithContext(ctx), payment.WorkspaceID, payment.PaymentMethodID, attempt.ProviderID)
	if err != nil {
		return provider.Result{}, fmt.Errorf("%w: token for replay: %w", errStillUnknown, err)
	}
	r.opts.Metrics.replays.Add(1)
	callCtx, cancel := context.WithTimeout(ctx, r.opts.LookupTimeout)
	result := adapter.Submit(callCtx, provider.SubmitRequest{
		WorkspaceID: attempt.WorkspaceID, ProviderID: attempt.ProviderID, IdempotencyKey: attempt.ProviderIdempotencyKey,
		AmountMinor: payment.AmountMinor, Currency: payment.Currency, TokenReference: token.TokenReference,
	})
	cancel()
	if definitive(result) {
		return result, nil
	}
	return provider.Result{}, fmt.Errorf("%w: same-key replay was %s (%s)", errStillUnknown, result.Outcome, result.Code)
}

func definitive(result provider.Result) bool {
	return result.Outcome == provider.OutcomeSucceeded || result.Outcome == provider.OutcomeDeclined
}

// resolve records the definitive answer and completes the command in one
// transaction. A result that conflicts with a terminal state is not applied;
// it is recorded as a payment.late_outcome for reconciliation instead.
func (r *Recovery) resolve(ctx context.Context, claim event.Claim, command LookupCommand, attempt storage.PaymentAttempt, result provider.Result) error {
	since := r.unknownSince(ctx, command)
	now := r.opts.Now().UnixMilli()
	err := r.transact(ctx, func(tx *gorm.DB) error {
		o := Outcome{
			WorkspaceID: command.WorkspaceID, PaymentID: command.PaymentID, AttemptID: command.AttemptID,
			ProviderReference: result.ProviderReference, Code: result.Code, ActorReference: RecoveryActor, Now: now, Claim: &claim,
		}
		var err error
		if result.Outcome == provider.OutcomeSucceeded {
			o.Reason = "original provider confirmed success for the stored provider key"
			_, err = ApplySuccess(tx, o)
		} else {
			o.Reason = "original provider proved no effect (declined) for the stored provider key"
			_, err = ApplyDecline(tx, o)
		}
		return err
	})
	if errors.Is(err, ErrLateOutcome) {
		r.opts.Metrics.observeResolution("late_outcome", -1)
		return r.transact(ctx, func(tx *gorm.DB) error {
			_, err := RecordLateOutcome(tx, LateOutcome{
				WorkspaceID: command.WorkspaceID, PaymentID: command.PaymentID, AttemptID: attempt.ID, ProviderID: attempt.ProviderID,
				Outcome: string(result.Outcome), ProviderReference: result.ProviderReference, Code: result.Code, Source: "lookup", ObservedAt: now,
			}, &claim)
			return err
		})
	}
	if err != nil {
		return err
	}
	age := int64(-1)
	if since > 0 {
		age = now - since
	}
	r.opts.Metrics.observeResolution(string(result.Outcome), age)
	return nil
}

// unknownSince is when the payment last became unknown (0 if never).
func (r *Recovery) unknownSince(ctx context.Context, command LookupCommand) int64 {
	var since *int64
	_ = r.db.WithContext(ctx).Model(&storage.PaymentStateTransition{}).
		Where("workspace_id = ? AND payment_id = ? AND to_status = ?", command.WorkspaceID, command.PaymentID, string(StatusUnknown)).
		Select("max(created_at)").Scan(&since).Error
	if since == nil {
		return 0
	}
	return *since
}

// RecoverStale is the startup scan (2.4.3). Run it after migrations and before
// the dispatcher starts. Every attempt still submitted whose payment.dispatch
// command has no live lease is treated as possibly executed: attempt and
// payment move to unknown (error_code stale_submitted) and a lookup at the
// attempt's own provider is scheduled. Nothing is resubmitted here.
func (r *Recovery) RecoverStale(ctx context.Context) (int, error) {
	now := r.opts.Now().UnixMilli()
	var stale []storage.PaymentAttempt
	err := r.db.WithContext(ctx).Raw(`SELECT a.* FROM payment_attempts a
WHERE a.status = ? AND NOT EXISTS (
  SELECT 1 FROM outbox_events o
  WHERE o.workspace_id = a.workspace_id AND o.deduplication_key = ? || a.payment_id
    AND o.status = ? AND o.lease_until >= ?)
ORDER BY a.created_at, a.id`, AttemptSubmitted, OutboxPaymentDispatch+":", event.StatusProcessing, now).Scan(&stale).Error
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, attempt := range stale {
		var applied Applied
		err := r.transact(ctx, func(tx *gorm.DB) error {
			var err error
			applied, err = ApplyUnknown(tx, Outcome{
				WorkspaceID: attempt.WorkspaceID, PaymentID: attempt.PaymentID, AttemptID: attempt.ID, Code: StaleSubmittedCode,
				Reason:         "startup scan: attempt still submitted with no live dispatch lease; treated as possibly executed, lookup scheduled at the original provider",
				ActorReference: RecoveryActor, Now: now, LookupDelay: r.opts.LookupDelay,
			})
			return err
		})
		if err != nil {
			r.opts.Logger.Error("stale attempt not recovered", "payment_id", attempt.PaymentID, "attempt_id", attempt.ID, "error", err.Error())
			continue
		}
		if applied.Changed {
			recovered++
			r.opts.Metrics.staleRecovered.Add(1)
		}
	}
	return recovered, nil
}

func (r *Recovery) transact(ctx context.Context, fn func(tx *gorm.DB) error) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.opts.PersistTimeout)
	defer cancel()
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = r.db.WithContext(persistCtx).Transaction(fn)
		if !storage.IsBusy(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return err
}

// backoff is jittered exponential: uniform in [ceiling/2, ceiling], with
// ceiling = min(MaxBackoff, BaseBackoff * 2^(lookups-1)).
func (r *Recovery) backoff(lookups int64) time.Duration {
	ceiling := r.opts.BaseBackoff << min(max(lookups-1, 0), 16)
	if ceiling > r.opts.MaxBackoff || ceiling <= 0 {
		ceiling = r.opts.MaxBackoff
	}
	half := ceiling / 2
	return half + time.Duration(rand.Int64N(int64(ceiling-half)+1))
}

// OutboxPaymentLateOutcome records a provider result that arrived after the
// payment or attempt already reached a conflicting state (2.4.7). It is never
// applied. The row stays pending in outbox_events, durable and
// reconciliation-visible, until the WBS 4.2 reconciliation consumer claims it;
// GET /v1/payments/{id}/recovery lists it.
const OutboxPaymentLateOutcome = "payment.late_outcome"

// LateOutcome is the payload of a payment.late_outcome row.
type LateOutcome struct {
	PaymentID         string `json:"payment_id"`
	WorkspaceID       string `json:"workspace_id"`
	AttemptID         string `json:"attempt_id"`
	ProviderID        string `json:"provider_id"`
	Outcome           string `json:"outcome"`
	ProviderReference string `json:"provider_reference,omitempty"`
	Code              string `json:"code,omitempty"`
	// PaymentStatus and AttemptStatus are the recorded states it conflicted with.
	PaymentStatus string `json:"payment_status"`
	AttemptStatus string `json:"attempt_status"`
	// Source is "dispatch" or "lookup".
	Source     string `json:"source"`
	ObservedAt int64  `json:"observed_at"`
}

// LateOutcomeDeduplicationKey records one row per attempt and outcome, so a
// repeated identical late result is recorded once.
func LateOutcomeDeduplicationKey(attemptID, outcome string) string {
	return OutboxPaymentLateOutcome + ":" + attemptID + ":" + outcome
}

// RecordLateOutcome appends the late result on the caller's transaction,
// filling in the recorded payment and attempt states, and completes claim (if
// any) so the result is not redelivered forever. It reports whether a new row
// was written.
func RecordLateOutcome(tx *gorm.DB, late LateOutcome, claim *event.Claim) (bool, error) {
	payment, err := storage.GetPayment(tx, late.WorkspaceID, late.PaymentID)
	if err != nil {
		return false, err
	}
	attempt, err := storage.GetAttempt(tx, late.WorkspaceID, late.AttemptID)
	if err != nil {
		return false, err
	}
	late.PaymentStatus, late.AttemptStatus = payment.Status, attempt.Status
	payload, err := json.Marshal(late)
	if err != nil {
		return false, err
	}
	paymentID := payment.ID
	inserted, err := event.Enqueue(tx, storage.OutboxEvent{
		WorkspaceID: late.WorkspaceID, PaymentID: &paymentID, EventType: OutboxPaymentLateOutcome,
		DeduplicationKey: LateOutcomeDeduplicationKey(attempt.ID, late.Outcome), Payload: string(payload),
		AvailableAt: late.ObservedAt, CreatedAt: late.ObservedAt,
	})
	if err != nil {
		return false, err
	}
	if claim != nil {
		if err := event.Complete(tx, *claim, late.ObservedAt); err != nil {
			return false, err
		}
	}
	return inserted, nil
}
