package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Webhook inbox (WBS 4.3). Every inbound provider event is stored once per
// (provider, provider event ID). A repeat delivery with the same payload hash
// only increments duplicate_count; a repeat with a different hash is flagged for
// investigation and never overwrites the first payload. Processing applies the
// event through the status-guarded Apply* functions, so an out-of-order event
// can never move a terminal payment backwards: it is recorded as ignored.

// Mock webhook event types accepted by the sandbox inbound route.
const (
	WebhookSucceeded  = "payment.succeeded"
	WebhookFailed     = "payment.failed"
	WebhookProcessing = "payment.processing"
)

// Inbox processing outcomes (webhook_inbox.outcome).
const (
	InboxApplied = "applied"
	InboxIgnored = "ignored"
)

// SignatureNotApplicable is the only signature status written for mock events,
// which carry no signature. verified/rejected belong to a future backend verifier.
const SignatureNotApplicable = "not_applicable"

// WebhookEvent is the validated inbound mock event.
type WebhookEvent struct {
	EventID           string `json:"event_id"`
	Type              string `json:"type"`
	PaymentID         string `json:"payment_id"`
	AttemptID         string `json:"attempt_id,omitempty"`
	ProviderReference string `json:"provider_reference,omitempty"`
}

// WebhookDelivery is one received request body for a provider in a workspace.
type WebhookDelivery struct {
	WorkspaceID string
	ProviderID  string
	Body        []byte
	Event       WebhookEvent
	Now         int64 // UTC Unix milliseconds
}

// WebhookResult reports what a delivery did.
type WebhookResult struct {
	Row       storage.WebhookInbox
	Duplicate bool // same payload hash as a stored event: counted, nothing applied
	Conflict  bool // same event ID, different payload hash: flagged, nothing applied
}

// PayloadHash is the lowercase hex SHA-256 of the raw request body.
func PayloadHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ReceiveWebhook stores and processes one delivery in a single transaction.
func ReceiveWebhook(ctx context.Context, db *gorm.DB, in WebhookDelivery) (WebhookResult, error) {
	var result WebhookResult
	hash := PayloadHash(in.Body)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result = WebhookResult{}
		id, err := storage.NewID()
		if err != nil {
			return err
		}
		row := storage.WebhookInbox{
			ID: id, WorkspaceID: in.WorkspaceID, ProviderID: in.ProviderID, ProviderEventID: in.Event.EventID,
			PayloadHash: hash, Payload: string(in.Body), Status: "pending", LastReceivedAt: in.Now, ReceivedAt: in.Now,
			EventType: in.Event.Type, SignatureStatus: SignatureNotApplicable,
		}
		insert := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider_id"}, {Name: "provider_event_id"}},
			DoNothing: true,
		}).Create(&row)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return recordRepeat(tx, in, hash, &result)
		}
		outcome, reason, err := applyWebhook(tx, in, &row)
		if err != nil {
			return err
		}
		update := map[string]any{
			"status": "processed", "processed_at": in.Now, "outcome": outcome, "outcome_reason": reason,
			"payment_id": row.PaymentID, "attempt_id": row.AttemptID,
		}
		if err := tx.Model(&storage.WebhookInbox{}).Where("id = ?", row.ID).Updates(update).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", row.ID).Take(&result.Row).Error
	})
	return result, err
}

// recordRepeat counts a repeat delivery or flags a changed payload.
func recordRepeat(tx *gorm.DB, in WebhookDelivery, hash string, result *WebhookResult) error {
	var existing storage.WebhookInbox
	if err := tx.Where("provider_id = ? AND provider_event_id = ?", in.ProviderID, in.Event.EventID).Take(&existing).Error; err != nil {
		return err
	}
	if existing.WorkspaceID != in.WorkspaceID {
		return errors.New("webhook inbox row belongs to another workspace")
	}
	if existing.PayloadHash == hash {
		if err := tx.Model(&storage.WebhookInbox{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"duplicate_count": gorm.Expr("duplicate_count + 1"), "last_received_at": gorm.Expr("max(last_received_at, ?)", in.Now),
		}).Error; err != nil {
			return err
		}
		result.Duplicate = true
	} else {
		if err := tx.Model(&storage.WebhookInbox{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"investigation_flag": 1, "conflict_count": gorm.Expr("conflict_count + 1"),
			"conflicting_payload_hash": hash, "last_received_at": gorm.Expr("max(last_received_at, ?)", in.Now),
		}).Error; err != nil {
			return err
		}
		result.Conflict = true
	}
	return tx.Where("id = ?", existing.ID).Take(&result.Row).Error
}

