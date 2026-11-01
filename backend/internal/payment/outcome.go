package payment

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/ledger"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Attempt statuses (payment_attempts.status).
const (
	AttemptCreated   = "created"
	AttemptSubmitted = "submitted"
	AttemptSucceeded = "succeeded"
	AttemptFailed    = "failed"
	AttemptUnknown   = "unknown"
)

// OutboxPaymentLookup is the command scheduled when a provider outcome is
// ambiguous (or an attempt was found stale in submitted). Its handler (WBS 2.4)
// must call Lookup on the SAME provider (LookupCommand.ProviderID) with the
// attempt's stored provider_idempotency_key, outside any transaction, and then
// resolve with ApplySuccess / ApplyDecline passing its claim. It must never
// create an attempt at another provider.
const OutboxPaymentLookup = "payment.lookup"

// LookupCommand is the JSON payload of a payment.lookup outbox row. The
// provider idempotency key is not copied into the payload: read it from the
// attempt row (payment_attempts.provider_idempotency_key).
type LookupCommand struct {
	PaymentID   string `json:"payment_id"`
	WorkspaceID string `json:"workspace_id"`
	AttemptID   string `json:"attempt_id"`
	ProviderID  string `json:"provider_id"`
	// Reason is why the outcome is unknown (a provider result code or "stale_submitted").
	Reason string `json:"reason"`
}

// LookupDeduplicationKey is unique per attempt: one lookup command per
// unresolved attempt. Retries reuse the row via event.Release / RetryAfter.
func LookupDeduplicationKey(attemptID string) string { return OutboxPaymentLookup + ":" + attemptID }

var (
	// ErrLateOutcome means a provider result arrived for a payment or attempt
	// that already reached a conflicting state (for example a success after
	// failed). Nothing was applied; WBS 2.4.7 records it for reconciliation.
	ErrLateOutcome = errors.New("provider outcome conflicts with recorded state")
	// ErrUnsafeFailover means a new attempt would bypass an unresolved or
	// previously ambiguous attempt. Ambiguous outcomes are only ever resolved
	// against the original provider.
	ErrUnsafeFailover = errors.New("new attempt blocked: an unresolved or ambiguous attempt must be resolved at its original provider")
)

// Outcome identifies a provider result to record. Claim, when set, is the
// outbox command being processed; it is completed in the same transaction and
// an event.ErrLeaseLost from it rolls everything back.
type Outcome struct {
	WorkspaceID       string
	PaymentID         string
	AttemptID         string
	ProviderReference string
	// Code is the provider result code (decline or ambiguity reason).
	Code           string
	Reason         string
	ActorReference string
	Now            int64 // UTC Unix milliseconds
	Claim          *event.Claim
	// LookupDelay postpones the first payment.lookup that ApplyUnknown schedules.
	LookupDelay time.Duration
}

// Applied reports what an Apply* call changed.
type Applied struct {
	// Changed is false when the outcome was already recorded (idempotent repeat).
	Changed bool
	// From is the payment status before the call.
	From Status
	// Posting is the success journal (ApplySuccess only).
	Posting *ledger.Posting
	// Attempt is the attempt after the call.
	Attempt storage.PaymentAttempt
}

func (o Outcome) load(tx *gorm.DB) (storage.Payment, storage.PaymentAttempt, error) {
	if o.WorkspaceID == "" || o.PaymentID == "" || o.AttemptID == "" || o.ActorReference == "" || o.Now <= 0 {
		return storage.Payment{}, storage.PaymentAttempt{}, errors.New("outcome requires workspace, payment, attempt, actor and time")
	}
	payment, err := storage.GetPayment(tx, o.WorkspaceID, o.PaymentID)
	if err != nil {
		return payment, storage.PaymentAttempt{}, err
	}
	attempt, err := storage.GetAttempt(tx, o.WorkspaceID, o.AttemptID)
	if err != nil {
		return payment, attempt, err
	}
	if attempt.PaymentID != payment.ID {
		return payment, attempt, fmt.Errorf("attempt %s does not belong to payment %s", attempt.ID, payment.ID)
	}
	return payment, attempt, nil
}

