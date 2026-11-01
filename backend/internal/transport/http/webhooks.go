package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// POST /v1/sandbox/webhooks/mock (WBS 4.3) is a sandbox-only inbound route for
// the deterministic mock provider. It exists so duplicate, changed-payload and
// reordered deliveries can be driven over real HTTP. It is operator-only (the
// router already requires the operator role and a CSRF token for cookie
// sessions), workspace-scoped, and mock deliveries carry no signature, so they
// are recorded as "not applicable" and never as verified.

var providerEventID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func (h *handler) serveWebhooks(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/sandbox/webhooks/mock" {
		return false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, "validation_error", "Request body could not be read", id)
		return true
	}
	var event payment.WebhookEvent
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil || decoder.More() {
		fail(w, 400, "validation_error", "Body must be one JSON object with event_id, type, payment_id and optional attempt_id and provider_reference", id)
		return true
	}
	var fields []FieldError
	if !providerEventID.MatchString(event.EventID) {
		fields = append(fields, FieldError{Field: "event_id", Message: "Use 1 to 128 letters, digits, dot, colon, dash or underscore"})
	}
	switch event.Type {
	case payment.WebhookSucceeded, payment.WebhookFailed, payment.WebhookProcessing:
	default:
		fields = append(fields, FieldError{Field: "type", Message: "Use payment.succeeded, payment.failed or payment.processing"})
	}
	if !safeID.MatchString(event.PaymentID) {
		fields = append(fields, FieldError{Field: "payment_id", Message: "Use a payment ID"})
	}
	if event.AttemptID != "" && !safeID.MatchString(event.AttemptID) {
		fields = append(fields, FieldError{Field: "attempt_id", Message: "Use an attempt ID"})
	}
	if event.ProviderReference != "" && !providerEventID.MatchString(event.ProviderReference) {
		fields = append(fields, FieldError{Field: "provider_reference", Message: "Use up to 128 identifier characters"})
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid webhook event", id, fields...)
		return true
	}
	var provider storage.Provider
	if err := h.db.WithContext(r.Context()).Where("workspace_id = ? AND code = 'mock' AND enabled = 1", principal.WorkspaceID).Take(&provider).Error; err != nil {
		fail(w, 404, "not_found", "The mock provider is not enabled for this workspace", id)
		return true
	}
	result, err := payment.ReceiveWebhook(r.Context(), h.db, payment.WebhookDelivery{
		WorkspaceID: principal.WorkspaceID, ProviderID: provider.ID, Body: body, Event: event, Now: time.Now().UnixMilli(),
	})
	if err != nil {
		if errors.Is(err, r.Context().Err()) {
			return true
		}
		fail(w, 503, "unavailable", "Webhook could not be recorded; the same delivery can be retried safely", id)
		return true
	}
	if result.Conflict {
		fail(w, 409, "event_payload_conflict", "This event ID was already received with a different payload. The new payload was not applied and the event is flagged for investigation.", id)
		return true
	}
	status, state := 202, "processed"
	if result.Duplicate {
		status, state = 200, "duplicate"
	}
	writeJSON(w, status, map[string]any{
		"event_id":          inboxPrefix + result.Row.ID,
		"provider_event_id": result.Row.ProviderEventID,
		"result":            state,
		"outcome":           result.Row.Outcome,
		"outcome_reason":    result.Row.OutcomeReason,
		"duplicate_count":   result.Row.DuplicateCount,
		"signature_status":  result.Row.SignatureStatus,
	})
	return true
}