// applyWebhook runs the guarded transition for a new event. It writes nothing
// to the payment for events that do not match the recorded state.
func applyWebhook(tx *gorm.DB, in WebhookDelivery, row *storage.WebhookInbox) (outcome, reason string, err error) {
	ignored := func(why string) (string, string, error) { return InboxIgnored, why, nil }
	paymentRow, err := storage.GetPayment(tx, in.WorkspaceID, in.Event.PaymentID)
	if errors.Is(err, storage.ErrNotFound) {
		return ignored("unknown_payment")
	}
	if err != nil {
		return "", "", err
	}
	paymentID := paymentRow.ID
	row.PaymentID = &paymentID
	var attempt storage.PaymentAttempt
	if in.Event.AttemptID != "" {
		attempt, err = storage.GetAttempt(tx, in.WorkspaceID, in.Event.AttemptID)
		if err == nil && attempt.PaymentID != paymentRow.ID {
			return ignored("attempt_mismatch")
		}
	} else {
		err = tx.Where("workspace_id = ? AND payment_id = ?", in.WorkspaceID, paymentRow.ID).Order("attempt_number DESC").Take(&attempt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = storage.ErrNotFound
		}
	}
	if errors.Is(err, storage.ErrNotFound) {
		return ignored("no_attempt")
	}
	if err != nil {
		return "", "", err
	}
	attemptID := attempt.ID
	row.AttemptID = &attemptID
	if attempt.ProviderID != in.ProviderID {
		return ignored("provider_mismatch")
	}
	status := Status(paymentRow.Status)
	if in.Event.Type == WebhookProcessing {
		if status.Terminal() {
			return ignored("terminal_state")
		}
		return ignored("no_state_change")
	}
	o := Outcome{
		WorkspaceID: in.WorkspaceID, PaymentID: paymentRow.ID, AttemptID: attempt.ID, ProviderReference: in.Event.ProviderReference,
		ActorReference: "webhook:mock", Now: in.Now,
	}
	var applied Applied
	lateOutcome := "succeeded"
	if in.Event.Type == WebhookSucceeded {
		o.Reason = "mock webhook reported success"
		applied, err = ApplySuccess(tx, o)
	} else {
		lateOutcome = "declined"
		o.Reason, o.Code = "mock webhook reported failure", "webhook_failed"
		applied, err = ApplyDecline(tx, o)
	}
	switch {
	case errors.Is(err, ErrLateOutcome):
		if !status.Terminal() {
			return ignored("not_in_flight")
		}
		if lateOutcome == "succeeded" && status == StatusSucceeded || lateOutcome == "declined" && status == StatusFailed {
			return ignored("already_" + string(status))
		}
		// A conflicting result for a terminal payment is kept for reconciliation, never applied.
		if _, err := RecordLateOutcome(tx, LateOutcome{
			PaymentID: paymentRow.ID, WorkspaceID: in.WorkspaceID, AttemptID: attempt.ID, ProviderID: in.ProviderID,
			Outcome: lateOutcome, ProviderReference: in.Event.ProviderReference, Source: "webhook", ObservedAt: in.Now,
		}, nil); err != nil {
			return "", "", err
		}
		return ignored("terminal_state")
	case err != nil:
		return "", "", err
	case !applied.Changed:
		return ignored("already_" + string(status))
	}
	return InboxApplied, "state_changed", nil
}