func (o Outcome) complete(tx *gorm.DB) error {
	if o.Claim == nil {
		return nil
	}
	return event.Complete(tx, *o.Claim, o.Now)
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func reason(o Outcome, fallback string) string {
	if o.Reason != "" {
		return o.Reason
	}
	return fallback
}

// ApplySuccess is the WBS 2.3.5 unit of work, on the caller's transaction:
// payment processing|unknown -> succeeded (version-guarded, with audit row),
// attempt submitted|unknown -> succeeded with provider reference and
// completed_at, the balanced success journal (ledger.PostPaymentSuccess, debit
// the attempt provider's clearing account, credit merchant payable), and the
// outbox completion. Repeating it for an already-succeeded payment writes
// nothing except the claim completion and reports Changed=false; the posting
// key guarantees one journal. A success for a failed/canceled payment returns
// ErrLateOutcome.
func ApplySuccess(tx *gorm.DB, o Outcome) (Applied, error) {
	payment, attempt, err := o.load(tx)
	if err != nil {
		return Applied{}, err
	}
	applied := Applied{From: Status(payment.Status), Attempt: attempt}
	switch {
	case payment.Status == string(StatusSucceeded) && attempt.Status == AttemptSucceeded:
		posting, err := ledger.PostPaymentSuccess(tx, o.WorkspaceID, payment.ID, attempt.ProviderID, payment.Currency, payment.AmountMinor)
		if err != nil {
			return applied, err
		}
		applied.Posting = posting
		return applied, o.complete(tx)
	case (payment.Status == string(StatusProcessing) || payment.Status == string(StatusUnknown)) &&
		(attempt.Status == AttemptSubmitted || attempt.Status == AttemptUnknown):
	default:
		return applied, fmt.Errorf("%w: success for payment %s (%s) attempt %s (%s)", ErrLateOutcome, payment.ID, payment.Status, attempt.ID, attempt.Status)
	}
	now := o.Now
	if err := storage.UpdateAttemptStatus(tx, storage.AttemptUpdate{
		WorkspaceID: o.WorkspaceID, AttemptID: attempt.ID, From: []string{AttemptSubmitted, AttemptUnknown}, To: AttemptSucceeded,
		ProviderReference: optional(o.ProviderReference), CompletedAt: &now, Now: now,
	}); err != nil {
		return applied, err
	}
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: o.WorkspaceID, PaymentID: payment.ID, From: Status(payment.Status), To: StatusSucceeded, Version: payment.Version,
		Reason: reason(o, "provider confirmed success"), ActorReference: o.ActorReference, Now: now,
	}); err != nil {
		return applied, err
	}
	posting, err := ledger.PostPaymentSuccess(tx, o.WorkspaceID, payment.ID, attempt.ProviderID, payment.Currency, payment.AmountMinor)
	if err != nil {
		return applied, err
	}
	if err := o.complete(tx); err != nil {
		return applied, err
	}
	applied.Changed, applied.Posting = true, posting
	applied.Attempt, err = storage.GetAttempt(tx, o.WorkspaceID, attempt.ID)
	return applied, err
}

// ApplyDecline records proof of no effect: attempt submitted|unknown -> failed
// with the result code and completed_at, payment processing|unknown -> failed.
// A repeat for an already-failed payment and attempt is a no-op. A decline for
// a succeeded payment returns ErrLateOutcome.
func ApplyDecline(tx *gorm.DB, o Outcome) (Applied, error) {
	payment, attempt, err := o.load(tx)
	if err != nil {
		return Applied{}, err
	}
	applied := Applied{From: Status(payment.Status), Attempt: attempt}
	switch {
	case payment.Status == string(StatusFailed) && attempt.Status == AttemptFailed:
		return applied, o.complete(tx)
	case (payment.Status == string(StatusProcessing) || payment.Status == string(StatusUnknown)) &&
		(attempt.Status == AttemptSubmitted || attempt.Status == AttemptUnknown):
	default:
		return applied, fmt.Errorf("%w: decline for payment %s (%s) attempt %s (%s)", ErrLateOutcome, payment.ID, payment.Status, attempt.ID, attempt.Status)
	}
	now := o.Now
	if err := storage.UpdateAttemptStatus(tx, storage.AttemptUpdate{
		WorkspaceID: o.WorkspaceID, AttemptID: attempt.ID, From: []string{AttemptSubmitted, AttemptUnknown}, To: AttemptFailed,
		ProviderReference: optional(o.ProviderReference), ErrorCode: optional(o.Code), CompletedAt: &now, Now: now,
	}); err != nil {
		return applied, err
	}
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: o.WorkspaceID, PaymentID: payment.ID, From: Status(payment.Status), To: StatusFailed, Version: payment.Version,
		Reason: reason(o, "provider declined with no effect"), ActorReference: o.ActorReference, Now: now,
	}); err != nil {
		return applied, err
	}
	if err := o.complete(tx); err != nil {
		return applied, err
	}
	applied.Changed = true
	applied.Attempt, err = storage.GetAttempt(tx, o.WorkspaceID, attempt.ID)
	return applied, err
}

