package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/railvanta/railvanta/backend/internal/event"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// ErrNotCancelable means the payment left created (dispatch claimed it, or it
// reached another state) before cancellation could win.
var ErrNotCancelable = errors.New("payment can no longer be canceled")

// CancelRequest cancels one workspace payment. WorkspaceID and ActorReference
// come from the authenticated principal.
type CancelRequest struct {
	WorkspaceID    string
	PaymentID      string
	ActorReference string
}

// CancelResult is the payment after the call.
type CancelResult struct {
	Payment storage.Payment
	// Changed is false when the payment was already canceled.
	Changed bool
}

// Cancel applies created -> canceled (2.4.5) as a compare-and-swap on status
// and version with its audit row, and in the same transaction marks a still
// pending payment.dispatch command delivered so no dispatcher claims it. It
// races safely with dispatch: both sides compare-and-swap the same (created,
// version) row, so exactly one wins. When dispatch won, or the payment is in
// any state but created or canceled, it returns ErrNotCancelable with the
// current payment. Cancelling an already canceled payment changes nothing.
func (s *Service) Cancel(ctx context.Context, request CancelRequest) (CancelResult, error) {
	if request.WorkspaceID == "" || request.PaymentID == "" || request.ActorReference == "" {
		return CancelResult{}, fmt.Errorf("%w: cancel requires workspace, payment and actor", ErrInvalidRequest)
	}
	busy := 0
	for stale := 0; ; {
		result, err := s.cancelOnce(ctx, request)
		switch {
		case errors.Is(err, storage.ErrStaleVersion) && stale < 3:
			// Another writer changed the payment after we read it; re-read.
			stale++
			continue
		case storage.IsBusy(err):
			busy++
			if busy >= s.options.MaxBusyAttempts {
				return CancelResult{}, fmt.Errorf("%w: %d attempts", ErrBusy, busy)
			}
			if err := s.sleep(ctx, busy); err != nil {
				return CancelResult{}, err
			}
			continue
		}
		return result, err
	}
}

func (s *Service) cancelOnce(ctx context.Context, request CancelRequest) (CancelResult, error) {
	var result CancelResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := storage.GetPayment(tx, request.WorkspaceID, request.PaymentID)
		if err != nil {
			return err
		}
		result.Payment = current
		switch Status(current.Status) {
		case StatusCanceled:
			return nil
		case StatusCreated:
		default:
			return fmt.Errorf("%w: payment is %s", ErrNotCancelable, current.Status)
		}
		now := s.options.Now().UnixMilli()
		if err := ApplyTransition(tx, Transition{
			WorkspaceID: request.WorkspaceID, PaymentID: current.ID, From: StatusCreated, To: StatusCanceled, Version: current.Version,
			Reason: "canceled before dispatch", ActorReference: request.ActorReference, Now: now,
		}); err != nil {
			return err
		}
		if _, err := event.SkipPending(tx, request.WorkspaceID, DispatchDeduplicationKey(current.ID), "payment canceled before dispatch", now); err != nil {
			return err
		}
		result.Payment, err = storage.GetPayment(tx, request.WorkspaceID, current.ID)
		result.Changed = true
		return err
	})
	return result, err
}

// RecoveryView is the API view of a payment's recovery state.
type RecoveryView struct {
	PaymentID string `json:"payment_id"`
	// LookupStatus is none, pending (a lookup is scheduled or backing off),
	// in_progress, resolved, or failed (parked; needs an operator).
	LookupStatus string `json:"lookup_status"`
	// LookupAttempts counts lookup acquisitions so far.
	LookupAttempts int64 `json:"lookup_attempts"`
	// NextLookupAt is when the next lookup is due while pending.
	NextLookupAt *string `json:"next_lookup_at"`
	LastError    *string `json:"last_lookup_error"`
	// RetryAllowed is always false: ambiguous outcomes are resolved by lookup
	// at the original provider, never by a new attempt.
	RetryAllowed bool              `json:"retry_allowed"`
	LateOutcomes []LateOutcomeView `json:"late_outcomes"`
}

// LateOutcomeView is a provider result rejected because it conflicted with a
// recorded terminal state; it is kept for reconciliation, never applied.
type LateOutcomeView struct {
	AttemptID         string  `json:"attempt_id"`
	Outcome           string  `json:"outcome"`
	ProviderReference *string `json:"provider_reference"`
	Code              *string `json:"code"`
	PaymentStatus     string  `json:"payment_status"`
	AttemptStatus     string  `json:"attempt_status"`
	Source            string  `json:"source"`
	ObservedAt        string  `json:"observed_at"`
}

// Recovery reads a payment's lookup and late-outcome records.
func (s *Service) Recovery(ctx context.Context, workspaceID, paymentID string) (RecoveryView, error) {
	db := s.db.WithContext(ctx)
	if _, err := storage.GetPayment(db, workspaceID, paymentID); err != nil {
		return RecoveryView{}, err
	}
	view := RecoveryView{PaymentID: paymentID, LookupStatus: "none", LateOutcomes: []LateOutcomeView{}}
	var rows []storage.OutboxEvent
	if err := db.Where("workspace_id = ? AND payment_id = ? AND event_type IN ?", workspaceID, paymentID, []string{OutboxPaymentLookup, OutboxPaymentLateOutcome}).
		Order("created_at, id").Find(&rows).Error; err != nil {
		return view, err
	}
	for _, row := range rows {
		if row.EventType == OutboxPaymentLookup {
			view.LookupAttempts += row.Attempts
			view.LastError = row.LastError
			switch row.Status {
			case event.StatusPending:
				view.LookupStatus = "pending"
				at := FormatTimestamp(row.AvailableAt)
				view.NextLookupAt = &at
			case event.StatusProcessing:
				view.LookupStatus = "in_progress"
			case event.StatusDelivered:
				view.LookupStatus = "resolved"
			default:
				view.LookupStatus = "failed"
			}
			continue
		}
		var late LateOutcome
		if err := json.Unmarshal([]byte(row.Payload), &late); err != nil {
			continue
		}
		item := LateOutcomeView{AttemptID: late.AttemptID, Outcome: late.Outcome, PaymentStatus: late.PaymentStatus, AttemptStatus: late.AttemptStatus,
			Source: late.Source, ObservedAt: FormatTimestamp(late.ObservedAt)}
		if late.ProviderReference != "" {
			item.ProviderReference = &late.ProviderReference
		}
		if late.Code != "" {
			item.Code = &late.Code
		}
		view.LateOutcomes = append(view.LateOutcomes, item)
	}
	return view, nil
}