// ApplyUnknown records a possibly-executed attempt: attempt submitted -> unknown
// with the ambiguity code (completed_at stays NULL until resolution), payment
// processing -> unknown, and a payment.lookup command for the SAME provider and
// attempt. Repeating it for an already-unknown attempt only ensures the lookup
// command exists. For a terminal payment it changes nothing.
func ApplyUnknown(tx *gorm.DB, o Outcome) (Applied, error) {
	payment, attempt, err := o.load(tx)
	if err != nil {
		return Applied{}, err
	}
	applied := Applied{From: Status(payment.Status), Attempt: attempt}
	switch {
	case Status(payment.Status).Terminal():
		return applied, o.complete(tx)
	case payment.Status == string(StatusUnknown) && attempt.Status == AttemptUnknown:
		if err := scheduleLookup(tx, o, attempt); err != nil {
			return applied, err
		}
		return applied, o.complete(tx)
	case payment.Status == string(StatusProcessing) && attempt.Status == AttemptSubmitted:
	default:
		return applied, fmt.Errorf("%w: unknown outcome for payment %s (%s) attempt %s (%s)", ErrLateOutcome, payment.ID, payment.Status, attempt.ID, attempt.Status)
	}
	now := o.Now
	if err := storage.UpdateAttemptStatus(tx, storage.AttemptUpdate{
		WorkspaceID: o.WorkspaceID, AttemptID: attempt.ID, From: []string{AttemptSubmitted}, To: AttemptUnknown,
		ProviderReference: optional(o.ProviderReference), ErrorCode: optional(o.Code), Now: now,
	}); err != nil {
		return applied, err
	}
	if err := ApplyTransition(tx, Transition{
		WorkspaceID: o.WorkspaceID, PaymentID: payment.ID, From: StatusProcessing, To: StatusUnknown, Version: payment.Version,
		Reason: reason(o, "provider outcome unknown; lookup scheduled at the original provider"), ActorReference: o.ActorReference, Now: now,
	}); err != nil {
		return applied, err
	}
	if err := scheduleLookup(tx, o, attempt); err != nil {
		return applied, err
	}
	if err := o.complete(tx); err != nil {
		return applied, err
	}
	applied.Changed = true
	applied.Attempt, err = storage.GetAttempt(tx, o.WorkspaceID, attempt.ID)
	return applied, err
}

func scheduleLookup(tx *gorm.DB, o Outcome, attempt storage.PaymentAttempt) error {
	code := o.Code
	if code == "" {
		code = "unknown"
	}
	payload, err := json.Marshal(LookupCommand{PaymentID: attempt.PaymentID, WorkspaceID: o.WorkspaceID, AttemptID: attempt.ID, ProviderID: attempt.ProviderID, Reason: code})
	if err != nil {
		return err
	}
	paymentID := attempt.PaymentID
	_, err = event.Enqueue(tx, storage.OutboxEvent{
		WorkspaceID: o.WorkspaceID, PaymentID: &paymentID, EventType: OutboxPaymentLookup,
		DeduplicationKey: LookupDeduplicationKey(attempt.ID), Payload: string(payload), AvailableAt: o.Now + max(o.LookupDelay.Milliseconds(), 0), CreatedAt: o.Now,
	})
	return err
}

// GuardNewAttempt is the 2.2.11 failover guard, checked on the caller's
// transaction before any attempt insert. It rejects a new attempt when the
// payment has any unresolved attempt (created, submitted or unknown), or when
// the payment was ever unknown and providerID differs from a provider already
// attempted. Ambiguous outcomes are resolved by lookup at the original
// provider, never by submitting elsewhere.
func GuardNewAttempt(tx *gorm.DB, workspaceID, paymentID, providerID string) error {
	attempts, err := storage.ListPaymentAttempts(tx, workspaceID, paymentID)
	if err != nil {
		return err
	}
	for _, a := range attempts {
		switch a.Status {
		case AttemptCreated, AttemptSubmitted, AttemptUnknown:
			return fmt.Errorf("%w: attempt %s at provider %s is %s", ErrUnsafeFailover, a.ID, a.ProviderID, a.Status)
		}
	}
	var everUnknown int64
	if err := tx.Model(&storage.PaymentStateTransition{}).
		Where("workspace_id = ? AND payment_id = ? AND to_status = ?", workspaceID, paymentID, string(StatusUnknown)).
		Count(&everUnknown).Error; err != nil {
		return err
	}
	if everUnknown > 0 {
		for _, a := range attempts {
			if a.ProviderID != providerID {
				return fmt.Errorf("%w: payment was unknown after an attempt at provider %s", ErrUnsafeFailover, a.ProviderID)
			}
		}
	}
	return nil
}
